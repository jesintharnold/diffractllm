package server

import (
	"net/http"
	"testing"

	"diffractllm/internal/buildinfo"

	"github.com/stretchr/testify/assert"
)

// /v1/info is public (no admin prefix) and reports the stamped build.
func TestInfoReportsVersionAndLicense(t *testing.T) {
	defer func(v, l string) { buildinfo.Version, buildinfo.License = v, l }(buildinfo.Version, buildinfo.License)
	buildinfo.Version, buildinfo.License = "0.4.2", buildinfo.LicenseCommercial

	code, body := get(t, metricsServer(t, false), "/v1/info")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, map[string]any{"version": "0.4.2", "license": "commercial"}, body)
}
