package upload

import (
	"fmt"
)

// ProviderFactory is a function that creates a new provider instance
type ProviderFactory func() Provider

// Registry holds all available upload providers
var Registry = make(map[string]ProviderFactory)

// RegisterProvider registers a new upload provider
func RegisterProvider(name string, factory ProviderFactory) {
	Registry[name] = factory
}

// NewProvider creates a new provider instance by name
func NewProvider(name string) (Provider, error) {
	factory, ok := Registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown upload provider: %s", name)
	}
	return factory(), nil
}

// init registers all built-in providers
func init() {
	RegisterProvider("minio", func() Provider {
		return NewMinioProvider()
	})
}
