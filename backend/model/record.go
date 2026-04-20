// Package model defines the shared data types used across proxy-penguin.
package model

import (
	"time"
)

// RequestRecord represents a single completed proxied HTTP request captured by the proxy.
type Request struct {
	RequestStart
	Status     int     `json:"s"`
	DurationMs float64 `json:"d"`
}

type RequestStart struct {
	ID          int64     `json:"i"`
	Timestamp   time.Time `json:"t"`
	Hostname    string    `json:"h"`
	Path        string    `json:"p"`
	QueryParams string    `json:"qp"`
	ClientIP    string    `json:"cip"`
	UserAgent   string    `json:"ua"`
}

// RecordEvent wraps a RequestRecord for the processing pipeline.
// For new (pending) records, IDReady is closed once the record ID has been
// assigned by the storage layer, allowing the caller to read Record.ID safely.
type RecordEvent struct {
	Type   RecordEventType
	Record any
}

type RecordEventType string

const (
	RecordEventTypeRequest      RecordEventType = "r"
	RecordEventTypeRequestStart RecordEventType = "rs"
	RecordEventTypeRequestDone  RecordEventType = "rd"
)
