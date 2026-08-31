package config

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

const MinSyncInterval = 30 * time.Second

var (
	config   *GatewayConfig
	loadErr  error
	loadOnce sync.Once
)

// ServerConfig covers the inbound edge: what this gateway accepts and where it
// keeps its own state. MaxBodySize is the cap on client request bodies; the
// outbound mirror of it is UpstreamConfig.MaxResponseBytes.
type ServerConfig struct {
	Port         int    `mapstructure:"port"`
	MaxBodySize  int    `mapstructure:"max_body_size"`
	ConfigSource string `mapstructure:"config_source"`
	DBPath       string `mapstructure:"db_path"`
	MockEnabled  bool   `mapstructure:"mock_enabled"`
	AesPasskey   string `mapstructure:"aes_pass_key"`
}

// UpstreamConfig is the deployment-wide policy for outbound provider calls, and
// it is the FLOOR: every key here carries a viper default, so a field is never
// meaningfully unset. A provider's core.NetworkConfig is the only thing that
// overrides it, and only for the handful of settings it declares.
//
// There is deliberately no third level of compiled-in constants. An earlier
// draft had one, which meant every default existed twice - once here, once as a
// const in the transport - free to drift apart with nothing to catch it.
//
// Durations are real time.Duration: write "5s", or a bare 5 meaning seconds.
type UpstreamConfig struct {
	MaxIdleConns        int `mapstructure:"max_idle_conns"`
	MaxConnsPerHost     int `mapstructure:"max_conns_per_host"`
	MaxIdleConnsPerHost int `mapstructure:"max_idle_conns_per_host"`

	IdleConnTimeout       time.Duration `mapstructure:"idle_conn_timeout"`
	DialTimeout           time.Duration `mapstructure:"dial_timeout"`
	KeepAlive             time.Duration `mapstructure:"keep_alive"`
	TLSHandshakeTimeout   time.Duration `mapstructure:"tls_handshake_timeout"`
	ResponseHeaderTimeout time.Duration `mapstructure:"response_header_timeout"`
	RequestTimeout        time.Duration `mapstructure:"request_timeout"`
	StreamIdleTimeout     time.Duration `mapstructure:"stream_idle_timeout"`

	MaxResponseBytesKB int  `mapstructure:"max_response_bytes"`
	WriteBufferSize    int  `mapstructure:"write_buffer_size"`
	ReadBufferSize     int  `mapstructure:"read_buffer_size"`
	DisableCompression bool `mapstructure:"disable_compression"`
}

// MaxResponseBytes returns the cap in bytes. The yaml key is in KB so it reads
// beside server.max_body_size, which is also KB; the shift belongs here rather
// than at the call site, where a missing "<< 10" is a 1000x error that nothing
// downstream would catch.
func (u UpstreamConfig) MaxResponseBytes() int64 {
	return int64(u.MaxResponseBytesKB) << 10
}

type Observability struct {
	LogLevel string `mapstructure:"log_level"`
}

type ModelCatalogConfig struct {
	SourceURL    string        `mapstructure:"source_url"`
	SyncInterval time.Duration `mapstructure:"sync_interval"`
}

type GatewayConfig struct {
	ServerConfig *ServerConfig `mapstructure:"server"`

	// Upstream is a value, not a pointer: it is the fallback layer, so an
	// absent section must still yield a fully populated policy. A pointer would
	// make "no yaml section" indistinguishable from "all zeros" at every call
	// site, and every call site would need the same nil guard.
	Upstream UpstreamConfig `mapstructure:"upstream"`

	Observability *Observability      `mapstructure:"observability"`
	ModelCatalog  *ModelCatalogConfig `mapstructure:"modelcatalog"`
}

func Load() (*GatewayConfig, error) {
	loadOnce.Do(func() {
		config, loadErr = read()
	})
	return config, loadErr
}

func GlobalConfig() *GatewayConfig {
	cfg, _ := Load()
	return cfg
}

// durationHook accepts either an explicit unit (dial_timeout: 5s) or a bare
// number meaning seconds (dial_timeout: 5).
//
// It is not optional. time.Duration is an int64 of NANOseconds, so plain
// mapstructure decodes "dial_timeout: 5" as five nanoseconds - a config that is
// syntactically perfect, passes validation, and takes the gateway down. Seconds
// is the right implied unit because every duration in this file is a network
// timeout.
func durationHook(from reflect.Type, to reflect.Type, data any) (any, error) {
	if to != reflect.TypeOf(time.Duration(0)) {
		return data, nil
	}
	switch from.Kind() {
	case reflect.String:
		d, err := time.ParseDuration(data.(string))
		if err != nil {
			return nil, fmt.Errorf("parse duration %q: %w", data, err)
		}
		return d, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return time.Duration(reflect.ValueOf(data).Int()) * time.Second, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return time.Duration(reflect.ValueOf(data).Uint()) * time.Second, nil
	case reflect.Float32, reflect.Float64:
		return time.Duration(reflect.ValueOf(data).Float() * float64(time.Second)), nil
	default:
		return data, nil
	}
}

