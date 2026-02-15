package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	AccessToken string `json:"access_token"`
	APIHost     string `json:"api_host"`
	APIVersion  string `json:"api_version"`
	Timeout     string `json:"timeout"` // duration, e.g. "60s"
	Retries     int    `json:"retries"`
}

func Default() Config {
	return Config{
		APIHost:    "dataapi.uiscom.ru",
		APIVersion: "v2.0",
		Timeout:    "60s",
		Retries:    3,
	}
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "uis-cli", "config.json"), nil
}

func Load(path string) (Config, error) {
	cfg := Default()

	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}

	var onDisk Config
	if err := json.Unmarshal(b, &onDisk); err != nil {
		// Fail closed: if config is corrupt, we want a loud error rather than silently
		// running with unexpected defaults.
		return cfg, err
	}

	// Merge: zero-values keep defaults.
	if onDisk.AccessToken != "" {
		cfg.AccessToken = onDisk.AccessToken
	}
	if onDisk.APIHost != "" {
		cfg.APIHost = onDisk.APIHost
	}
	if onDisk.APIVersion != "" {
		cfg.APIVersion = onDisk.APIVersion
	}
	if onDisk.Timeout != "" {
		cfg.Timeout = onDisk.Timeout
	}
	if onDisk.Retries != 0 {
		cfg.Retries = onDisk.Retries
	}

	return cfg, nil
}

func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	// Best-effort permissions: keep token readable only by the user.
	_ = os.Remove(path)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	return nil
}

func (c Config) TimeoutDuration() (time.Duration, error) {
	if c.Timeout == "" {
		return 60 * time.Second, nil
	}
	return time.ParseDuration(c.Timeout)
}
