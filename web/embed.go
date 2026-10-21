package web

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
)

//go:embed static/*
var staticFS embed.FS

func init() {
	// Register essential MIME types explicitly so that embedded assets
	// serve with correct Content-Type headers even in minimal container environments (Alpine/Distroless).
	_ = mime.AddExtensionType(".html", "text/html; charset=utf-8")
	_ = mime.AddExtensionType(".css", "text/css; charset=utf-8")
	_ = mime.AddExtensionType(".js", "application/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".json", "application/json; charset=utf-8")
	_ = mime.AddExtensionType(".svg", "image/svg+xml")
	_ = mime.AddExtensionType(".png", "image/png")
	_ = mime.AddExtensionType(".ico", "image/x-icon")
}

// AssetHandler returns an http.Handler that serves embedded static dashboard files.
// If diskDir is non-empty, it serves files directly from the specified disk directory (enabling hot-reloading).
// If diskDir is empty, it serves files from the binary's embedded embed.FS.
// The handler serves "/" with index.html, supports paths under "/static/", and falls back to index.html for SPA routes.
func AssetHandler(diskDir string) http.Handler {
	var fileSystem http.FileSystem
	var fsRoot fs.FS

	if diskDir != "" {
		fileSystem = http.Dir(diskDir)
		fsRoot = os.DirFS(diskDir)
	} else {
		sub, err := fs.Sub(staticFS, "static")
		if err != nil {
			fsRoot = staticFS
			fileSystem = http.FS(staticFS)
		} else {
			fsRoot = sub
			fileSystem = http.FS(sub)
		}
	}

	fileServer := http.FileServer(fileSystem)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean and normalize requested path
		upath := r.URL.Path
		if !strings.HasPrefix(upath, "/") {
			upath = "/" + upath
		}
		upath = path.Clean(upath)

		// Strip /static prefix if present
		trimmedPath := strings.TrimPrefix(upath, "/static")
		if trimmedPath == "" {
			trimmedPath = "/"
		}

		// Root request -> serve index.html with no-cache headers to ensure immediate UI updates
		if trimmedPath == "/" || trimmedPath == "/index.html" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}

		// Check if physical file exists in the asset filesystem
		cleanRel := strings.TrimPrefix(trimmedPath, "/")
		if fileExists(fsRoot, cleanRel) {
			r.URL.Path = trimmedPath
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA Fallback: If not an API route and not a physical file, serve index.html
		if !strings.HasPrefix(upath, "/api/") {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}

		http.NotFound(w, r)
	})
}

// fileExists checks whether a physical file exists and is not a directory.
func fileExists(f fs.FS, name string) bool {
	if f == nil || name == "" {
		return false
	}
	stat, err := fs.Stat(f, name)
	if err != nil || stat.IsDir() {
		return false
	}
	return true
}
