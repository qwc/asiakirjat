package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Server.Address != "0.0.0.0" {
		t.Errorf("expected default address 0.0.0.0, got %s", cfg.Server.Address)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Errorf("expected default driver sqlite, got %s", cfg.Database.Driver)
	}
	if cfg.Storage.BasePath != "data/projects" {
		t.Errorf("expected default storage path data/projects, got %s", cfg.Storage.BasePath)
	}
	if cfg.Auth.Session.CookieName != "asiakirjat_session" {
		t.Errorf("expected default cookie name, got %s", cfg.Auth.Session.CookieName)
	}
}

func TestLoadYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
server:
  address: "127.0.0.1"
  port: 9090
database:
  driver: "postgres"
  dsn: "postgres://localhost/test"
storage:
  base_path: "/var/data/docs"
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Server.Address != "127.0.0.1" {
		t.Errorf("expected address 127.0.0.1, got %s", cfg.Server.Address)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Database.Driver != "postgres" {
		t.Errorf("expected driver postgres, got %s", cfg.Database.Driver)
	}
	if cfg.Storage.BasePath != "/var/data/docs" {
		t.Errorf("expected storage path /var/data/docs, got %s", cfg.Storage.BasePath)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load("/nonexistent/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	// Should return defaults
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("ASIAKIRJAT_SERVER_PORT", "3000")
	t.Setenv("ASIAKIRJAT_DB_DRIVER", "mysql")
	t.Setenv("ASIAKIRJAT_STORAGE_PATH", "/custom/path")
	t.Setenv("ASIAKIRJAT_SESSION_SECURE", "true")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Server.Port != 3000 {
		t.Errorf("expected port 3000, got %d", cfg.Server.Port)
	}
	if cfg.Database.Driver != "mysql" {
		t.Errorf("expected driver mysql, got %s", cfg.Database.Driver)
	}
	if cfg.Storage.BasePath != "/custom/path" {
		t.Errorf("expected storage path /custom/path, got %s", cfg.Storage.BasePath)
	}
	if !cfg.Auth.Session.Secure {
		t.Error("expected session secure to be true")
	}
}

func TestEnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
server:
  port: 9090
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ASIAKIRJAT_SERVER_PORT", "5555")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	// Env should override YAML
	if cfg.Server.Port != 5555 {
		t.Errorf("expected env override port 5555, got %d", cfg.Server.Port)
	}
}

func TestListenAddr(t *testing.T) {
	cfg := Defaults()
	if cfg.ListenAddr() != "0.0.0.0:8080" {
		t.Errorf("expected 0.0.0.0:8080, got %s", cfg.ListenAddr())
	}
}
