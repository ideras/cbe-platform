//go:build !bindings

package student

import "embed"

//go:embed all:frontend/dist
var Assets embed.FS
