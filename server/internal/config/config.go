package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	RegistrationOpen bool

	TrustProxyHeaders bool

	DataDir string

	Addr string

	MaxAttachmentBytes int64

	AttachmentReclaimIntervalSeconds int64

	MinClientVersion string

	ScheduledBackupIntervalSeconds int64

	ScheduledBackupRetentionCount int64

	PublicBaseURL string
}

const envRegistrationOpen = "HHO_REGISTRATION_OPEN"

const envTrustProxyHeaders = "HHO_TRUST_PROXY_HEADERS"

const envDataDir = "HHO_DATA_DIR"

const DefaultDataDir = "/data"

const envAddr = "HHO_ADDR"

const DefaultAddr = ":7745"

const envMaxAttachmentBytes = "HHO_MAX_ATTACHMENT_SIZE_BYTES"

const DefaultMaxAttachmentBytes int64 = 25 << 20

const envAttachmentReclaimIntervalSeconds = "HHO_ATTACHMENT_RECLAIM_INTERVAL_SECONDS"

const DefaultAttachmentReclaimIntervalSeconds int64 = 3600

const envMinClientVersion = "HHO_MIN_CLIENT_VERSION"

const envScheduledBackupIntervalSeconds = "HHO_BACKUP_INTERVAL_SECONDS"

const DefaultScheduledBackupIntervalSeconds int64 = 86400

const envScheduledBackupRetentionCount = "HHO_BACKUP_RETENTION_COUNT"

const DefaultScheduledBackupRetentionCount int64 = 7

const envPublicBaseURL = "HHO_PUBLIC_BASE_URL"

const databaseSubpath = "db/hho.db"

func Load() (Config, error) {
	registrationOpen, err := boolEnv(envRegistrationOpen, false)
	if err != nil {
		return Config{}, err
	}
	trustProxyHeaders, err := boolEnv(envTrustProxyHeaders, false)
	if err != nil {
		return Config{}, err
	}
	dataDir := stringEnv(envDataDir, DefaultDataDir)
	addr := stringEnv(envAddr, DefaultAddr)
	maxAttachmentBytes, err := int64Env(envMaxAttachmentBytes, DefaultMaxAttachmentBytes)
	if err != nil {
		return Config{}, err
	}
	attachmentReclaimIntervalSeconds, err := intervalSecondsEnv(envAttachmentReclaimIntervalSeconds, DefaultAttachmentReclaimIntervalSeconds)
	if err != nil {
		return Config{}, err
	}
	minClientVersion, err := clientVersionEnv(envMinClientVersion)
	if err != nil {
		return Config{}, err
	}
	scheduledBackupIntervalSeconds, err := intervalSecondsEnv(envScheduledBackupIntervalSeconds, DefaultScheduledBackupIntervalSeconds)
	if err != nil {
		return Config{}, err
	}
	scheduledBackupRetentionCount, err := positiveCountEnv(envScheduledBackupRetentionCount, DefaultScheduledBackupRetentionCount)
	if err != nil {
		return Config{}, err
	}
	publicBaseURL, err := publicBaseURLEnv(envPublicBaseURL)
	if err != nil {
		return Config{}, err
	}
	return Config{
		RegistrationOpen:                 registrationOpen,
		TrustProxyHeaders:                trustProxyHeaders,
		DataDir:                          dataDir,
		Addr:                             addr,
		MaxAttachmentBytes:               maxAttachmentBytes,
		AttachmentReclaimIntervalSeconds: attachmentReclaimIntervalSeconds,
		MinClientVersion:                 minClientVersion,
		ScheduledBackupIntervalSeconds:   scheduledBackupIntervalSeconds,
		ScheduledBackupRetentionCount:    scheduledBackupRetentionCount,
		PublicBaseURL:                    publicBaseURL,
	}, nil
}

func (c Config) DatabasePath() string {
	return filepath.Join(c.DataDir, databaseSubpath)
}

func boolEnv(name string, def bool) (bool, error) {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("config: %s=%q is not a valid boolean (use true/false/1/0): %w", name, raw, err)
	}
	return v, nil
}

func stringEnv(name, def string) string {
	if raw, ok := os.LookupEnv(name); ok && raw != "" {
		return raw
	}
	return def
}

func int64Env(name string, def int64) (int64, error) {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s=%q is not a valid integer: %w", name, raw, err)
	}
	if v <= 0 {
		return 0, fmt.Errorf("config: %s=%q must be a positive number of bytes", name, raw)
	}
	return v, nil
}

func positiveCountEnv(name string, def int64) (int64, error) {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s=%q is not a valid integer: %w", name, raw, err)
	}
	if v <= 0 {
		return 0, fmt.Errorf("config: %s=%q must be a positive count", name, raw)
	}
	return v, nil
}

func intervalSecondsEnv(name string, def int64) (int64, error) {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s=%q is not a valid integer: %w", name, raw, err)
	}
	return v, nil
}

func clientVersionEnv(name string) (string, error) {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return "", nil
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("config: %s=%q is not MAJOR.MINOR.PATCH (want exactly 3 dot-separated components, got %d)", name, raw, len(parts))
	}
	for _, part := range parts {
		if part == "" {
			return "", fmt.Errorf("config: %s=%q is not MAJOR.MINOR.PATCH (an empty component)", name, raw)
		}
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return "", fmt.Errorf("config: %s=%q is not MAJOR.MINOR.PATCH (component %q is not a non-negative integer)", name, raw, part)
			}
		}
	}
	return raw, nil
}
