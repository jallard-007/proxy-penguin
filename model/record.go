// Package model defines the shared data types used across proxy-penguin.
package model

import "time"

// RequestRecord represents a single proxied HTTP request captured by the proxy.
type RequestRecord struct {
	ID          int64     `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	Hostname    string    `json:"hostname"`
	Path        string    `json:"path"`
	QueryParams string    `json:"queryParams"`
	ClientIP    string    `json:"clientIp"`
	Status      int       `json:"status"`
	DurationMs  float64   `json:"durationMs"`
	UserAgent   string    `json:"userAgent"`
	Pending     bool      `json:"pending"`
}

// RecordEvent wraps a RequestRecord for the processing pipeline.
// For new (pending) records, IDReady is closed once the record ID has been
// assigned by the storage layer, allowing the caller to read Record.ID safely.
type RecordEvent struct {
	Record  *RequestRecord
	IDReady chan struct{} // nil for updates; closed after Insert sets Record.ID
}
