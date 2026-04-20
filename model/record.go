// Package model defines the shared data types used across proxy-penguin.
package model

import "time"

// RequestRecord represents a single proxied HTTP request captured by the proxy.
type RequestRecord struct {
	ID         int64     `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Hostname   string    `json:"hostname"`
	Path       string    `json:"path"`
	ClientIP   string    `json:"clientIp"`
	Status     int       `json:"status"`
	DurationMs float64   `json:"durationMs"`
}
