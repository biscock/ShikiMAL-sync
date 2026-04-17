package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	defaultDataDir = "data"
	defaultAppName = "ShikiMAL Sync"
)

type ProviderConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURL  string `json:"redirect_url"`
}

type Config struct {
	AppName      string         `json:"app_name"`
	PollInterval string         `json:"poll_interval"`
	DataDir      string         `json:"data_dir"`
	Shikimori    ProviderConfig `json:"shikimori"`
	MyAnimeList  ProviderConfig `json:"myanimelist"`

	rootDir string
}

func Load(path string) (*Config, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.rootDir = filepath.Dir(absPath)
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.AppName == "" {
		c.AppName = defaultAppName
	}
	if c.DataDir == "" {
		c.DataDir = defaultDataDir
	}
}

func (c *Config) Validate() error {
	if c.PollInterval == "" {
		return errors.New("poll_interval is required")
	}
	if c.Shikimori.ClientID == "" || c.Shikimori.ClientSecret == "" || c.Shikimori.RedirectURL == "" {
		return errors.New("shikimori.client_id, shikimori.client_secret and shikimori.redirect_url are required")
	}
	if c.MyAnimeList.ClientID == "" || c.MyAnimeList.RedirectURL == "" {
		return errors.New("myanimelist.client_id and myanimelist.redirect_url are required")
	}
	if _, err := c.PollDuration(); err != nil {
		return fmt.Errorf("invalid poll_interval: %w", err)
	}
	return nil
}

func (c *Config) PollDuration() (time.Duration, error) {
	d, err := time.ParseDuration(c.PollInterval)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, errors.New("must be greater than zero")
	}
	return d, nil
}

func (c *Config) ResolvePath(parts ...string) string {
	all := append([]string{c.rootDir, c.DataDir}, parts...)
	return filepath.Join(all...)
}

func (c *Config) StatePath() string {
	return c.ResolvePath("state.json")
}

func (c *Config) ShikimoriTokenPath() string {
	return c.ResolvePath("tokens", "shikimori.json")
}

func (c *Config) MALTokenPath() string {
	return c.ResolvePath("tokens", "mal.json")
}
