package station

import (
	"strings"

	"github.com/vossenwout/claw-radio/internal/config"
)

type PlaylistSongStatus string

const (
	PlaylistSongReady     PlaylistSongStatus = "ready"
	PlaylistSongPreparing PlaylistSongStatus = "preparing"
)

type PlaylistSong struct {
	Seed   string             `json:"seed"`
	Artist string             `json:"artist,omitempty"`
	Title  string             `json:"title,omitempty"`
	Path   string             `json:"path,omitempty"`
	Status PlaylistSongStatus `json:"status"`
}

type PlaylistSnapshot struct {
	Songs     []PlaylistSong `json:"songs"`
	Ready     int            `json:"ready"`
	Preparing int            `json:"preparing"`
}

func BuildPlaylistSnapshot(cfg *config.Config, seeds []string, currentPath string, remainingPaths []string) PlaylistSnapshot {
	queued := queuedSeedPaths(cfg, remainingPaths)
	currentKey := normalize(seedFromPath(currentPath))

	snapshot := PlaylistSnapshot{Songs: make([]PlaylistSong, 0, len(seeds))}
	for _, seed := range seeds {
		song := agentSongFromSeed(seed)
		key := normalize(song.Seed)
		if key == "" {
			continue
		}
		if currentKey != "" && key == currentKey {
			continue
		}

		entry := PlaylistSong{
			Seed:   song.Seed,
			Artist: song.Artist,
			Title:  song.Title,
			Status: PlaylistSongPreparing,
		}
		if path, ok := queued[key]; ok {
			entry.Path = path
			entry.Status = PlaylistSongReady
			snapshot.Ready++
		} else {
			snapshot.Preparing++
		}
		snapshot.Songs = append(snapshot.Songs, entry)
	}

	return snapshot
}

func NextAgentSong(seeds []string, currentPath string) (AgentSong, bool) {
	currentKey := normalize(seedFromPath(currentPath))
	for _, seed := range seeds {
		song := agentSongFromSeed(seed)
		key := normalize(song.Seed)
		if key == "" {
			continue
		}
		if currentKey != "" && key == currentKey {
			continue
		}
		return song, true
	}
	return AgentSong{}, false
}

func SameAgentSong(a, b AgentSong) bool {
	keyA := agentSongKey(a)
	keyB := agentSongKey(b)
	if keyA != "" && keyB != "" {
		return keyA == keyB
	}
	return sameMediaPath(a.Path, b.Path)
}

func agentSongFromSeed(seed string) AgentSong {
	trimmed := strings.TrimSpace(seed)
	artist, title := splitSeedParts(trimmed)
	song := AgentSong{
		Seed:   trimmed,
		Artist: artist,
		Title:  title,
	}
	if song.Title == "" {
		song.Title = trimmed
	}
	return song
}

func agentSongKey(song AgentSong) string {
	if key := normalize(song.Seed); key != "" {
		return key
	}
	if key := normalize(seedFromPath(song.Path)); key != "" {
		return key
	}
	artist := normalize(song.Artist)
	title := normalize(song.Title)
	if artist != "" && title != "" {
		return artist + " - " + title
	}
	if title != "" {
		return title
	}
	return ""
}

func queuedSeedPaths(cfg *config.Config, remainingPaths []string) map[string]string {
	queued := make(map[string]string, len(remainingPaths))
	for _, path := range remainingPaths {
		path = strings.TrimSpace(path)
		if path == "" || isBanterPath(cfg, path) {
			continue
		}
		key := normalize(seedFromPath(path))
		if key == "" {
			continue
		}
		if _, exists := queued[key]; exists {
			continue
		}
		queued[key] = path
	}
	return queued
}

func splitSeedParts(seed string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(seed), " - ", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}
