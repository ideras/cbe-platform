//go:build !bindings

package proctor

import "embed"

//go:embed all:frontend/dist
var Assets embed.FS
