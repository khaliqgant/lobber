package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigSaveLoad(t *testing.T) {
	// Use temp directory
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := &Config{
		Token:          "test-token-123",
		DefaultInspect: true,
	}

	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.Token != cfg.Token {
		t.Errorf("Token = %q, want %q", loaded.Token, cfg.Token)
	}
	if loaded.DefaultInspect != cfg.DefaultInspect {
		t.Errorf("DefaultInspect = %v, want %v", loaded.DefaultInspect, cfg.DefaultInspect)
	}

	// Verify file was created in right place
	configPath := filepath.Join(tmpDir, ".lobber", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file not created")
	}
}
