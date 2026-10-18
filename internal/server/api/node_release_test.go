package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerBuildIdentityIsIndependentOfNodeArtifactRelease(t *testing.T) {
	opts := Options{Version: "local-path-groups-build", NodeRelease: "v0.13.0", AgentVersion: "v0.8.1"}
	if releaseVersion(opts) != "local-path-groups-build" || nodeReleaseVersion(opts) != "v0.13.0" {
		t.Fatal("server and node identities are coupled")
	}
	rec := httptest.NewRecorder()
	installScriptHandler(opts).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/install.sh", nil))
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "local-path-groups-build") || !strings.Contains(rec.Body.String(), "v0.13.0") {
		t.Fatalf("installer lost published release reference: status %d", rec.Code)
	}
	if nodeReleaseVersion(Options{Version: "v0.13.0"}) != "v0.13.0" {
		t.Fatal("default release contract changed")
	}
}
