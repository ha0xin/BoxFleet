package units

import (
	"fmt"
	"math"
	"strings"

	"github.com/dustin/go-humanize"
)

func ParseBytes(input string) (int64, error) {
	value := strings.TrimSpace(strings.ToLower(input))
	if value == "" {
		return 0, fmt.Errorf("empty byte value")
	}
	if value == "0" || value == "unlimited" || value == "none" {
		return 0, nil
	}
	bytes, err := humanize.ParseBytes(value)
	if err != nil {
		return 0, fmt.Errorf("parse byte value %q: %w", input, err)
	}
	if bytes > math.MaxInt64 {
		return 0, fmt.Errorf("byte value too large")
	}
	if bytes == 0 {
		return 0, fmt.Errorf("byte value must be at least 1 byte or 0 for unlimited")
	}
	return int64(bytes), nil
}

func FormatBytes(bytes int64) string {
	if bytes == 0 {
		return "unlimited"
	}
	return humanize.IBytes(uint64(bytes))
}
