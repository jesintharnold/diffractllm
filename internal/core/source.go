package core

type ConfigSource interface {
	Load() (string, error)
	Name() string
	Path() string
	Close() error
}
