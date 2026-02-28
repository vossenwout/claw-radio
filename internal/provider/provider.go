package provider

type Provider interface {
	Resolve(seed, cacheDir string) (audioPath string, err error)
	Name() string
}
