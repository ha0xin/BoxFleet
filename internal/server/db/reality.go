package db

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// NormalizeRealityShortID validates the single Reality short ID supported by
// BoxFleet and returns its canonical lowercase representation.
func NormalizeRealityShortID(shortID string) (string, error) {
	shortID = strings.ToLower(strings.TrimSpace(shortID))
	if len(shortID) > 8 {
		return "", errors.New("short_id must contain 0 to 8 hexadecimal digits")
	}
	if len(shortID)%2 != 0 {
		return "", errors.New("short_id must contain an even number of hexadecimal digits")
	}
	if _, err := hex.DecodeString(shortID); err != nil {
		return "", errors.New("short_id must contain only hexadecimal digits")
	}
	return shortID, nil
}

func normalizeVLESSRealitySettingsJSON(raw string) (string, error) {
	var settings map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return "", fmt.Errorf("settings_json must be a JSON object: %w", err)
	}
	if settings == nil {
		return "", errors.New("settings_json must be a JSON object")
	}
	encodedShortID, ok := settings["short_id"]
	if !ok {
		return raw, nil
	}
	var shortID string
	if err := json.Unmarshal(encodedShortID, &shortID); err != nil {
		return "", errors.New("settings_json short_id must be a string")
	}
	normalized, err := NormalizeRealityShortID(shortID)
	if err != nil {
		return "", err
	}
	if normalized == shortID {
		return raw, nil
	}
	settings["short_id"], err = json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
