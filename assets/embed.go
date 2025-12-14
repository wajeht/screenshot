package assets

import "embed"

//go:embed "filters/domains.json" "static" "templates" "migrations"
var EmbeddedFiles embed.FS
