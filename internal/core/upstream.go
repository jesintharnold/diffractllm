package core

import "time"

type NetworkConfig struct {
	BaseURL            string            `json:"base_url"`
	Headers            map[string]string `json:"headers,omitempty"`
	RequestTimeout     *time.Duration    `json:"request_timeout,omitempty"`
	MaxRetries         *int              `json:"max_retries,omitempty"`
	RetryBackoff       *time.Duration    `json:"retry_backoff,omitempty"`
	MaxConnsPerHost    *int              `json:"max_conns_per_host,omitempty"`
	InsecureSkipVerify bool              `json:"insecure_skip_verify,omitempty"`
}

type ProxyType string

const (
	ProxyHTTP        ProxyType = "http"
	ProxySOCKS5      ProxyType = "socks5"
	ProxyEnvironment ProxyType = "environment"
)

type ProxyConfig struct {
	Type     ProxyType `json:"type"`
	URL      string    `json:"url,omitempty"`
	Username string    `json:"username,omitempty"`
	Password string    `json:"password,omitempty"`
}

type Upstream struct {
	Provider Provider      `json:"provider"`
	Network  NetworkConfig `json:"network_config"`
	Proxy    *ProxyConfig  `json:"proxy_config,omitempty"`
}
