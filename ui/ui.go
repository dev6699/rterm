package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:src
var assets embed.FS

func Assets() (fs.FS, error) {
	return fs.Sub(assets, "src")
}
