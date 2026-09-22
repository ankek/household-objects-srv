package config

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestLoadDefaultsToClosedOnEmptyEnvironment(t *testing.T) {
	unsetEnv(t, envRegistrationOpen)
	unsetEnv(t, envTrustProxyHeaders)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() on an empty environment: %v", err)
	}
	if cfg.RegistrationOpen {
		t.Error("RegistrationOpen = true on an empty environment, want false (FR-005 default)")
	}
	if cfg.TrustProxyHeaders {
		t.Error("TrustProxyHeaders = true on an empty environment, want false (T036's safe default)")
	}
}

func TestLoadParsesTrustProxyHeaders(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"false", false},
		{"0", false},
		{"", false},
	} {
		t.Run("raw="+tc.raw, func(t *testing.T) {
			t.Setenv(envTrustProxyHeaders, tc.raw)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() with %s=%q: %v", envTrustProxyHeaders, tc.raw, err)
			}
			if cfg.TrustProxyHeaders != tc.want {
				t.Errorf("Load() with %s=%q: TrustProxyHeaders = %v, want %v", envTrustProxyHeaders, tc.raw, cfg.TrustProxyHeaders, tc.want)
			}
		})
	}
}

func TestLoadRejectsMalformedTrustProxyHeaders(t *testing.T) {
	for _, raw := range []string{"yes", "enabled", "2"} {
		t.Run("raw="+raw, func(t *testing.T) {
			t.Setenv(envTrustProxyHeaders, raw)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with %s=%q returned no error, want a startup failure", envTrustProxyHeaders, raw)
			}
		})
	}
}

func TestLoadParsesRegistrationOpen(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"1", true},
		{"t", true},
		{"false", false},
		{"FALSE", false},
		{"False", false},
		{"0", false},
		{"f", false},
		{"", false},
	} {
		t.Run("raw="+tc.raw, func(t *testing.T) {
			t.Setenv(envRegistrationOpen, tc.raw)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() with %s=%q: %v", envRegistrationOpen, tc.raw, err)
			}
			if cfg.RegistrationOpen != tc.want {
				t.Errorf("Load() with %s=%q: RegistrationOpen = %v, want %v", envRegistrationOpen, tc.raw, cfg.RegistrationOpen, tc.want)
			}
		})
	}
}

func TestLoadRejectsMalformedRegistrationOpen(t *testing.T) {
	for _, raw := range []string{"yes", "open", "enabled", "TRUE ", " true", "2", "on"} {
		t.Run("raw="+raw, func(t *testing.T) {
			t.Setenv(envRegistrationOpen, raw)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with %s=%q returned no error, want a startup failure", envRegistrationOpen, raw)
			}
		})
	}
}

func TestLoadDefaultsDataDirToSlashData(t *testing.T) {
	unsetEnv(t, envDataDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() on an empty environment: %v", err)
	}
	if cfg.DataDir != DefaultDataDir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, DefaultDataDir)
	}
	if got, want := cfg.DatabasePath(), DefaultDataDir+"/db/hho.db"; got != want {
		t.Errorf("DatabasePath() = %q, want %q", got, want)
	}
}

func TestLoadReadsDataDirFromEnvironment(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"/srv/hho-data", "/srv/hho-data"},
		{"", DefaultDataDir},
	} {
		t.Run("raw="+tc.raw, func(t *testing.T) {
			t.Setenv(envDataDir, tc.raw)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() with %s=%q: %v", envDataDir, tc.raw, err)
			}
			if cfg.DataDir != tc.want {
				t.Errorf("Load() with %s=%q: DataDir = %q, want %q", envDataDir, tc.raw, cfg.DataDir, tc.want)
			}
		})
	}
}

func TestDatabasePathJoinsDataDirWithTheFixedSubpath(t *testing.T) {
	cfg := Config{DataDir: "/srv/hho-data"}
	if got, want := cfg.DatabasePath(), "/srv/hho-data/db/hho.db"; got != want {
		t.Errorf("DatabasePath() = %q, want %q", got, want)
	}
}

