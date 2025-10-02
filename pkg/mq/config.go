package mq

// Config holds message queue settings.
type Config struct {
	Brokers     []string `json:"brokers" yaml:"brokers"`
	Topic       string   `json:"topic" yaml:"topic"`
	Group       string   `json:"group" yaml:"group"`
	MaxInFlight int      `json:"max_in_flight" yaml:"max_in_flight"`
	Retry       Retry    `json:"retry" yaml:"retry"`
	DeadLetter  string   `json:"dead_letter" yaml:"dead_letter"`
}

// Retry controls retry behaviour for consumers.
type Retry struct {
	Attempts int    `json:"attempts" yaml:"attempts"`
	Backoff  string `json:"backoff" yaml:"backoff"`
}
