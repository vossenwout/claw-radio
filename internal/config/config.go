package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	defaultMPVSocket      = "/tmp/claw-radio-mpv.sock"
	defaultTTSSocket      = "/tmp/claw-radio-tts.sock"
	defaultSearchSearxURL = "http://localhost:8888"
)

type Config struct {
	MPV     MPVConfig     `json:"mpv"`
	YtDlp   BinaryConfig  `json:"ytdlp"`
	FFmpeg  BinaryConfig  `json:"ffmpeg"`
	TTS     TTSConfig     `json:"tts"`
	Station StationConfig `json:"station"`
	Search  SearchConfig  `json:"search"`
}

type SearchConfig struct {
	SearxNGURL string `json:"searxng_url"`
}

type MPVConfig struct {
	Socket string `json:"socket"`
	Binary string `json:"binary"`
	Log    string `json:"log"`
}

type BinaryConfig struct {
	Binary string `json:"binary"`
}

type TTSConfig struct {
	Socket         string            `json:"socket"`
	DataDir        string            `json:"data_dir"`
	FallbackBinary string            `json:"fallback_binary"`
	Voices         map[string]string `json:"voices"`
}

type StationConfig struct {
	QueueDepth int    `json:"queue_depth"`
	CacheDir   string `json:"cache_dir"`
	StateDir   string `json:"state_dir"`
}

func Load() (*Config, error) {
	cfg := defaultConfig()

	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	expandPaths(&cfg)
	resolveBinaries(&cfg)

	return &cfg, nil
}

func defaultConfig() Config {
	return Config{
		MPV: MPVConfig{
			Socket: defaultMPVSocket,
			Log:    "~/.local/share/claw-radio/mpv.log",
		},
		TTS: TTSConfig{
			Socket:  defaultTTSSocket,
			DataDir: "~/.local/share/claw-radio",
			Voices: map[string]string{
				"pop":        "",
				"country":    "",
				"electronic": "",
				"default":    "",
			},
		},
		Station: StationConfig{
			QueueDepth: 5,
			CacheDir:   "~/.local/share/claw-radio/cache",
			StateDir:   "~/.local/share/claw-radio/state",
		},
		Search: SearchConfig{
			SearxNGURL: defaultSearchSearxURL,
		},
	}
}

func configPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv("CLAW_RADIO_CONFIG")); path != "" {
		return expandPath(path), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "claw-radio", "config.json"), nil
}

func resolveBinaries(cfg *Config) {
	cfg.MPV.Binary = resolveBinary(cfg.MPV.Binary, "mpv")
	cfg.YtDlp.Binary = resolveBinary(cfg.YtDlp.Binary, "yt-dlp")
	cfg.FFmpeg.Binary = resolveBinary(cfg.FFmpeg.Binary, "ffmpeg")

	if cfg.TTS.FallbackBinary == "" {
		switch runtime.GOOS {
		case "darwin":
			cfg.TTS.FallbackBinary = resolveBinary("", "say")
		default:
			cfg.TTS.FallbackBinary = resolveBinary("", "espeak-ng", "espeak", "festival")
		}
	}
}

func resolveBinary(configured string, names ...string) string {
	if configured != "" {
		return configured
	}
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func expandPaths(cfg *Config) {
	cfg.MPV.Log = expandPath(cfg.MPV.Log)
	cfg.TTS.DataDir = expandPath(cfg.TTS.DataDir)
	cfg.Station.CacheDir = expandPath(cfg.Station.CacheDir)
	cfg.Station.StateDir = expandPath(cfg.Station.StateDir)

	if cfg.TTS.Voices == nil {
		cfg.TTS.Voices = map[string]string{}
	}
	for key, path := range cfg.TTS.Voices {
		cfg.TTS.Voices[key] = expandPath(path)
	}
}

func expandPath(path string) string {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
