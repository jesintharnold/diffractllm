package providers

import "net/url"

func SanitizeProviderEndpoint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.RawQuery = ""
	u.User = nil
	return u.String()
}
