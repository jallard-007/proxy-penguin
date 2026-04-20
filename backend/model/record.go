// Package model defines the shared data types used across proxy-penguin.
package model

// RequestRecord represents a single completed proxied HTTP request captured by the proxy.
type Request struct {
	ID          int64  `json:"i"`
	Timestamp   int64  `json:"t"`
	Hostname    string `json:"h"`
	Path        string `json:"p"`
	QueryParams string `json:"qp"`
	ClientIP    string `json:"cip"`
	UserAgent   string `json:"ua"`
	Status      int64  `json:"s"`
	DurationMs  int64  `json:"d"`
}

// RecordEvent wraps a RequestRecord for the processing pipeline.
// For new (pending) records, IDReady is closed once the record ID has been
// assigned by the storage layer, allowing the caller to read Record.ID safely.
type RecordEvent struct {
	Type   RecordEventType
	Record Request
}

type RecordEventType string

const (
	RecordEventTypeRequest      RecordEventType = "r"
	RecordEventTypeRequestStart RecordEventType = "rs"
	RecordEventTypeRequestDone  RecordEventType = "rd"
)
