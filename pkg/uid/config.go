package uid

import "time"

// Config controls Snowflake-style ID generation.
type Config struct {
	WorkerID         int           `json:"worker_id" yaml:"worker_id"`
	Epoch            time.Time     `json:"epoch" yaml:"epoch"`
	WorkerBits       uint8         `json:"worker_bits" yaml:"worker_bits"`
	SequenceBits     uint8         `json:"sequence_bits" yaml:"sequence_bits"`
	ClockBackoff     time.Duration `json:"clock_backoff" yaml:"clock_backoff"`
	AllowClockSkew   bool          `json:"allow_clock_skew" yaml:"allow_clock_skew"`
	UseCoordination  bool          `json:"use_coordination" yaml:"use_coordination"`
	CoordinationPath string        `json:"coordination_path" yaml:"coordination_path"`
}
