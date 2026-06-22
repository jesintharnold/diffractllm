package config

import (
	"fmt"
	"sync"

	"github.com/spf13/viper"
)

var (
	config          *GatewayConfig
	configsingleton sync.Once
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

type GatewayConfig struct {
	ServerConfig    *ServerConfig    `mapstructure:"server"`
	DataPlaneConfig *DataPlaneConfig `mapstructure:"dataplane"`
	Observability   *Observability   `mapstructure:"observability"`
}

func configNew() *GatewayConfig {
	configsingleton.Do(func() {
		viper.SetConfigName("server")
		viper.SetConfigType("yaml")
		viper.AddConfigPath("./") // preferred location
		viper.AddConfigPath("./") // fallback for transition

		viper.SetDefault("server.port", 8080)
		viper.SetDefault("server.config_source", "yaml")
		viper.SetDefault("server.db_path", "./data/diffractllm.db")
		viper.SetDefault("dataplane.max_idle_conns", 1000)
		viper.SetDefault("dataplane.enforce_global_timeouts", true)
		viper.SetDefault("dataplane.global_request_timeout", 30)

		if err := viper.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); ok {
				// Config file not found; ignore error if desired
				// fmt.Println("Config file not found, using defaults")
			} else {
				panic(fmt.Sprintf("error reading config.yaml %v ", err))
			}
		}
		config = &GatewayConfig{}
		if err := viper.Unmarshal(config); err != nil {
			panic(fmt.Sprintf("error during unmarshal %v ", err))
		}
	})
	return config
}

func GlobalConfig() *GatewayConfig {
	if config == nil {
		return configNew()
	}
	return config
}
