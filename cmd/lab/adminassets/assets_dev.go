//go:build !admin_ui

package adminassets

import "io/fs"

func Built() (fs.FS, bool) { return nil, false }
