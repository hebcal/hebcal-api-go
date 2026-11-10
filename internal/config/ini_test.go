package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadINI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.ini")
	content := "; comment\n# another\n[section]\n\nhebcal.mysql.host = db.example.com\n" +
		"hebcal.mysql.port=25060\nhebcal.mysql.password=\"p=a;ss\"\nnoequals\nhebcal.mysql.port=3306\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := ReadINI(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"hebcal.mysql.host":     "db.example.com",
		"hebcal.mysql.port":     "3306",
		"hebcal.mysql.password": "p=a;ss",
	}
	if len(m) != len(want) {
		t.Errorf("got %d keys, want %d: %v", len(m), len(want), m)
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
	if _, err := ReadINI(filepath.Join(t.TempDir(), "missing.ini")); err == nil {
		t.Error("missing file: want an error")
	}
}
