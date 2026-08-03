package config

import (
	"fmt"
	"sync"
	"time"

	"github.com/spf13/viper"
)

const MinSyncInterval = 30 * time.Second

var (
	config   *GatewayConfig
	loadErr  error
	loadOnce sync.Once
)

type ServerConfig struct {
	Port         int    `mapstructure:"port"`
	LogLevel     string `mapstructure:"log_level"`
	MaxBodySize  int    `mapstructure:"max_body_size"`
	ConfigSource string `mapstructure:"config_source"`
	DBPath       string `mapstructure:"db_path"`
	MockEnabled  bool   `mapstructure:"mock_enabled"`
	AesPasskey   string `mapstructure:"aes_pass_key"`
}

type DataPlaneConfig struct {
	MaxIdleConns              int             `mapstructure:"max_idle_conns"`
	EnforceGlobalTimeouts     bool            `mapstructure:"enforce_global_timeouts"`
	GlobalRequestTimeout      int             `mapstructure:"global_request_timeout"`
	GlobalHealthcheckTimeout  int             `mapstructure:"global_health_timeout"`
	GlobalHealthcheckInterval int             `mapstructure:"global_health_interval"`
	GlobalHealthcheckPath     string          `mapstructure:"global_health_path"`
	GlobalHealthfailureCount  int             `mapstructure:"global_health_fail_count"`
	GlobalHealthsucessCount   int             `mapstructure:"global_health_success_count"`
	TransportConfig           TransportConfig `mapstructure:"transport"` // https , http , h2c
}

type TransportConfig struct {
	GlobalMaxIdleConns        int    `mapstructure:"max_idle_conns"`
	GlobalMaxIdleConnsPerHost int    `mapstructure:"max_idle_conns_per_host"`
	GlobalMaxConnsPerHost     int    `mapstructure:"max_conns_per_host"`
	GlobalIdleConnTimeout     int    `mapstructure:"idle_conn_timeout"`
	GlobalResponseTimeout     int    `mapstructure:"response_header_timeout"`
	GlobalDialTimeout         int    `mapstructure:"dial_timeout"`
	GlobalKeepAlive           int    `mapstructure:"keep_alive"`
	GlobalTLSTimeout          int    `mapstructure:"tls_handshake_timeout"`
	GlobalDisableCompression  bool   `mapstructure:"disable_compression"`
	GlobalScheme              string `mapstructure:"defaultScheme"`
}

type Observability struct {
	LogLevel string `mapstructure:"log_level"`
}

type ModelCatalogConfig struct {
	SourceName   string        `mapstructure:"source_name"` // "litellm" | "bifrost"
	SourceURL    string        `mapstructure:"source_url"`
	SyncInterval time.Duration `mapstructure:"sync_interval"`
}

type GatewayConfig struct {
	ServerConfig    *ServerConfig       `mapstructure:"server"`
	DataPlaneConfig *DataPlaneConfig    `mapstructure:"dataplane"`
	Observability   *Observability      `mapstructure:"observability"`
	ModelCatalog    *ModelCatalogConfig `mapstructure:"modelcatalog"`
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

func read() (*GatewayConfig, error) {
	viper.SetConfigName("server")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./configs")
	viper.AddConfigPath("./")

	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.config_source", "yaml")
	viper.SetDefault("server.db_path", "./data/diffractllm.db")
	viper.SetDefault("dataplane.max_idle_conns", 1000)
	viper.SetDefault("dataplane.enforce_global_timeouts", true)
	viper.SetDefault("dataplane.global_request_timeout", 30)

	viper.SetDefault("modelcatalog.sync_interval", "5m")
	viper.SetDefault("modelcatalog.source_name", "litellm")
	viper.SetDefault("modelcatalog.source_url",
		"https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json")
	viper.SetDefault("modelcatalog.enabled", false)

	if err := viper.ReadInConfig(); err != nil {
		if _, notFound := err.(viper.ConfigFileNotFoundError); !notFound {
			return nil, fmt.Errorf("read server.yaml: %w", err)
		}
		// No file is fine - defaults apply.
	}

	cfg := &GatewayConfig{}
	if err := viper.Unmarshal(cfg); err != nil {
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
	if c.ServerConfig.Port <= 0 || c.ServerConfig.Port > 65535 {
		return fmt.Errorf("server.port must be in 1-65535, got %d", c.ServerConfig.Port)
	}
	if c.ServerConfig.DBPath == "" {
		return fmt.Errorf("server.db_path is required")
	}
	if c.ModelCatalog != nil {
		if err := c.ModelCatalog.Validate(); err != nil {
			return fmt.Errorf("modelcatalog: %w", err)
		}
	}
	return nil
}

func (m *ModelCatalogConfig) Validate() error {
	if err := checkInterval("sync_interval", m.SyncInterval); err != nil {
		return err
	}

	if m.SourceName == "" {
		return fmt.Errorf("source_name is required when enabled")
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
