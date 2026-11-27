// Package webembed embeds the built management UI.
package webembed

import (
	"embed"
	"io/fs"
)

// The dist directory always contains a placeholder .keep so the package
// compiles before the frontend is built.
//
//go:embed all:dist
var assets embed.FS

// FS returns the embedded UI filesystem rooted at dist.
func FS() fs.FS {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil
	}
	return sub
}
