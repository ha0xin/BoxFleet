package model

import "testing"

func TestBootstrapRoundTrip(t *testing.T) {
	encoded, err := EncodeBootstrap(BootstrapConfig{
		NodeName:   "azus",
		Token:      "secret",
		ServerURL:  "http://100.64.0.1:18081",
		SingBoxURL: "https://example.test/sing-box",
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeBootstrap(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.NodeName != "azus" || decoded.Token != "secret" || decoded.ServerURL != "http://100.64.0.1:18081" || decoded.SingBoxURL != "https://example.test/sing-box" {
		t.Fatalf("decoded = %#v", decoded)
	}
}
