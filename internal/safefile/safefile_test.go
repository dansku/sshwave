//go:build unix

package safefile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadLimit(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f")
	os.WriteFile(f, []byte("0123456789"), 0o600)

	for _, limit := range []int64{10, 11} { // a file of exactly limit bytes is not truncated
		if data, truncated, err := Read(f, limit); err != nil || truncated || string(data) != "0123456789" {
			t.Errorf("limit %d: data=%q truncated=%v err=%v", limit, data, truncated, err)
		}
	}
	if data, truncated, err := Read(f, 4); err != nil || !truncated || string(data) != "0123" {
		t.Errorf("limit 4: data=%q truncated=%v err=%v", data, truncated, err)
	}
	if _, err := ReadAll(f, 4); err == nil {
		t.Error("ReadAll must refuse a file larger than the limit")
	}
	if data, err := ReadAll(f, 10); err != nil || string(data) != "0123456789" {
		t.Errorf("ReadAll: data=%q err=%v", data, err)
	}
}

func TestReadRefusesNonRegularFiles(t *testing.T) {
	d := t.TempDir()
	pipe := filepath.Join(d, "pipe")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skip("no FIFO here:", err)
	}
	// A hang on the FIFO fails by the test timeout.
	for _, p := range []string{pipe, d, filepath.Join(d, "missing")} {
		if _, _, err := Read(p, 10); err == nil {
			t.Errorf("Read(%s) must fail", p)
		}
	}
}
