package station

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Station struct {
	StateDir string   `json:"-"`
	Label    string   `json:"label"`
	Seeds    []string `json:"seeds"`

	PlayedVideoIDs []string `json:"played_video_ids"`
	PlayedSongKeys []string `json:"played_song_keys"`

	seedIndex int
}

type stationFile struct {
	Label string   `json:"label"`
	Seeds []string `json:"seeds"`
}

type stateFile struct {
	PlayedVideoIDs []string `json:"played_video_ids"`
	PlayedSongKeys []string `json:"played_song_keys"`
}

func Load(stateDir string) (*Station, error) {
	st := &Station{
		StateDir:       stateDir,
		Seeds:          []string{},
		PlayedVideoIDs: []string{},
		PlayedSongKeys: []string{},
	}

	if stateDir == "" {
		return st, nil
	}

	stationData, err := os.ReadFile(filepath.Join(stateDir, "station.json"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read station file: %w", err)
		}
	} else {
		var sf stationFile
		if err := json.Unmarshal(stationData, &sf); err != nil {
			return nil, fmt.Errorf("parse station file: %w", err)
		}
		st.Label = sf.Label
		st.Seeds = append([]string(nil), sf.Seeds...)
	}

	stateData, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read state file: %w", err)
		}
	} else {
		var sf stateFile
		if err := json.Unmarshal(stateData, &sf); err != nil {
			return nil, fmt.Errorf("parse state file: %w", err)
		}
		st.PlayedVideoIDs = append([]string(nil), sf.PlayedVideoIDs...)
		st.PlayedSongKeys = append([]string(nil), sf.PlayedSongKeys...)
	}

	if st.Seeds == nil {
		st.Seeds = []string{}
	}
	if st.PlayedVideoIDs == nil {
		st.PlayedVideoIDs = []string{}
	}
	if st.PlayedSongKeys == nil {
		st.PlayedSongKeys = []string{}
	}

	return st, nil
}

func (s *Station) Save() error {
	if s == nil {
		return fmt.Errorf("station is nil")
	}
	if s.StateDir == "" {
		return fmt.Errorf("state directory is empty")
	}
	if err := os.MkdirAll(s.StateDir, 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	stationPayload := stationFile{
		Label: s.Label,
		Seeds: append([]string(nil), s.Seeds...),
	}
	if err := writeJSONAtomic(filepath.Join(s.StateDir, "station.json"), stationPayload); err != nil {
		return fmt.Errorf("write station file: %w", err)
	}

	statePayload := stateFile{
		PlayedVideoIDs: append([]string(nil), s.PlayedVideoIDs...),
		PlayedSongKeys: append([]string(nil), s.PlayedSongKeys...),
	}
	if err := writeJSONAtomic(filepath.Join(s.StateDir, "state.json"), statePayload); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}

	return nil
}

func (s *Station) SetSeeds(seeds []string, label string) {
	if s == nil {
		return
	}

	s.Seeds = append([]string(nil), seeds...)
	s.Label = label
	s.seedIndex = 0
}

func (s *Station) AppendSeeds(seeds []string) {
	if s == nil {
		return
	}

	existing := make(map[string]struct{}, len(s.Seeds))
	for _, seed := range s.Seeds {
		existing[normalize(seed)] = struct{}{}
	}

	for _, seed := range seeds {
		key := normalize(seed)
		if key == "" {
			continue
		}
		if _, ok := existing[key]; ok {
			continue
		}
		s.Seeds = append(s.Seeds, seed)
		existing[key] = struct{}{}
	}
}

func (s *Station) RemoveSeed(seed string) bool {
	if s == nil {
		return false
	}
	key := normalize(seed)
	if key == "" {
		return false
	}
	for i, existing := range s.Seeds {
		if normalize(existing) != key {
			continue
		}
		s.Seeds = append(s.Seeds[:i], s.Seeds[i+1:]...)
		if s.seedIndex > i {
			s.seedIndex--
		}
		if s.seedIndex >= len(s.Seeds) {
			s.seedIndex = 0
		}
		return true
	}
	return false
}

func (s *Station) PickSeed() string {
	if s == nil || len(s.Seeds) == 0 {
		return ""
	}

	seed := s.Seeds[s.seedIndex]
	s.seedIndex = (s.seedIndex + 1) % len(s.Seeds)
	return seed
}

func (s *Station) MarkPlayed(videoID, songKey string) {
	if s == nil {
		return
	}

	videoID = strings.TrimSpace(videoID)
	if videoID != "" && !contains(s.PlayedVideoIDs, videoID) {
		s.PlayedVideoIDs = append(s.PlayedVideoIDs, videoID)
	}

	normalizedSong := normalize(songKey)
	if normalizedSong != "" && !contains(s.PlayedSongKeys, normalizedSong) {
		s.PlayedSongKeys = append(s.PlayedSongKeys, normalizedSong)
	}
}

func (s *Station) AlreadyPlayed(videoID, songKey string) bool {
	if s == nil {
		return false
	}

	videoID = strings.TrimSpace(videoID)
	if videoID != "" && contains(s.PlayedVideoIDs, videoID) {
		return true
	}

	normalizedSong := normalize(songKey)
	return normalizedSong != "" && contains(s.PlayedSongKeys, normalizedSong)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func normalize(input string) string {
	clean := strings.ToLower(strings.TrimSpace(input))
	if clean == "" {
		return ""
	}
	return strings.Join(strings.Fields(clean), " ")
}

func writeJSONAtomic(path string, payload interface{}) (retErr error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if retErr != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		_ = tmp.Close()
		retErr = err
		return retErr
	}

	if err := tmp.Close(); err != nil {
		retErr = err
		return retErr
	}

	retErr = os.Rename(tmpPath, path)
	return retErr
}
