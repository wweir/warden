package web

import "embed"

//go:embed admin/dist/*.br admin/dist/assets/*.br
var AdminFS embed.FS
