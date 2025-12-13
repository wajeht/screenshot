package assets

import "embed"

//go:embed "filters/domains.json" "static"
var EmbeddedFiles embed.FS
