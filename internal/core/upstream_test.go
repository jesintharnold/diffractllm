package core

import (
	"testing"
	"time"
)

func TestNetworkConfigIsZero(t *testing.T) {
	timeout := 5 * time.Second
	conns := 10

	tests := []struct {
		name   string
		config NetworkConfig
		want   bool
	}{
		{
			name:   "empty config is zero",
			config: NetworkConfig{},
			want:   true,
		},
		{
			name:   "base url set",
			config: NetworkConfig{BaseURL: "https://api.openai.com"},
			want:   false,
		},
		{
			name:   "headers set",
			config: NetworkConfig{Headers: map[string]string{"x-trace": "1"}},
			want:   false,
		},
		{
			name:   "empty headers map is still zero",
			config: NetworkConfig{Headers: map[string]string{}},
			want:   true,
		},
		{
			name:   "request timeout set",
			config: NetworkConfig{RequestTimeout: &timeout},
			want:   false,
		},
		{
			name:   "max conns set",
			config: NetworkConfig{MaxConnsPerHost: &conns},
			want:   false,
		},
		{
			name:   "insecure skip verify set",
			config: NetworkConfig{InsecureSkipVerify: true},
			want:   false,
		},
		{
			name:   "allow private network set",
			config: NetworkConfig{AllowPrivateNetwork: true},
			want:   false,
		},
		{
			name:   "max response bytes set",
			config: NetworkConfig{MaxResponseBytes: 1024},
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.config.IsZero()
			if got != tc.want {
				t.Errorf("IsZero() = %v, want %v", got, tc.want)
			}
		})
	}
}