func TestLoadDefaultsMaxAttachmentBytesOnEmptyEnvironment(t *testing.T) {
	unsetEnv(t, envMaxAttachmentBytes)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() on an empty environment: %v", err)
	}
	if cfg.MaxAttachmentBytes != DefaultMaxAttachmentBytes {
		t.Errorf("MaxAttachmentBytes = %d, want %d (DefaultMaxAttachmentBytes)", cfg.MaxAttachmentBytes, DefaultMaxAttachmentBytes)
	}
}

func TestLoadParsesMaxAttachmentBytes(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int64
	}{
		{"10485760", 10 << 20},
		{"1", 1},
		{"", DefaultMaxAttachmentBytes},
	} {
		t.Run("raw="+tc.raw, func(t *testing.T) {
			t.Setenv(envMaxAttachmentBytes, tc.raw)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() with %s=%q: %v", envMaxAttachmentBytes, tc.raw, err)
			}
			if cfg.MaxAttachmentBytes != tc.want {
				t.Errorf("Load() with %s=%q: MaxAttachmentBytes = %d, want %d", envMaxAttachmentBytes, tc.raw, cfg.MaxAttachmentBytes, tc.want)
			}
		})
	}
}

func TestLoadRejectsMalformedMaxAttachmentBytes(t *testing.T) {
	for _, raw := range []string{"twenty-five-mib", "25MB", "1.5", "0x19000000"} {
		t.Run("raw="+raw, func(t *testing.T) {
			t.Setenv(envMaxAttachmentBytes, raw)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with %s=%q returned no error, want a startup failure", envMaxAttachmentBytes, raw)
			}
		})
	}
}

func TestLoadRejectsNonPositiveMaxAttachmentBytes(t *testing.T) {
	for _, raw := range []string{"0", "-1", "-26214400"} {
		t.Run("raw="+raw, func(t *testing.T) {
			t.Setenv(envMaxAttachmentBytes, raw)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with %s=%q returned no error, want a startup failure (non-positive byte limit)", envMaxAttachmentBytes, raw)
			}
		})
	}
}

func TestLoadDefaultsAttachmentReclaimIntervalSecondsOnEmptyEnvironment(t *testing.T) {
	unsetEnv(t, envAttachmentReclaimIntervalSeconds)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() on an empty environment: %v", err)
	}
	if cfg.AttachmentReclaimIntervalSeconds != DefaultAttachmentReclaimIntervalSeconds {
		t.Errorf("AttachmentReclaimIntervalSeconds = %d, want %d (DefaultAttachmentReclaimIntervalSeconds)",
			cfg.AttachmentReclaimIntervalSeconds, DefaultAttachmentReclaimIntervalSeconds)
	}
}

func TestLoadParsesAttachmentReclaimIntervalSecondsIncludingDisabled(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int64
	}{
		{"60", 60},
		{"0", 0},
		{"-1", -1},
		{"", DefaultAttachmentReclaimIntervalSeconds},
	} {
		t.Run("raw="+tc.raw, func(t *testing.T) {
			t.Setenv(envAttachmentReclaimIntervalSeconds, tc.raw)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() with %s=%q: %v", envAttachmentReclaimIntervalSeconds, tc.raw, err)
			}
			if cfg.AttachmentReclaimIntervalSeconds != tc.want {
				t.Errorf("Load() with %s=%q: AttachmentReclaimIntervalSeconds = %d, want %d",
					envAttachmentReclaimIntervalSeconds, tc.raw, cfg.AttachmentReclaimIntervalSeconds, tc.want)
			}
		})
	}
}

