package yahrzeitdb

import (
	"strings"
	"testing"
)

func TestConfigFromINI(t *testing.T) {
	cfg, ok := ConfigFromINI(map[string]string{
		"hebcal.mysql.host":     "db.example.com",
		"hebcal.mysql.port":     "25060",
		"hebcal.mysql.user":     "hebcal",
		"hebcal.mysql.password": "secret",
		"hebcal.mysql.dbname":   "hebcal5",
	})
	if !ok {
		t.Fatal("want ok")
	}
	if cfg.Port != 25060 || cfg.TLS != "preferred" {
		t.Errorf("got %+v", cfg)
	}
	if s := cfg.String(); strings.Contains(s, "secret") || s != "mysql://hebcal@db.example.com:25060/hebcal5" {
		t.Errorf("String() = %q", s)
	}
	cfg, ok = ConfigFromINI(map[string]string{"hebcal.mysql.host": "x"})
	if ok {
		t.Error("no user or dbname: want !ok")
	}
	if cfg.Port != 3306 {
		t.Errorf("default port = %d", cfg.Port)
	}
}

func TestOpenDoesNotConnect(t *testing.T) {
	cfg, _ := ConfigFromINI(map[string]string{
		"hebcal.mysql.host": "127.0.0.1", "hebcal.mysql.port": "1",
		"hebcal.mysql.user": "u", "hebcal.mysql.dbname": "d",
	})
	db, err := Open(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	db.Touch("01jthv2t5k88yermamssn96pzf", true) // queued; fails in the background
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}
