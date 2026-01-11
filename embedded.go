package main

import (
	"embed"
	"io/fs"
)

//go:embed web/* assets/*
var embeddedFiles embed.FS

func embeddedWebFS() (fs.FS, error) {
	return fs.Sub(embeddedFiles, "web")
}

func embeddedAssetsFS() (fs.FS, error) {
	return fs.Sub(embeddedFiles, "assets")
}

func embeddedAsset(name string) ([]byte, error) {
	return embeddedFiles.ReadFile("assets/" + name)
}
