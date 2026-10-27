//go:build bindings

package proctor

import "embed"

// Binding generation must work before frontend/dist exists on a clean checkout.
var Assets embed.FS
