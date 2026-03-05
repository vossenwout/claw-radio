package station

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/vossenwout/claw-radio/internal/config"
	"github.com/vossenwout/claw-radio/internal/mpv"
	"github.com/vossenwout/claw-radio/internal/provider"
)

const (
	defaultQueueDepth = 5
	safetyTick        = 2 * time.Second
)

type mpvClient interface {
	Close() error
	PlaylistCount() (int, error)
	PlaylistPaths() ([]string, error)
	LoadFile(path, mode string) error
	Get(prop string) (json.RawMessage, error)
	Events() <-chan map[string]interface{}
}

var dialMPV = func(socketPath string) (mpvClient, error) {
	return mpv.Dial(socketPath)
}

func Run(cfg *config.Config, prov provider.Provider, log io.Writer) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if prov == nil {
		return fmt.Errorf("provider is nil")
	}

	if err := os.MkdirAll(cfg.Station.StateDir, 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	if err := os.MkdirAll(cfg.Station.CacheDir, 0o755); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}

	st, err := Load(cfg.Station.StateDir)
	if err != nil {
		return fmt.Errorf("load station state: %w", err)
	}

	client, err := connectMPVWithBackoff(cfg.MPV.Socket)
	if err != nil {
		return err
	}
	defer client.Close()

	svc := &service{
		cfg:    cfg,
		st:     st,
		client: client,
		prov:   prov,
		log:    log,
		events: NewAgentEventStore(cfg.Station.StateDir),
	}
	if err := svc.events.Ensure(); err != nil {
		svc.logf("ensure agent events failed: %v", err)
	}
	var startupPrefetched *prefetchedSong
	if svc.pendingIntroExists() {
		prefetched, err := svc.prefetchOneSeedForStartup()
		if err != nil {
			svc.logf("prefetch first song before intro failed: %v", err)
		} else {
			startupPrefetched = prefetched
		}
	}
	if err := svc.enqueuePendingIntro(); err != nil {
		svc.logf("enqueue pending intro failed: %v", err)
	}
	if startupPrefetched != nil {
		if err := svc.enqueuePrefetchedSong(startupPrefetched); err != nil {
			svc.logf("enqueue prefetched startup song failed: %v", err)
		}
	}

	if err := svc.fillQueue(); err != nil {
		svc.logf("initial fillQueue failed: %v", err)
	}

	ticker := time.NewTicker(safetyTick)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	for {
		select {
		case event, ok := <-client.Events():
			if !ok {
				svc.emitEngineStopped()
				svc.saveState()
				return nil
			}
			if name, _ := event["event"].(string); name == "file-loaded" {
				if err := svc.handleTrackStarted(); err != nil {
					svc.logf("handleTrackStarted failed: %v", err)
				}
			}
			if name, _ := event["event"].(string); name == "end-file" {
				if err := svc.fillQueue(); err != nil {
					svc.logf("fillQueue on end-file failed: %v", err)
				}
				svc.emitQueueLowIfNeeded()
			}
		case <-ticker.C:
			if err := svc.fillQueue(); err != nil {
				svc.logf("fillQueue on ticker failed: %v", err)
			}
		case <-sigCh:
			svc.emitEngineStopped()
			svc.saveState()
			return nil
		}
	}
}

type service struct {
	cfg    *config.Config
	st     *Station
	client mpvClient
	prov   provider.Provider
	log    io.Writer
	events *AgentEventStore
}

type prefetchedSong struct {
	AudioPath string
	SongKey   string
	VideoID   string
}

func (s *service) pendingIntroExists() bool {
	if s.events == nil {
		return false
	}
	pending, err := s.events.LoadPendingIntro()
	if err != nil || pending == nil {
		return false
	}
	if strings.TrimSpace(pending.AudioPath) == "" {
		return false
	}
	if _, err := os.Stat(pending.AudioPath); err != nil {
		return false
	}
	return true
}

func (s *service) prefetchOneSeedForStartup() (*prefetchedSong, error) {
	s.refreshQueueFromDisk()
	if s == nil || s.st == nil || s.prov == nil || len(s.st.Seeds) == 0 {
		return nil, nil
	}
	for _, seed := range s.st.Seeds {
		if seed == "" {
			continue
		}
		audioPath, err := s.prov.Resolve(seed, s.cfg.Station.CacheDir)
		if err != nil {
			s.logf("prefetch seed %q failed: %v", seed, err)
			continue
		}
		return &prefetchedSong{AudioPath: audioPath, SongKey: normalize(seed), VideoID: videoIDFromPath(audioPath)}, nil
	}

	return nil, nil
}

