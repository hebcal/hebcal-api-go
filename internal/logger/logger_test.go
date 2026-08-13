package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reopen is what makes logrotate work: the process has to let go of the old
// inode and start writing to a fresh file.
func TestAccessLogReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.log")
	lg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	lg.Info("before")
	if err := os.Rename(path, filepath.Join(dir, "api.log.1")); err != nil {
		t.Fatal(err)
	}
	if err := lg.Reopen(); err != nil {
		t.Fatal(err)
	}
	lg.Info("after")

	fresh, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no new log file after Reopen: %v", err)
	}
	if !strings.Contains(string(fresh), "after") {
		t.Error("new file should carry lines written after the reopen")
	}
	if strings.Contains(string(fresh), "before") {
		t.Error("new file should not carry lines from before the rotation")
	}
}