func TestLoadRejectsMalformedAttachmentReclaimIntervalSeconds(t *testing.T) {
	for _, raw := range []string{"one-hour", "1h", "3600s", "1.5"} {
		t.Run("raw="+raw, func(t *testing.T) {
			t.Setenv(envAttachmentReclaimIntervalSeconds, raw)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with %s=%q returned no error, want a startup failure", envAttachmentReclaimIntervalSeconds, raw)
			}
		})
	}
}

func TestLoadDefaultsMinClientVersionToUnsetOnEmptyEnvironment(t *testing.T) {
	unsetEnv(t, envMinClientVersion)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() on an empty environment: %v", err)
	}
	if cfg.MinClientVersion != "" {
		t.Errorf("MinClientVersion = %q, want \"\" (no minimum)", cfg.MinClientVersion)
	}
}

func TestLoadParsesMinClientVersion(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"1.4.2", "1.4.2"},
		{"0.0.0", "0.0.0"},
		{"", ""},
	} {
		t.Run("raw="+tc.raw, func(t *testing.T) {
			t.Setenv(envMinClientVersion, tc.raw)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() with %s=%q: %v", envMinClientVersion, tc.raw, err)
			}
			if cfg.MinClientVersion != tc.want {
				t.Errorf("Load() with %s=%q: MinClientVersion = %q, want %q", envMinClientVersion, tc.raw, cfg.MinClientVersion, tc.want)
			}
		})
	}
}

func TestLoadRejectsMalformedMinClientVersion(t *testing.T) {
	for _, raw := range []string{"abc", "1.2", "1.2.3.4", "1.-2.3", "-1.2.3", "1.2.3-rc1", "v1.2.3", "1..3", "1.2."} {
		t.Run("raw="+raw, func(t *testing.T) {
			t.Setenv(envMinClientVersion, raw)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with %s=%q returned no error, want a startup failure", envMinClientVersion, raw)
			}
		})
	}
}

func TestLoadDefaultsScheduledBackupIntervalSecondsOnEmptyEnvironment(t *testing.T) {
	unsetEnv(t, envScheduledBackupIntervalSeconds)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() on an empty environment: %v", err)
	}
	if cfg.ScheduledBackupIntervalSeconds != DefaultScheduledBackupIntervalSeconds {
		t.Errorf("ScheduledBackupIntervalSeconds = %d, want %d (DefaultScheduledBackupIntervalSeconds)",
			cfg.ScheduledBackupIntervalSeconds, DefaultScheduledBackupIntervalSeconds)
	}
	if cfg.ScheduledBackupIntervalSeconds <= 0 {
		t.Errorf("ScheduledBackupIntervalSeconds = %d, want a positive default so FR-134's pass is enabled out of the box",
			cfg.ScheduledBackupIntervalSeconds)
	}
}

func TestLoadParsesScheduledBackupIntervalSecondsIncludingDisabled(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int64
	}{
		{"3600", 3600},
		{"0", 0},
		{"-1", -1},
		{"", DefaultScheduledBackupIntervalSeconds},
	} {
		t.Run("raw="+tc.raw, func(t *testing.T) {
			t.Setenv(envScheduledBackupIntervalSeconds, tc.raw)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() with %s=%q: %v", envScheduledBackupIntervalSeconds, tc.raw, err)
			}
			if cfg.ScheduledBackupIntervalSeconds != tc.want {
				t.Errorf("Load() with %s=%q: ScheduledBackupIntervalSeconds = %d, want %d",
					envScheduledBackupIntervalSeconds, tc.raw, cfg.ScheduledBackupIntervalSeconds, tc.want)
			}
		})
	}
}

func TestLoadRejectsMalformedScheduledBackupIntervalSeconds(t *testing.T) {
	for _, raw := range []string{"one-day", "24h", "86400s", "1.5"} {
		t.Run("raw="+raw, func(t *testing.T) {
			t.Setenv(envScheduledBackupIntervalSeconds, raw)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with %s=%q returned no error, want a startup failure", envScheduledBackupIntervalSeconds, raw)
			}
		})
	}
}