func (s *service) enqueuePrefetchedSong(prefetched *prefetchedSong) error {
	if prefetched == nil || strings.TrimSpace(prefetched.AudioPath) == "" {
		return nil
	}

	mode := "append"
	if count, err := s.client.PlaylistCount(); err == nil && count == 0 {
		mode = "append-play"
	}
	if err := s.client.LoadFile(prefetched.AudioPath, mode); err != nil {
		return err
	}
	return nil
}

func (s *service) handleTrackStarted() error {
	currentPath, _ := readStringPropertyCompat(s.client, "path")
	if currentPath != "" {
		s.consumePlayedSeedForPath(currentPath)
	}
	if currentPath != "" {
		if pending, err := s.events.LoadPendingBanter(); err == nil && pending != nil {
			if sameMediaPath(pending.NextSong.Path, currentPath) {
				_ = s.events.ClearPendingBanter()
			}
		}
	}
	if isBanterPath(s.cfg, currentPath) {
		return nil
	}
	return s.emitBanterNeededForUpcoming(currentPath)
}

func (s *service) emitBanterNeededForUpcoming(currentPath string) error {
	nextSong, ok, err := s.peekNextSong()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if sameMediaPath(currentPath, nextSong.Path) {
		return nil
	}

	if pending, err := s.events.LoadPendingBanter(); err == nil && pending != nil {
		if sameMediaPath(pending.NextSong.Path, nextSong.Path) {
			return nil
		}
	}

	eventID := fmt.Sprintf("evt_%d", time.Now().UnixNano())
	nextDisplay := strings.TrimSpace(nextSong.Artist)
	if strings.TrimSpace(nextSong.Title) != "" {
		if nextDisplay != "" {
			nextDisplay += " - " + strings.TrimSpace(nextSong.Title)
		} else {
			nextDisplay = strings.TrimSpace(nextSong.Title)
		}
	}
	prompt := "Generate banter for the next song in 1-2 short sentences."
	if nextDisplay != "" {
		prompt += " Next song: " + nextDisplay
	}

	if err := s.events.SavePendingBanter(PendingBanter{
		EventID:    eventID,
		TS:         time.Now().Unix(),
		Prompt:     prompt,
		NextSong:   nextSong,
		DeadlineMS: defaultBanterDeadlineMillis,
		Fulfilled:  false,
	}); err != nil {
		return err
	}

	return s.events.Append(AgentEvent{
		Event:      "banter_needed",
		EventID:    eventID,
		TS:         time.Now().Unix(),
		Prompt:     prompt,
		NextSong:   &nextSong,
		DeadlineMS: defaultBanterDeadlineMillis,
	})
}

func (s *service) emitQueueLowIfNeeded() {
	count, err := s.client.PlaylistCount()
	if err != nil {
		return
	}
	if count > 2 {
		return
	}
	depth := s.cfg.Station.QueueDepth
	if depth <= 0 {
		depth = defaultQueueDepth
	}
	_ = s.events.Append(AgentEvent{
		Event:   "queue_low",
		EventID: fmt.Sprintf("evt_%d", time.Now().UnixNano()),
		TS:      time.Now().Unix(),
		Count:   count,
		Depth:   depth,
	})
}

func (s *service) emitEngineStopped() {
	if s.events == nil {
		return
	}
	_ = s.events.Append(AgentEvent{
		Event:   "engine_stopped",
		EventID: fmt.Sprintf("evt_%d", time.Now().UnixNano()),
		TS:      time.Now().Unix(),
	})
}

func (s *service) enqueuePendingIntro() error {
	if s.events == nil {
		return nil
	}
	pending, err := s.events.LoadPendingIntro()
	if err != nil || pending == nil {
		return err
	}
	if strings.TrimSpace(pending.AudioPath) == "" {
		_ = s.events.ClearPendingIntro()
		return nil
	}
	if _, err := os.Stat(pending.AudioPath); err != nil {
		_ = s.events.ClearPendingIntro()
		return nil
	}
	mode := "append"
	if count, err := s.client.PlaylistCount(); err == nil && count == 0 {
		mode = "append-play"
	}
	if err := s.client.LoadFile(pending.AudioPath, mode); err != nil {
		return err
	}
	_ = s.events.ClearPendingIntro()
	return nil
}

