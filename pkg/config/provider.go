package config

import "context"

// Provider exposes read and watch capabilities for configuration data.
type Provider interface {
	UnmarshalKey(key string, out any) error
	Watch(ctx context.Context, key string, onChange func(Provider)) error
}

// Loader abstracts the creation of a configuration provider based on
// environment info. Implementations live under internal/config.
type Loader interface {
	Load(ctx context.Context) (Provider, error)
}
