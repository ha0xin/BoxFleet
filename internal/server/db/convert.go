package db

import (
	"database/sql"
	"fmt"
	"strings"
)

func normalizeName(name string) string {
	return strings.TrimSpace(name)
}

func validateNameForAuth(name, resource string) error {
	if strings.Contains(name, "@") || strings.Contains(name, ">>>") {
		return fmt.Errorf("%s name must not contain @ or >>>", resource)
	}
	return nil
}

func nullableTrimmedString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func boolToInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func int64ToBool(value int64) bool {
	return value != 0
}

func requireAffected(affected int64, resource, name string) error {
	if affected == 0 {
		return fmt.Errorf("%s %q not found", resource, name)
	}
	return nil
}
