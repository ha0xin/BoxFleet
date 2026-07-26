package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapValueSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bootstrap")
	if err := os.WriteFile(path, []byte("boxfleet-bootstrap:file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv(bootstrapEnvVar, "")
	value, err := bootstrapValue(parseBootstrapArgs(t), path)
	if err != nil || value != "boxfleet-bootstrap:file" {
		t.Fatalf("--from-file = %q, %v", value, err)
	}

	t.Setenv(bootstrapEnvVar, "boxfleet-bootstrap:env\n")
	value, err = bootstrapValue(parseBootstrapArgs(t), "")
	if err != nil || value != "boxfleet-bootstrap:env" {
		t.Fatalf("%s = %q, %v", bootstrapEnvVar, value, err)
	}

	// The argument form stays supported for compatibility even though it exposes
	// the node token through ps.
	t.Setenv(bootstrapEnvVar, "")
	value, err = bootstrapValue(parseBootstrapArgs(t, "boxfleet-bootstrap:arg"), "")
	if err != nil || value != "boxfleet-bootstrap:arg" {
		t.Fatalf("argument = %q, %v", value, err)
	}

	if _, err := bootstrapValue(parseBootstrapArgs(t), ""); err == nil {
		t.Fatal("missing bootstrap string was accepted")
	}
	t.Setenv(bootstrapEnvVar, "boxfleet-bootstrap:env")
	if _, err := bootstrapValue(parseBootstrapArgs(t, "boxfleet-bootstrap:arg"), ""); err == nil {
		t.Fatal("ambiguous bootstrap sources were accepted")
	}
}

func parseBootstrapArgs(t *testing.T, args ...string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs
}
