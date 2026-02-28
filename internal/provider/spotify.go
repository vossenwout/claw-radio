package provider

import "fmt"

type SpotifyProvider struct {
	ClientID     string
	ClientSecret string
}

func (p *SpotifyProvider) Resolve(seed, cacheDir string) (string, error) {
	return "", fmt.Errorf("spotify provider not implemented")
}

func (p *SpotifyProvider) Name() string {
	return "spotify"
}