func TestLoadDefaultsScheduledBackupRetentionCountOnEmptyEnvironment(t *testing.T) {
	unsetEnv(t, envScheduledBackupRetentionCount)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() on an empty environment: %v", err)
	}
	if cfg.ScheduledBackupRetentionCount != DefaultScheduledBackupRetentionCount {
		t.Errorf("ScheduledBackupRetentionCount = %d, want %d (DefaultScheduledBackupRetentionCount)",
			cfg.ScheduledBackupRetentionCount, DefaultScheduledBackupRetentionCount)
	}
}

func TestLoadParsesScheduledBackupRetentionCount(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int64
	}{
		{"1", 1},
		{"30", 30},
		{"", DefaultScheduledBackupRetentionCount},
	} {
		t.Run("raw="+tc.raw, func(t *testing.T) {
			t.Setenv(envScheduledBackupRetentionCount, tc.raw)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() with %s=%q: %v", envScheduledBackupRetentionCount, tc.raw, err)
			}
			if cfg.ScheduledBackupRetentionCount != tc.want {
				t.Errorf("Load() with %s=%q: ScheduledBackupRetentionCount = %d, want %d",
					envScheduledBackupRetentionCount, tc.raw, cfg.ScheduledBackupRetentionCount, tc.want)
			}
		})
	}
}

func TestLoadRejectsNonPositiveScheduledBackupRetentionCount(t *testing.T) {
	for _, raw := range []string{"0", "-1", "-7"} {
		t.Run("raw="+raw, func(t *testing.T) {
			t.Setenv(envScheduledBackupRetentionCount, raw)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with %s=%q returned no error, want a startup failure (non-positive retention count)", envScheduledBackupRetentionCount, raw)
			}
		})
	}
}

func TestLoadRejectsMalformedScheduledBackupRetentionCount(t *testing.T) {
	for _, raw := range []string{"seven", "7.5", "0x7"} {
		t.Run("raw="+raw, func(t *testing.T) {
			t.Setenv(envScheduledBackupRetentionCount, raw)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with %s=%q returned no error, want a startup failure", envScheduledBackupRetentionCount, raw)
			}
		})
	}
}

func TestLoadDefaultsPublicBaseURLToUnsetOnEmptyEnvironment(t *testing.T) {
	unsetEnv(t, envPublicBaseURL)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() on an empty environment: %v", err)
	}
	if cfg.PublicBaseURL != "" {
		t.Errorf("PublicBaseURL = %q, want \"\" (unset, per-request derivation)", cfg.PublicBaseURL)
	}
}

func TestLoadParsesPublicBaseURL(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"https://example.com", "https://example.com"},
		{"https://example.com/", "https://example.com"},
		{"http://example.com:8080", "http://example.com:8080"},
		{"https://example.com/hho", "https://example.com/hho"},
		{"https://example.com/hho/", "https://example.com/hho"},
		{"HTTPS://example.com", "https://example.com"},
		{"", ""},
	} {
		t.Run("raw="+tc.raw, func(t *testing.T) {
			t.Setenv(envPublicBaseURL, tc.raw)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() with %s=%q: %v", envPublicBaseURL, tc.raw, err)
			}
			if cfg.PublicBaseURL != tc.want {
				t.Errorf("Load() with %s=%q: PublicBaseURL = %q, want %q", envPublicBaseURL, tc.raw, cfg.PublicBaseURL, tc.want)
			}
		})
	}
}

func TestLoadRejectsMalformedPublicBaseURL(t *testing.T) {
	for _, raw := range []string{
		"example.com",
		"ftp://example.com",
		"https://",
		"https:///path",
		"https://example.com?x=1",
		"https://example.com#section",
		"https://user:pass@example.com",
		"://example.com",
		" https://example.com",
	} {
		t.Run("raw="+raw, func(t *testing.T) {
			t.Setenv(envPublicBaseURL, raw)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with %s=%q returned no error, want a startup failure", envPublicBaseURL, raw)
			}
		})
	}
}

