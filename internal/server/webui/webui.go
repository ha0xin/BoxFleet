package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed assets
var assets embed.FS

func Handler(mountPath string) http.Handler {
	root, err := fs.Sub(assets, "assets/generated")
	if err != nil {
		panic(err)
	}
	mountPath = "/" + strings.Trim(strings.TrimSpace(mountPath), "/")
	if mountPath == "/" {
		mountPath = "/admin"
	}
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, mountPath)
		if path == "" || path == "/" {
			serveIndex(root, mountPath, w)
			return
		}
		name := strings.TrimPrefix(path, "/")
		if file, err := root.Open(name); err == nil {
			_ = file.Close()
			serveWebFile(fileServer, r, w, "/"+name)
			return
		}
		serveIndex(root, mountPath, w)
	})
}

func serveIndex(root fs.FS, mountPath string, w http.ResponseWriter) {
	raw, err := fs.ReadFile(root, "index.html")
	if err != nil {
		http.Error(w, "admin UI is not available", http.StatusInternalServerError)
		return
	}
	html := strings.ReplaceAll(string(raw), `"/admin/`, `"`+mountPath+`/`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func serveWebFile(handler http.Handler, r *http.Request, w http.ResponseWriter, path string) {
	clone := r.Clone(r.Context())
	clone.URL.Path = path
	handler.ServeHTTP(w, clone)
}
