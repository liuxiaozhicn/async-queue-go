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

	for name, qcfg := range cfg.Queues {
		if qcfg.TimeoutSeconds <= 0 {
			qcfg.TimeoutSeconds = 2
		}
		if qcfg.HandleTimeout <= 0 {
			qcfg.HandleTimeout = 10
		}
		if len(qcfg.RetrySeconds) == 0 {
			qcfg.RetrySeconds = []int{5}
		}
		if qcfg.MaxAttempts <= 0 {
			qcfg.MaxAttempts = 3
		}
		if qcfg.Processes <= 0 {
			qcfg.Processes = 1
		}
		if qcfg.Concurrent <= 0 {
			qcfg.Concurrent = 10
		}
		if qcfg.ShutdownTimeout <= 0 {
			qcfg.ShutdownTimeout = 30
		}
		cfg.Queues[name] = qcfg
	}

	return &cfg, nil
}
