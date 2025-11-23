package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist/*
var uiAssets embed.FS

func uiHandler() http.Handler {
	fsys, err := fs.Sub(uiAssets, "dist")
	if err != nil {
		// If dist is missing (e.g. during dev), return 404 or empty handler
		return http.NotFoundHandler()
	}
	return http.FileServer(http.FS(fsys))
}
