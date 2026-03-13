package asyncqueue

import (
	"encoding/json"
	"fmt"
	"os"
)

// QueueConfig defines the configuration for a single queue.
type QueueConfig struct {
	Name            string `json:"name"`
	Channel         string `json:"channel"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	HandleTimeout   int    `json:"handle_timeout"`
	RetrySeconds    []int  `json:"retry_seconds"`
	MaxAttempts     int    `json:"max_attempts"` // Maximum retry attempts for jobs in this queue
	Processes       int    `json:"processes"`
	Concurrent      int    `json:"concurrent"`
	MaxMessages     int    `json:"max_messages"`
	ShutdownTimeout int    `json:"shutdown_timeout"`
	Enabled         bool   `json:"enabled"`
	AutoRestart     bool   `json:"auto_restart"` // Automatically restart worker after reaching max_messages
}

// Config defines the multi-queue application configuration.
type Config struct {
	Queues map[string]QueueConfig `json:"queues"`
}

// LoadConfig loads configuration from a JSON file.
func LoadConfig(filepath string) (*Config, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	for name, queueCfg := range cfg.Queues {
		if queueCfg.TimeoutSeconds <= 0 {
			queueCfg.TimeoutSeconds = 2
		}
		if queueCfg.HandleTimeout <= 0 {
			queueCfg.HandleTimeout = 10
		}
		if len(queueCfg.RetrySeconds) == 0 {
			queueCfg.RetrySeconds = []int{5}
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
