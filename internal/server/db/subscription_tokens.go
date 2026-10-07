package db

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"github.com/haoxin/boxfleet/internal/id"
)

const subscriptionTokenPrefix = "bfsub_"

var ErrActiveSubscriptionTokenExists = errors.New("Mihomo profile already has an active subscription token")

func newSubscriptionToken() (string, string, error) {
	tokenID, err := id.New("stok")
	if err != nil {
		return "", "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	return tokenID, subscriptionTokenPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}
