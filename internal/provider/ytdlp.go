package provider

import "fmt"

type YtDlpProvider struct {
	Binary string
}

func NewYtDlpProvider(binary string) *YtDlpProvider {
	return &YtDlpProvider{Binary: binary}
}

func (p *YtDlpProvider) Resolve(seed, cacheDir string) (string, error) {
	return "", fmt.Errorf("youtube provider not implemented")
}

func (p *YtDlpProvider) Name() string {
	return "youtube"
}