func read() (*GatewayConfig, error) {
	viper.SetConfigName("server")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./configs")
	viper.AddConfigPath("./")

	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.config_source", "yaml")
	viper.SetDefault("server.db_path", "./data/diffractllm.db")
	viper.SetDefault("server.max_body_size", 32768) // KB -> 32MB

	// Every upstream key carries a default: this section is the fallback layer,
	// so a gap here is a zero reaching net/http, not a harmless omission.
	viper.SetDefault("upstream.max_idle_conns", 1000)
	viper.SetDefault("upstream.max_conns_per_host", 256)
	viper.SetDefault("upstream.max_idle_conns_per_host", 256)
	viper.SetDefault("upstream.idle_conn_timeout", "90s")
	viper.SetDefault("upstream.dial_timeout", "5s")
	viper.SetDefault("upstream.keep_alive", "30s")
	viper.SetDefault("upstream.tls_handshake_timeout", "10s")
	viper.SetDefault("upstream.response_header_timeout", "30s")
	viper.SetDefault("upstream.request_timeout", "60s")
	viper.SetDefault("upstream.stream_idle_timeout", "60s")
	viper.SetDefault("upstream.max_response_bytes", 32768) // KB -> 32MB
	viper.SetDefault("upstream.write_buffer_size", 64<<10)
	viper.SetDefault("upstream.read_buffer_size", 64<<10)
	viper.SetDefault("upstream.disable_compression", true)

	viper.SetDefault("observability.log_level", "info")

	viper.SetDefault("modelcatalog.sync_interval", "5m")
	viper.SetDefault("modelcatalog.source_url",
		"https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json")

	if err := viper.ReadInConfig(); err != nil {
		if _, notFound := err.(viper.ConfigFileNotFoundError); !notFound {
			return nil, fmt.Errorf("read server.yaml: %w", err)
		}
		// No file is fine - defaults apply.
	}

	cfg := &GatewayConfig{}
	// viper.DecodeHook REPLACES viper's built-in hook set, so the slice hook is
	// re-composed here; durationHook subsumes the string-to-duration one.
	if err := viper.Unmarshal(cfg, viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		mapstructure.DecodeHookFuncType(durationHook),
		mapstructure.StringToSliceHookFunc(","),
	))); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

func (c *GatewayConfig) Validate() error {
	if c.ServerConfig == nil {
		return fmt.Errorf("server section is required")
	}
	if err := c.ServerConfig.Validate(); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	if err := c.Upstream.Validate(); err != nil {
		return fmt.Errorf("upstream: %w", err)
	}
	if c.ModelCatalog != nil {
		if err := c.ModelCatalog.Validate(); err != nil {
			return fmt.Errorf("modelcatalog: %w", err)
		}
	}
	return nil
}

func (s *ServerConfig) Validate() error {
	if s.Port <= 0 || s.Port > 65535 {
		return fmt.Errorf("port must be in 1-65535, got %d", s.Port)
	}
	if s.DBPath == "" {
		return fmt.Errorf("db_path is required")
	}
	if s.MaxBodySize <= 0 {
		return fmt.Errorf("max_body_size must be positive, got %d", s.MaxBodySize)
	}
	return nil
}

// Validate is stricter than it was when compiled constants sat underneath this
// struct. Now that this IS the floor, a zero no longer means "fall through to
// the default" - it is the value net/http receives, and net/http reads most
// zeros as "unlimited" or "2". So every field is required to be positive.
func (u UpstreamConfig) Validate() error {
	if u.MaxConnsPerHost > 0 && u.MaxIdleConnsPerHost > u.MaxConnsPerHost {
		return fmt.Errorf(
			"max_idle_conns_per_host (%d) exceeds max_conns_per_host (%d); the idle limit is unreachable",
			u.MaxIdleConnsPerHost, u.MaxConnsPerHost)
	}

	counts := []struct {
		name  string
		value int
	}{
		{"max_idle_conns", u.MaxIdleConns},
		{"max_conns_per_host", u.MaxConnsPerHost},
		{"max_idle_conns_per_host", u.MaxIdleConnsPerHost},
		{"max_response_bytes", u.MaxResponseBytesKB},
		{"write_buffer_size", u.WriteBufferSize},
		{"read_buffer_size", u.ReadBufferSize},
	}
	for _, f := range counts {
		if f.value <= 0 {
			return fmt.Errorf("%s must be positive, got %d", f.name, f.value)
		}
	}

	durations := []struct {
		name  string
		value time.Duration
	}{
		{"idle_conn_timeout", u.IdleConnTimeout},
		{"dial_timeout", u.DialTimeout},
		{"keep_alive", u.KeepAlive},
		{"tls_handshake_timeout", u.TLSHandshakeTimeout},
		{"response_header_timeout", u.ResponseHeaderTimeout},
		{"request_timeout", u.RequestTimeout},
		{"stream_idle_timeout", u.StreamIdleTimeout},
	}
	for _, f := range durations {
		if f.value <= 0 {
			return fmt.Errorf("%s must be positive, got %s", f.name, f.value)
		}
	}
	return nil
}

func (m *ModelCatalogConfig) Validate() error {
	if err := checkInterval("sync_interval", m.SyncInterval); err != nil {
		return err
	}

	if m.SourceURL == "" {
		return fmt.Errorf("source_url is required when enabled")
	}

	return nil
}

func checkInterval(name string, d time.Duration) error {
	if d <= 0 {
		return fmt.Errorf("%s must be positive, got %s", name, d)
	}
	if d < MinSyncInterval {
		return fmt.Errorf("%s must be at least %s, got %s", name, MinSyncInterval, d)
	}
	return nil
}
