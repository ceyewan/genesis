package idgen

import (
	"github.com/ceyewan/genesis/xerrors"
)

// ========================================
// 配置结构 (Configuration)
// ========================================

// GeneratorMode Snowflake 位布局模式。
type GeneratorMode string

const (
	// GeneratorModeSingleDC 使用 41bit 时间戳 + 10bit worker + 12bit sequence。
	GeneratorModeSingleDC GeneratorMode = "single_dc"

	// GeneratorModeMultiDC 使用 41bit 时间戳 + 5bit datacenter + 5bit worker + 12bit sequence。
	GeneratorModeMultiDC GeneratorMode = "multi_dc"
)

// GeneratorConfig ID 生成器配置 (Snowflake)
type GeneratorConfig struct {
	// Mode 位布局模式，默认 "multi_dc"。
	Mode GeneratorMode `yaml:"mode" json:"mode"`

	// WorkerID 工作节点 ID。
	// single_dc 模式范围 [0, 1023]，multi_dc 模式范围 [0, 31]。
	WorkerID int64 `yaml:"worker_id" json:"worker_id"`

	// DatacenterID 数据中心 ID。
	// single_dc 模式下必须为 0，multi_dc 模式范围 [0, 31]。
	DatacenterID int64 `yaml:"datacenter_id" json:"datacenter_id"`
}

func (c *GeneratorConfig) setDefaults() {
	if c.Mode == "" {
		c.Mode = GeneratorModeMultiDC
	}
}

func (c *GeneratorConfig) validate() error {
	switch c.Mode {
	case GeneratorModeSingleDC:
		if c.WorkerID < 0 || c.WorkerID > 1023 {
			return xerrors.WithCode(ErrInvalidInput, "worker_id_out_of_range")
		}
		if c.DatacenterID != 0 {
			return xerrors.WithCode(ErrInvalidInput, "datacenter_id_must_be_zero")
		}
	case GeneratorModeMultiDC:
		if c.WorkerID < 0 || c.WorkerID > 31 {
			return xerrors.WithCode(ErrInvalidInput, "worker_id_out_of_range")
		}
		if c.DatacenterID < 0 || c.DatacenterID > 31 {
			return xerrors.WithCode(ErrInvalidInput, "datacenter_id_out_of_range")
		}
	default:
		return xerrors.WithCode(ErrInvalidInput, "unsupported_generator_mode")
	}

	return nil
}

// ========================================

// SequencerConfig 序列号生成器配置
type SequencerConfig struct {
	// Driver 后端类型: "redis" | "etcd"，默认 "redis"
	Driver string `yaml:"driver" json:"driver"`

	// KeyPrefix 键前缀
	KeyPrefix string `yaml:"key_prefix" json:"key_prefix"`

	// Step 步长，默认为 1
	Step int64 `yaml:"step" json:"step"`

	// MaxValue 最大值限制，达到后循环（0 表示不限制）
	MaxValue int64 `yaml:"max_value" json:"max_value"`

	// TTL 键过期时间（秒），0 表示永不过期
	TTL int64 `yaml:"ttl" json:"ttl"`
}

func (c *SequencerConfig) setDefaults() {
	if c.Driver == "" {
		c.Driver = "redis"
	}
	if c.Step <= 0 {
		c.Step = 1
	}
}

func (c *SequencerConfig) validate() error {
	if c.Driver != "redis" {
		return xerrors.WithCode(ErrInvalidInput, "unsupported_driver")
	}
	if c.Step <= 0 {
		return xerrors.WithCode(ErrInvalidInput, "step_must_be_positive")
	}
	if c.MaxValue < 0 {
		return xerrors.WithCode(ErrInvalidInput, "max_value_cannot_be_negative")
	}
	if c.TTL < 0 {
		return xerrors.WithCode(ErrInvalidInput, "ttl_cannot_be_negative")
	}
	return nil
}

// ========================================

// AllocatorConfig WorkerID 分配器配置
type AllocatorConfig struct {
	// Driver 后端类型: "redis" | "etcd"
	Driver string `yaml:"driver" json:"driver"`

	// KeyPrefix 键前缀，默认 "genesis:idgen:worker"
	KeyPrefix string `yaml:"key_prefix" json:"key_prefix"`

	// MaxID 最大 ID 范围 [0, maxID)，默认 1024
	MaxID int `yaml:"max_id" json:"max_id"`

	// TTL 租约 TTL（秒），默认 30
	TTL int `yaml:"ttl" json:"ttl"`
}

func (c *AllocatorConfig) setDefaults() {
	if c.Driver == "" {
		c.Driver = "redis"
	}
	if c.KeyPrefix == "" {
		c.KeyPrefix = "genesis:idgen:worker"
	}
	if c.MaxID <= 0 {
		c.MaxID = 1024
	}
	if c.TTL <= 0 {
		c.TTL = 30
	}
}

func (c *AllocatorConfig) validate() error {
	if c.Driver != "redis" && c.Driver != "etcd" {
		return xerrors.WithCode(ErrInvalidInput, "unsupported_driver")
	}
	if c.MaxID <= 0 || c.MaxID > 1024 {
		return xerrors.WithCode(ErrInvalidInput, "max_id_out_of_range")
	}
	if c.TTL <= 0 {
		return xerrors.WithCode(ErrInvalidInput, "ttl_must_be_positive")
	}
	return nil
}
