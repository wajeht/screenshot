package assets

import "embed"

//go:embed "filters/domains.json" "static" "templates"
var EmbeddedFiles embed.FS