func TestResolvePublicBaseURLUsesOverrideUnconditionally(t *testing.T) {
	cfg := Config{PublicBaseURL: "https://override.example.com/hho", TrustProxyHeaders: true}

	r := httptest.NewRequest(http.MethodGet, "/i/some-id", nil)
	r.Host = "internal-host:9999"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "attacker.example.com")

	if got, want := cfg.ResolvePublicBaseURL(r), "https://override.example.com/hho"; got != want {
		t.Errorf("ResolvePublicBaseURL() = %q, want %q (override must win)", got, want)
	}
}

func TestResolvePublicBaseURLDefaultsFromRequestHostAndTLS(t *testing.T) {
	for _, tc := range []struct {
		name string
		tls  bool
		host string
		want string
	}{
		{"plain http", false, "hho.local:7745", "http://hho.local:7745"},
		{"tls terminated at this process", true, "hho.example.com", "https://hho.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{}
			r := httptest.NewRequest(http.MethodGet, "/i/some-id", nil)
			r.Host = tc.host
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}

			if got := cfg.ResolvePublicBaseURL(r); got != tc.want {
				t.Errorf("ResolvePublicBaseURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolvePublicBaseURLIgnoresForwardedHeadersUnlessTrusted(t *testing.T) {
	cfg := Config{TrustProxyHeaders: false}
	r := httptest.NewRequest(http.MethodGet, "/i/some-id", nil)
	r.Host = "internal-host:9999"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "public.example.com")

	if got, want := cfg.ResolvePublicBaseURL(r), "http://internal-host:9999"; got != want {
		t.Errorf("ResolvePublicBaseURL() = %q, want %q (forwarded headers must be ignored when TrustProxyHeaders is false)", got, want)
	}
}

func TestResolvePublicBaseURLHonoursForwardedHeadersWhenTrusted(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proto string
		host  string
		want  string
	}{
		{"single hop", "https", "public.example.com", "https://public.example.com"},
		{"multi-hop takes the last entry", "http, https", "10.0.0.5, public.example.com", "https://public.example.com"},
		{"proto is case-insensitive", "HTTPS", "public.example.com", "https://public.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{TrustProxyHeaders: true}
			r := httptest.NewRequest(http.MethodGet, "/i/some-id", nil)
			r.Host = "internal-host:9999"
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			r.Header.Set("X-Forwarded-Host", tc.host)

			if got := cfg.ResolvePublicBaseURL(r); got != tc.want {
				t.Errorf("ResolvePublicBaseURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolvePublicBaseURLIgnoresMalformedForwardedHeaders(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proto string
		host  string
	}{
		{"unrecognised proto scheme", "ftp", ""},
		{"forwarded host carries a path", "", "public.example.com/evil"},
		{"forwarded host carries a query string", "", "public.example.com?x=1"},
		{"forwarded host contains a header-injection attempt", "", "public.example.com\r\nX-Injected: 1"},
		{"forwarded host is only whitespace", "", "   "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{TrustProxyHeaders: true}
			r := httptest.NewRequest(http.MethodGet, "/i/some-id", nil)
			r.Host = "internal-host:9999"
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			if tc.host != "" {
				r.Header.Set("X-Forwarded-Host", tc.host)
			}

			want := "http://internal-host:9999"
			if got := cfg.ResolvePublicBaseURL(r); got != want {
				t.Errorf("ResolvePublicBaseURL() = %q, want %q (malformed forwarded header must be ignored)", got, want)
			}
		})
	}
}

func unsetEnv(t *testing.T, name string) {
	t.Helper()
	prev, had := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if !had {
			return
		}
		if err := os.Setenv(name, prev); err != nil {
			t.Fatalf("restore %s: %v", name, err)
		}
	})
}
