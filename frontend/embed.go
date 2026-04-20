// Package frontend embeds the static web assets served by the dashboard.
package frontend

import "embed"

// FS contains the embedded dashboard static files.
//
//go:embed index.html
var FS embed.FS
