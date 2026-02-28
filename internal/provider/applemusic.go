package provider

import "fmt"

type AppleMusicProvider struct{}

func (p *AppleMusicProvider) Resolve(seed, cacheDir string) (string, error) {
	return "", fmt.Errorf("apple music provider not implemented")
}

func (p *AppleMusicProvider) Name() string {
	return "applemusic"
}
