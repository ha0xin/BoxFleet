package webui

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

//go:embed assets
var assets embed.FS
var gzipAssets sync.Map

func Handler(mountPath string) http.Handler {
	root, err := fs.Sub(assets, "assets/generated")
	if err != nil {
		panic(err)
	}
	mountPath = "/" + strings.Trim(strings.TrimSpace(mountPath), "/")
	if mountPath == "/" {
		mountPath = "/admin"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, mountPath)
		if path == "" || path == "/" {
			serveIndex(root, mountPath, w)
			return
		}
		name := strings.TrimPrefix(path, "/")
		if file, err := root.Open(name); err == nil {
			_ = file.Close()
			serveWebFile(root, name, r, w)
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
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(html))
}

func serveWebFile(root fs.FS, name string, r *http.Request, w http.ResponseWriter) {
	raw, err := fs.ReadFile(root, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	body := raw
	if acceptsGzip(r) && compressibleAsset(name) {
		body = cachedGzip(name, raw)
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func acceptsGzip(r *http.Request) bool {
	for _, encoding := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.TrimSpace(strings.SplitN(encoding, ";", 2)[0]) == "gzip" {
			return true
		}
	}
	return false
}

func compressibleAsset(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".css", ".html", ".js", ".json", ".svg":
		return true
	default:
		return false
	}
}

func cachedGzip(name string, raw []byte) []byte {
	if cached, ok := gzipAssets.Load(name); ok {
		return cached.([]byte)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write(raw)
	_ = writer.Close()
	result := compressed.Bytes()
	gzipAssets.Store(name, result)
	return result
}
