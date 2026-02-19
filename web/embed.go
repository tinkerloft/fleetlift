// Package web provides the embedded web UI assets.
package web

import "embed"

//go:embed dist
var DistFS embed.FS
