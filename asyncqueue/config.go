package asyncqueue

import (
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
)

const defaultMessageTTLSeconds = 10 * 24 * 60 * 60

type QueueConfig struct {
	Name            string `json:"name"             yaml:"name"`
	Channel         string `json:"channel"          yaml:"channel"`
	Enabled         bool   `json:"enabled"          yaml:"enabled"`
	PopTimeout      int    `json:"pop_timeout"      yaml:"pop_timeout"`
	HandleTimeout   int    `json:"handle_timeout"   yaml:"handle_timeout"`
	RetrySeconds    []int  `json:"retry_seconds"    yaml:"retry_seconds"`
	MessageTTL      int    `json:"message_ttl"      yaml:"message_ttl"`
	MaxAttempts     int    `json:"max_attempts"     yaml:"max_attempts"`
	Processes       int    `json:"processes"        yaml:"processes"`
	Concurrent      int    `json:"concurrent"       yaml:"concurrent"`
	MaxMessages     int    `json:"max_messages"     yaml:"max_messages"`
	AutoRestart     bool   `json:"auto_restart"     yaml:"auto_restart"`
	ShutdownTimeout int    `json:"shutdown_timeout" yaml:"shutdown_timeout"`
}

type Config struct {
	Queues map[string]QueueConfig `json:"queues" yaml:"queues"`
}

// LoadConfig loads configuration from a JSON file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".json":
		err = json.Unmarshal(data, &cfg)
	case ".yaml", ".yml":
		err = yaml.Unmarshal(data, &cfg)
	default:
		return nil, fmt.Errorf("unsupported config format: %s", ext)
	}
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	for name, queueCfg := range cfg.Queues {
		if queueCfg.PopTimeout <= 0 {
			queueCfg.PopTimeout = 1
		}
		if queueCfg.HandleTimeout <= 0 {
			queueCfg.HandleTimeout = 10
		}
		if len(queueCfg.RetrySeconds) == 0 {
			queueCfg.RetrySeconds = []int{5}
		}
		if queueCfg.MessageTTL <= 0 {
			queueCfg.MessageTTL = defaultMessageTTLSeconds
		}
		if queueCfg.MaxAttempts <= 0 {
			queueCfg.MaxAttempts = 3
		}
		if queueCfg.Processes <= 0 {
			queueCfg.Processes = 1
		}
		if queueCfg.Concurrent <= 0 {
			queueCfg.Concurrent = 10
		}
		if queueCfg.ShutdownTimeout <= 0 {
			queueCfg.ShutdownTimeout = 30
		}
		cfg.Queues[name] = queueCfg
	}

	return &cfg, nil
}
