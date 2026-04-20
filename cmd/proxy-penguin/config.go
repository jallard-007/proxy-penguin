package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Config holds the top-level application configuration loaded from config.json.
type Config struct {
	Addr                 string            `json:"addr"`
	DataHome             string            `json:"dataHome"`
	Routes               map[string]string `json:"routes"`
	DashboardHost        string            `json:"dashboardHost"`
	ApiPassword          string            `json:"apiPassword"`
	MaxStreamConnections int64             `json:"maxStreamConnections"`
}

func loadConfig(path string) (Config, error) {
	cfg := Config{
		Addr:     ":8080",
		DataHome: ".",
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid config %s: %w", path, err)
	}

	return cfg, nil
}
