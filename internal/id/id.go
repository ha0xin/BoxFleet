package id

import (
	"fmt"

	"github.com/google/uuid"
)

func New(prefix string) (string, error) {
	if prefix == "" {
		return uuid.NewString(), nil
	}
	return fmt.Sprintf("%s_%s", prefix, uuid.NewString()), nil
}
