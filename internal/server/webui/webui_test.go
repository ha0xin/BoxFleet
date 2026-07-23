package webui

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashedAssetsAreCompressedAndCached(t *testing.T) {
	handler := Handler("/admin")
	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/admin/", nil))
	if index.Code != http.StatusOK {
		t.Fatalf("index status = %d", index.Code)
	}
	start := strings.Index(index.Body.String(), `src="`)
	if start < 0 {
		t.Fatal("index has no script")
	}
	start += len(`src="`)
	end := strings.Index(index.Body.String()[start:], `"`)
	if end < 0 {
		t.Fatal("script src is not terminated")
	}
	scriptPath := index.Body.String()[start : start+end]
	if strings.HasPrefix(scriptPath, "./") {
		scriptPath = "/admin/" + strings.TrimPrefix(scriptPath, "./")
	}

	req := httptest.NewRequest(http.MethodGet, scriptPath, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	result := rec.Result()
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d", result.StatusCode)
	}
	if got := result.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	if got := result.Header.Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("Cache-Control = %q", got)
	}
	compressed, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(compressed) >= 500_000 {
		t.Fatalf("compressed script = %d bytes, want < 500000", len(compressed))
	}
}

func TestIndexIsNotStoredAsImmutable(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler("/admin").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/nodes", nil))
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestNestedRouteLoadsInitialAssetFromHiddenMount(t *testing.T) {
	handler := Handler("/secret/admin")
	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/secret/admin/mihomo-profiles", nil))
	if index.Code != http.StatusOK {
		t.Fatalf("index status = %d", index.Code)
	}
	start := strings.Index(index.Body.String(), `src="`)
	if start < 0 {
		t.Fatal("index has no script")
	}
	start += len(`src="`)
	end := strings.Index(index.Body.String()[start:], `"`)
	if end < 0 {
		t.Fatal("script src is not terminated")
	}
	scriptPath := index.Body.String()[start : start+end]
	if !strings.HasPrefix(scriptPath, "./assets/") {
		t.Fatalf("script src = %q, want mount-relative asset", scriptPath)
	}

	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(
		http.MethodGet,
		"/secret/admin/"+strings.TrimPrefix(scriptPath, "./"),
		nil,
	))
	if asset.Code != http.StatusOK {
		t.Fatalf("asset status = %d", asset.Code)
	}
	if got := asset.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestBuiltJavaScriptUsesMountRelativeAssets(t *testing.T) {
	err := fs.WalkDir(assets, "assets/generated/assets", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".js" {
			return nil
		}
		body, err := fs.ReadFile(assets, path)
		if err != nil {
			return err
		}
		if bytes.Contains(body, []byte(`"/admin/`)) {
			t.Errorf("%s contains an absolute /admin asset URL", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
