package asyncqueue

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Run("load valid config", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.json")

		configData := map[string]any{
			"queues": map[string]any{
				"default": map[string]any{
					"channel":          "{queue}",
					"timeout_seconds":  2,
					"handle_timeout":   10,
					"retry_seconds":    []int{5, 10},
					"processes":        2,
					"concurrent":       10,
					"max_messages":     1000,
					"shutdown_timeout": 30,
					"enabled":          true,
				},
			},
		}

		data, _ := json.Marshal(configData)
		if err := os.WriteFile(configFile, data, 0o644); err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadConfig(configFile)
		if err != nil {
			t.Fatalf("LoadConfig failed: %v", err)
		}

		if len(cfg.Queues) != 1 {
			t.Fatalf("expected 1 queue, got %d", len(cfg.Queues))
		}

		qcfg, ok := cfg.Queues["default"]
		if !ok {
			t.Fatal("queue 'default' not found")
		}

		if qcfg.Name != "default" {
			t.Fatalf("expected queue name default, got %s", qcfg.Name)
		}
		if qcfg.Channel != "{queue}" {
			t.Errorf("expected channel '{queue}', got '%s'", qcfg.Channel)
		}
		if qcfg.Processes != 2 {
			t.Errorf("expected processes 2, got %d", qcfg.Processes)
		}
		if qcfg.Concurrent != 10 {
			t.Errorf("expected concurrent 10, got %d", qcfg.Concurrent)
		}
		if !qcfg.Enabled {
			t.Error("expected enabled true")
		}
	})

	t.Run("apply default values", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.json")

		configData := map[string]any{
			"queues": map[string]any{
				"test": map[string]any{
					"redis_addr": "127.0.0.1:6379",
					"channel":    "{queue:test}",
					"enabled":    true,
				},
			},
		}

		data, _ := json.Marshal(configData)
		if err := os.WriteFile(configFile, data, 0o644); err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadConfig(configFile)
		if err != nil {
			t.Fatalf("LoadConfig failed: %v", err)
		}

		qcfg := cfg.Queues["test"]
		if qcfg.Name != "test" {
			t.Errorf("expected default name test, got %s", qcfg.Name)
		}
		if qcfg.TimeoutSeconds != 2 {
			t.Errorf("expected default timeout_seconds 2, got %d", qcfg.TimeoutSeconds)
		}
		if qcfg.HandleTimeout != 10 {
			t.Errorf("expected default handle_timeout 10, got %d", qcfg.HandleTimeout)
		}
		if len(qcfg.RetrySeconds) != 1 || qcfg.RetrySeconds[0] != 5 {
			t.Errorf("expected default retry_seconds [5], got %v", qcfg.RetrySeconds)
		}
		if qcfg.Processes != 1 {
			t.Errorf("expected default processes 1, got %d", qcfg.Processes)
		}
		if qcfg.Concurrent != 10 {
			t.Errorf("expected default concurrent 10, got %d", qcfg.Concurrent)
		}
		if qcfg.ShutdownTimeout != 30 {
			t.Errorf("expected default shutdown_timeout 30, got %d", qcfg.ShutdownTimeout)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := LoadConfig("/nonexistent/config.json")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.json")

		if err := os.WriteFile(configFile, []byte("invalid json"), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := LoadConfig(configFile)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("multiple queues", func(t *testing.T) {
		tmpDir := t.TempDir()
		configFile := filepath.Join(tmpDir, "config.json")

		configData := map[string]any{
			"queues": map[string]any{
				"default": map[string]any{
					"redis_addr": "127.0.0.1:6379",
					"channel":    "{queue}",
					"enabled":    true,
				},
				"email": map[string]any{
					"redis_addr": "127.0.0.1:6379",
					"channel":    "{queue:email}",
					"processes":  3,
					"enabled":    true,
				},
				"disabled": map[string]any{
					"redis_addr": "127.0.0.1:6379",
					"channel":    "{queue:disabled}",
					"enabled":    false,
				},
			},
		}

		data, _ := json.Marshal(configData)
		if err := os.WriteFile(configFile, data, 0o644); err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadConfig(configFile)
		if err != nil {
			t.Fatalf("LoadConfig failed: %v", err)
		}

		if len(cfg.Queues) != 3 {
			t.Fatalf("expected 3 queues, got %d", len(cfg.Queues))
		}
		if cfg.Queues["email"].Processes != 3 {
			t.Errorf("expected email queue processes 3, got %d", cfg.Queues["email"].Processes)
		}
		if cfg.Queues["disabled"].Enabled {
			t.Error("expected disabled queue to be disabled")
		}
	})
}
