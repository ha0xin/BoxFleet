package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
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
