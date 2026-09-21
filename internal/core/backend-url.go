package core

import "net/url"

func SanitizeBackendURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.RawQuery = ""
	u.ForceQuery = false
	u.User = nil
	u.Fragment = ""
	return u.String()
}
