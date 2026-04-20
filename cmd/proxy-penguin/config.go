package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config holds the top-level application configuration loaded from config.json.
type Config struct {
	Addr          string            `json:"addr"`
	DBPath        string            `json:"dbPath"`
	Routes        map[string]string `json:"routes"`
	DashboardHost string            `json:"dashboardHost"`
	ApiPassword   string            `json:"apiPassword"`
}

func loadConfig(path string) (Config, error) {
	cfg := Config{
		Addr:   ":8080",
		DBPath: "proxy-penguin.db",
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid config %s: %w", path, err)
	}

	return cfg, nil
}
