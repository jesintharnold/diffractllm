// Package buildinfo holds facts stamped into the binary at build time:
//
//	go build -ldflags "-X diffractllm/internal/buildinfo.Version=0.4.2 -X diffractllm/internal/buildinfo.License=commercial"
package buildinfo

// Version is the release; unstamped builds report 0.0.0.
var Version = "0.0.0"

// License is the edition this binary was built as: LicenseFree or LicenseCommercial.
// It is not verified yet; there is no license key check.
var License = LicenseFree

const (
	LicenseFree       = "free"
	LicenseCommercial = "commercial"
)

type Info struct {
	Version string `json:"version"`
	License string `json:"license"`
}

func Get() Info {
	license := License
	if license != LicenseCommercial {
		license = LicenseFree
	}
	return Info{Version: Version, License: license}
}
