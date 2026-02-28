package station

import (
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
	safetyTick        = 30 * time.Second
)

type mpvClient interface {
	Close() error
	PlaylistCount() (int, error)
	PlaylistPaths() ([]string, error)
	LoadFile(path, mode string) error
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
				svc.saveState()
				return nil
			}
			if name, _ := event["event"].(string); name == "end-file" {
				if err := svc.fillQueue(); err != nil {
					svc.logf("fillQueue on end-file failed: %v", err)
				}
			}
		case <-ticker.C:
			if err := svc.fillQueue(); err != nil {
				svc.logf("fillQueue on ticker failed: %v", err)
			}
		case <-sigCh:
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
}

func (s *service) fillQueue() error {
	count, err := s.client.PlaylistCount()
	if err != nil {
		return fmt.Errorf("read playlist count: %w", err)
	}

	target := s.cfg.Station.QueueDepth
	if target <= 0 {
		target = defaultQueueDepth
	}

	seeds := len(s.st.Seeds)
	if seeds == 0 {
		return s.gcCache()
	}

	attempts := 0
	maxAttempts := target * seeds * 2
	if maxAttempts < seeds {
		maxAttempts = seeds
	}

	for count < target && attempts < maxAttempts {
		seed := s.st.PickSeed()
		if seed == "" {
			break
		}
		attempts++

		songKey := normalize(seed)
		if s.st.AlreadyPlayed("", songKey) {
			continue
		}

		audioPath, err := s.prov.Resolve(seed, s.cfg.Station.CacheDir)
		if err != nil {
			s.logf("resolve seed %q failed: %v", seed, err)
			continue
		}

		videoID := videoIDFromPath(audioPath)
		if s.st.AlreadyPlayed(videoID, songKey) {
			continue
		}

		if err := s.client.LoadFile(audioPath, "append"); err != nil {
			s.logf("append path %q failed: %v", audioPath, err)
			continue
		}

		s.st.MarkPlayed(videoID, songKey)
		count++
	}

	s.saveState()
	return s.gcCache()
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
