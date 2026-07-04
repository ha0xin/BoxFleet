package db

import (
	"encoding/json"
	"testing"
)

func TestNormalizeRealityShortID(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{input: "", want: ""},
		{input: " 01AB ", want: "01ab"},
		{input: "01234567", want: "01234567"},
	} {
		got, err := NormalizeRealityShortID(tc.input)
		if err != nil {
			t.Fatalf("NormalizeRealityShortID(%q): %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeRealityShortID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}

	for _, input := range []string{"0", "012", "0123456789", "not-hex"} {
		if _, err := NormalizeRealityShortID(input); err == nil {
			t.Fatalf("NormalizeRealityShortID(%q) unexpectedly succeeded", input)
		}
	}
}

func TestNormalizeVLESSRealitySettingsJSON(t *testing.T) {
	got, err := normalizeVLESSRealitySettingsJSON(`{"server_name":"example.com","short_id":" 01AB "}`)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(got), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["short_id"] != "01ab" || settings["server_name"] != "example.com" {
		t.Fatalf("settings = %#v", settings)
	}

	for _, raw := range []string{
		`{"short_id":"123"}`,
		`{"short_id":"xx"}`,
		`{"short_id":1234}`,
		`[]`,
		`null`,
	} {
		if _, err := normalizeVLESSRealitySettingsJSON(raw); err == nil {
			t.Fatalf("normalizeVLESSRealitySettingsJSON(%q) unexpectedly succeeded", raw)
		}
	}
}
