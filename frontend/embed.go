// Package frontend embeds the static web assets served by the dashboard.
package frontend

import (
	"embed"
	"io/fs"
)

// dist contains the embedded built frontend assets.
//
//go:embed dist/*
var dist embed.FS

// FS is the filesystem containing the built frontend assets, rooted at dist/.
var FS fs.FS

func init() {
	var err error
	FS, err = fs.Sub(dist, "dist")
	if err != nil {
		panic("frontend: failed to create sub filesystem: " + err.Error())
	}
}
