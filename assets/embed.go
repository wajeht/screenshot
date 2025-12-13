package assets

import "embed"

//go:embed "filters" "static"
var EmbeddedFiles embed.FS
