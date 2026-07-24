package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	previousVersion := version
	version = "v-test"
	t.Cleanup(func() { version = previousVersion })

	cmd := newServerCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(output.String()); got != "bfs version v-test" {
		t.Fatalf("version output = %q", got)
	}
}
