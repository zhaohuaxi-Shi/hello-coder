package web

import "embed"

// Static holds the login UI and future frontend assets.
//
//go:embed all:static
var Static embed.FS
