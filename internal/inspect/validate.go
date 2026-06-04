package inspect

import (
	"errors"
	"net/url"
	"strings"
)

// ValidateTraceUIBaseURL ensures --trace-ui is an http(s) base without a javascript: footgun.
func ValidateTraceUIBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("inspect: invalid --trace-ui URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("inspect: --trace-ui must use http or https")
	}
	if u.Host == "" {
		return "", errors.New("inspect: --trace-ui must include a host")
	}
	return strings.TrimRight(raw, "/"), nil
}
