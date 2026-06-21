package gork

import (
	"embed"
	"io/fs"
)

//go:embed config.defaults.toml app/statics
var embeddedFiles embed.FS

func DefaultConfigTOML() ([]byte, error) {
	return embeddedFiles.ReadFile("config.defaults.toml")
}

func StaticFile(name string) ([]byte, error) {
	return embeddedFiles.ReadFile("app/statics/" + name)
}

func StaticFS() fs.FS {
	staticFS, err := fs.Sub(embeddedFiles, "app/statics")
	if err != nil {
		return embeddedFiles
	}
	return staticFS
}
