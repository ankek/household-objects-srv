package config

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func publicBaseURLEnv(name string) (string, error) {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("config: %s=%q is not a valid URL: %w", name, raw, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("config: %s=%q must be an absolute http:// or https:// URL", name, raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("config: %s=%q must include a host", name, raw)
	}
	if u.User != nil {
		return "", fmt.Errorf("config: %s=%q must not include userinfo", name, raw)
	}
	if u.RawQuery != "" {
		return "", fmt.Errorf("config: %s=%q must not include a query string", name, raw)
	}
	if u.Fragment != "" || u.RawFragment != "" {
		return "", fmt.Errorf("config: %s=%q must not include a fragment", name, raw)
	}
	path := strings.TrimRight(u.Path, "/")
	return scheme + "://" + u.Host + path, nil
}

func (c Config) ResolvePublicBaseURL(r *http.Request) string {
	if c.PublicBaseURL != "" {
		return c.PublicBaseURL
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host

	if c.TrustProxyHeaders {
		if raw, ok := lastForwardedValue(r.Header.Get("X-Forwarded-Proto")); ok {
			if p := strings.ToLower(raw); p == "http" || p == "https" {
				scheme = p
			}
		}
		if raw, ok := lastForwardedValue(r.Header.Get("X-Forwarded-Host")); ok && isValidForwardedHost(raw) {
			host = raw
		}
	}

	return scheme + "://" + host
}

func lastForwardedValue(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	parts := strings.Split(raw, ",")
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "" {
		return "", false
	}
	return last, true
}

func isValidForwardedHost(host string) bool {
	if host == "" {
		return false
	}
	u, err := url.Parse("http://" + host + "/")
	return err == nil && u.Host == host
}
