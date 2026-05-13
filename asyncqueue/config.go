package asyncqueue

const defaultMessageTTLSeconds = 10 * 24 * 60 * 60

type QueueConfig struct {
	Name            string `json:"name"             yaml:"name"`
	Driver          string `json:"driver"           yaml:"driver"`
	Channel         string `json:"channel"          yaml:"channel"`
	Enabled         bool   `json:"enabled"          yaml:"enabled"`
	PopTimeout      int    `json:"pop_timeout"      yaml:"pop_timeout"`
	HandleTimeout   int    `json:"handle_timeout"   yaml:"handle_timeout"`
	RetrySeconds    []int  `json:"retry_seconds"    yaml:"retry_seconds"`
	MessageTTL      int    `json:"message_ttl"      yaml:"message_ttl"`
	MaxAttempts     int    `json:"max_attempts"     yaml:"max_attempts"`
	Processes       int    `json:"processes"        yaml:"processes"`
	Concurrent      int    `json:"concurrent"       yaml:"concurrent"`
	AutoRestart     bool   `json:"auto_restart"     yaml:"auto_restart"`
	MaxMessages     int    `json:"max_messages"     yaml:"max_messages"`
	ShutdownTimeout int    `json:"shutdown_timeout" yaml:"shutdown_timeout"`
}

type Config struct {
	Queues map[string]QueueConfig `json:"queues" yaml:"queues"`
}
