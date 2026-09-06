package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadOpenRCLogResumesAndHandlesTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.log")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, offset, err := readOpenRCLog(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lines, []string{"one", "two"}) || offset != 8 {
		t.Fatalf("lines=%v offset=%d", lines, offset)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("three\n"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	lines, offset, err = readOpenRCLog(path, offset)
	if err != nil || !reflect.DeepEqual(lines, []string{"three"}) || offset != 14 {
		t.Fatalf("lines=%v offset=%d err=%v", lines, offset, err)
	}
	if err := os.WriteFile(path, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, offset, err = readOpenRCLog(path, offset)
	if err != nil || !reflect.DeepEqual(lines, []string{"new"}) || offset != 4 {
		t.Fatalf("truncated lines=%v offset=%d err=%v", lines, offset, err)
	}
}
