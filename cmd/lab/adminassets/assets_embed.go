//go:build admin_ui

package adminassets

import (
	"embed"
	"io/fs"
)

//go:embed dist
var assets embed.FS

func Built() (fs.FS, bool) {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return sub, true
}
