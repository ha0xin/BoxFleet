package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteAdminErrorUsesStructuredJSON(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeAdminError(recorder, assertError("invalid request"))

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnprocessableEntity)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q, want application/json", got)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error != "invalid request" {
		t.Fatalf("error = %q, want invalid request", body.Error)
	}
}

type assertError string

func (err assertError) Error() string { return string(err) }