func (s *service) peekNextSong() (AgentSong, bool, error) {
	raw, err := s.client.Get("playlist")
	if err != nil {
		return AgentSong{}, false, err
	}

	var items []struct {
		Filename string `json:"filename"`
		Current  bool   `json:"current"`
		Playing  bool   `json:"playing"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return AgentSong{}, false, err
	}
	if len(items) == 0 {
		return AgentSong{}, false, nil
	}

	currentIndex := -1
	for i, item := range items {
		if item.Current || item.Playing {
			currentIndex = i
			break
		}
	}
	if currentIndex < 0 {
		return AgentSong{}, false, nil
	}
	nextIndex := currentIndex + 1
	if nextIndex >= len(items) {
		return AgentSong{}, false, nil
	}
	nextPath := strings.TrimSpace(items[nextIndex].Filename)
	if nextPath == "" {
		return AgentSong{}, false, nil
	}

	song := songFromPath(nextPath)
	return song, true, nil
}

func songFromPath(path string) AgentSong {
	song := AgentSong{Path: strings.TrimSpace(path)}
	if song.Path == "" {
		return song
	}

	metaPath := song.Path + ".meta.json"
	data, err := os.ReadFile(metaPath)
	if err == nil {
		var meta struct {
			Seed    string `json:"seed"`
			Artist  string `json:"artist"`
			Title   string `json:"title"`
			Display string `json:"display"`
		}
		if err := json.Unmarshal(data, &meta); err == nil {
			song.Artist = strings.TrimSpace(meta.Artist)
			song.Title = strings.TrimSpace(meta.Title)
			if song.Artist == "" && song.Title == "" {
				display := strings.TrimSpace(meta.Display)
				if display == "" {
					display = strings.TrimSpace(meta.Seed)
				}
				if display != "" {
					parts := strings.SplitN(display, " - ", 2)
					if len(parts) == 2 {
						song.Artist = strings.TrimSpace(parts[0])
						song.Title = strings.TrimSpace(parts[1])
					} else {
						song.Title = display
					}
				}
			}
		}
	}

	if song.Title == "" {
		base := filepath.Base(song.Path)
		song.Title = strings.TrimSpace(strings.TrimSuffix(base, filepath.Ext(base)))
	}

	return song
}

func sameMediaPath(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil && errB == nil {
		return absA == absB
	}
	return false
}

func readStringPropertyCompat(client mpvClient, prop string) (string, bool) {
	raw, err := client.Get(prop)
	if err != nil {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

func readBoolPropertyCompat(client mpvClient, prop string) (bool, bool) {
	raw, err := client.Get(prop)
	if err != nil {
		return false, false
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, false
	}
	return value, true
}

func (s *service) consumePlayedSeedForPath(path string) {
	seed := seedFromPath(path)
	if seed == "" {
		return
	}
	latest, err := Load(s.cfg.Station.StateDir)
	if err != nil {
		s.logf("load station for consume failed: %v", err)
		return
	}
	if latest.RemoveSeed(seed) {
		if err := latest.Save(); err != nil {
			s.logf("save station after consume failed: %v", err)
			return
		}
		s.st.Seeds = append([]string(nil), latest.Seeds...)
		s.st.Label = latest.Label
	}
}

func (s *service) refreshQueueFromDisk() {
	latest, err := Load(s.cfg.Station.StateDir)
	if err != nil {
		s.logf("reload station state failed: %v", err)
		return
	}
	s.st.Seeds = append([]string(nil), latest.Seeds...)
	s.st.Label = latest.Label
}

func seedFromPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	data, err := os.ReadFile(trimmed + ".meta.json")
	if err != nil {
		return ""
	}
	var meta struct {
		Seed    string `json:"seed"`
		Display string `json:"display"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	if strings.TrimSpace(meta.Seed) != "" {
		return strings.TrimSpace(meta.Seed)
	}
	return strings.TrimSpace(meta.Display)
}

func isBanterPath(cfg *config.Config, mediaPath string) bool {
	if cfg == nil {
		return false
	}
	base := strings.TrimSpace(cfg.TTS.DataDir)
	path := strings.TrimSpace(mediaPath)
	if base == "" || path == "" {
		return false
	}
	banterDir := filepath.Join(base, "banter")
	absBanter, errA := filepath.Abs(banterDir)
	absPath, errB := filepath.Abs(path)
	if errA != nil || errB != nil {
		return strings.HasPrefix(path, banterDir+string(os.PathSeparator)) || path == banterDir
	}
	return strings.HasPrefix(absPath, absBanter+string(os.PathSeparator)) || absPath == absBanter
}

func (s *service) fillQueue() error {
	s.refreshQueueFromDisk()

	count, err := s.client.PlaylistCount()
	if err != nil {
		return fmt.Errorf("read playlist count: %w", err)
	}

	target := s.cfg.Station.QueueDepth
	if target <= 0 {
		target = defaultQueueDepth
	}

	if len(s.st.Seeds) == 0 {
		return s.gcCache()
	}
	queued := s.queuedSeedKeys()
	added := false
	for _, seed := range s.st.Seeds {
		if count >= target {
			break
		}
		songKey := normalize(seed)
		if songKey == "" {
			continue
		}
		if _, exists := queued[songKey]; exists {
			continue
		}
		audioPath, err := s.prov.Resolve(seed, s.cfg.Station.CacheDir)
		if err != nil {
			s.logf("resolve seed %q failed: %v", seed, err)
			continue
		}

		mode := "append"
		idleActive, ok := readBoolPropertyCompat(s.client, "idle-active")
		if count == 0 || (ok && idleActive) {
			mode = "append-play"
		}
		if err := s.client.LoadFile(audioPath, mode); err != nil {
			s.logf("append path %q failed: %v", audioPath, err)
			continue
		}
		queued[songKey] = struct{}{}
		count++
		added = true
	}
	if added {
		currentPath, _ := readStringPropertyCompat(s.client, "path")
		if currentPath != "" && !isBanterPath(s.cfg, currentPath) {
			if err := s.emitBanterNeededForUpcoming(currentPath); err != nil {
				s.logf("emit banter cue after queue update failed: %v", err)
			}
		}
	}
	return s.gcCache()
}

func (s *service) queuedSeedKeys() map[string]struct{} {
	keys := map[string]struct{}{}
	paths, err := s.client.PlaylistPaths()
	if err != nil {
		return keys
	}
	for _, path := range paths {
		seed := seedFromPath(path)
		key := normalize(seed)
		if key == "" {
			continue
		}
		keys[key] = struct{}{}
	}
	return keys
}

func (s *service) gcCache() error {
	playlistPaths, err := s.client.PlaylistPaths()
	if err != nil {
		return fmt.Errorf("read playlist paths: %w", err)
	}

	queueDepth := s.cfg.Station.QueueDepth
	if queueDepth <= 0 {
		queueDepth = defaultQueueDepth
	}
	keepRecent := queueDepth + 3

	keepSet := make(map[string]struct{}, len(playlistPaths)*2)
	for _, p := range playlistPaths {
		if p == "" {
			continue
		}
		keepSet[p] = struct{}{}
		if abs, err := filepath.Abs(p); err == nil {
			keepSet[abs] = struct{}{}
		}
	}

	entries, err := os.ReadDir(s.cfg.Station.CacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read cache directory: %w", err)
	}

	type cacheItem struct {
		path    string
		modTime time.Time
	}

	items := make([]cacheItem, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fullPath := filepath.Join(s.cfg.Station.CacheDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		items = append(items, cacheItem{path: fullPath, modTime: info.ModTime()})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].modTime.After(items[j].modTime)
	})

	keepByPath := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, ok := keepSet[item.path]; ok {
			keepByPath[item.path] = struct{}{}
		}
	}

	keptRecent := 0
	for _, item := range items {
		if _, pinned := keepByPath[item.path]; pinned {
			continue
		}
		if keptRecent < keepRecent {
			keepByPath[item.path] = struct{}{}
			keptRecent++
		}
	}

	for _, item := range items {
		if _, keep := keepByPath[item.path]; keep {
			continue
		}
		if err := os.Remove(item.path); err != nil && !os.IsNotExist(err) {
			s.logf("remove cache file %q failed: %v", item.path, err)
		}
	}

	return nil
}

func (s *service) saveState() {
	if err := s.st.Save(); err != nil {
		s.logf("save state failed: %v", err)
	}
}

func (s *service) logf(format string, args ...interface{}) {
	if s.log == nil {
		return
	}
	_, _ = fmt.Fprintf(s.log, "%s station: %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

func connectMPVWithBackoff(socketPath string) (mpvClient, error) {
	if strings.TrimSpace(socketPath) == "" {
		return nil, fmt.Errorf("mpv socket path is empty")
	}

	backoff := 100 * time.Millisecond
	for {
		client, err := dialMPV(socketPath)
		if err == nil {
			return client, nil
		}

		time.Sleep(backoff)
		if backoff < 5*time.Second {
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}
}

func videoIDFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext == "" {
		return base
	}
	return strings.TrimSuffix(base, ext)
}
