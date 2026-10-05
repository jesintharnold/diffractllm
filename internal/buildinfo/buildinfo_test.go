package buildinfo

import "testing"

func TestGetReportsStampedValues(t *testing.T) {
	defer func(v, l string) { Version, License = v, l }(Version, License)

	Version, License = "0.4.2", LicenseCommercial
	if got := Get(); got != (Info{Version: "0.4.2", License: "commercial"}) {
		t.Fatalf("got %+v", got)
	}
}

// A typo in the -X stamp must not advertise an edition that does not exist.
func TestUnknownLicenseReadsAsFree(t *testing.T) {
	defer func(l string) { License = l }(License)

	License = "enterprise"
	if got := Get().License; got != LicenseFree {
		t.Fatalf("got %q, want free", got)
	}
}
