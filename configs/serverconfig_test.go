package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func validUpstream() UpstreamConfig {
	return UpstreamConfig{
		MaxIdleConns: 10, MaxConnsPerHost: 8, MaxIdleConnsPerHost: 8,
		IdleConnTimeout: time.Second, DialTimeout: time.Second, KeepAlive: time.Second,
		TLSHandshakeTimeout: time.Second, ResponseHeaderTimeout: time.Second,
		RequestTimeout: time.Second, StreamIdleTimeout: time.Second, RetryBackoff: time.Second,
		MaxResponseBytesKB: 64, WriteBufferSize: 4096, ReadBufferSize: 4096,
	}
}

func TestMaxConcurrencyValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   int
		wantErr bool
	}{
		{"off", 0, false},
		{"within max_conns_per_host", 8, false},
		{"negative", -1, true},
		{"above max_conns_per_host", 9, true},
		{"above the upper bound", 100_001, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := validUpstream()
			u.MaxConcurrency = tc.value
			if tc.value == 100_001 {
				u.MaxConnsPerHost, u.MaxIdleConnsPerHost = 200_000, 200_000
			}
			if tc.wantErr {
				assert.Error(t, u.Validate())
			} else {
				assert.NoError(t, u.Validate())
			}
		})
	}
}
