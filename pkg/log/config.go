package log

import "fmt"

// Config 定义 clog 组件的配置结构体
// 支持通过环境变量、配置文件或直接构造进行配置
type Config struct {
	// Level 日志级别，控制记录哪些级别的日志
	// 可选值：debug, info, warn, error, fatal
	Level string `json:"level" yaml:"level"`

	// Format 日志输出格式
	// json: 结构化 JSON 格式，适合生产环境和日志收集系统
	// console: 人类可读的格式，适合开发环境
	Format string `json:"format" yaml:"format"`

	// Output 日志输出目标
	// stdout: 标准输出
	// stderr: 标准错误输出
	// 文件路径: 输出到指定文件，支持日志轮转
	Output string `json:"output" yaml:"output"`

	// AddSource 是否在日志中包含源码文件名和行号
	// 开发环境建议开启，便于调试；生产环境可根据需要关闭
	AddSource bool `json:"addSource" yaml:"addSource"`

	// EnableColor 是否启用颜色输出（仅 console 格式有效）
	// 开发环境建议开启，提升可读性
	EnableColor bool `json:"enableColor" yaml:"enableColor"`

	// RootPath 项目根目录路径，用于缩短显示的源码路径
	// 设置后，日志中的调用者信息将显示相对于 RootPath 的路径
	RootPath string `json:"rootPath,omitempty" yaml:"rootPath,omitempty"`

	// Rotation 日志文件轮转配置（仅文件输出时生效）
	// 用于控制日志文件的大小、数量和保留时间
	Rotation *RotationConfig `json:"rotation,omitempty" yaml:"rotation,omitempty"`
}

// RotationConfig 定义日志文件轮转配置
// 基于 lumberjack 实现，支持按大小、时间和数量进行日志轮转
type RotationConfig struct {
	// MaxSize 单个日志文件的最大大小（MB）
	// 超过此大小后，当前日志文件会被轮转
	MaxSize int `json:"maxSize" yaml:"maxSize"`

	// MaxBackups 保留的旧日志文件最大数量
	// 超过此数量后，最旧的日志文件会被删除
	MaxBackups int `json:"maxBackups" yaml:"maxBackups"`

	// MaxAge 旧日志文件的最大保留天数
	// 超过此天数的日志文件会被删除
	MaxAge int `json:"maxAge" yaml:"maxAge"`

	// Compress 是否压缩已轮转的日志文件
	// 压缩可节省磁盘空间，但会增加 CPU 开销
	Compress bool `json:"compress" yaml:"compress"`
}

// GetDefaultConfig 返回环境相关的默认配置
// 根据不同的运行环境提供优化的配置，减少配置工作量
//
// 参数：
//   - env: 运行环境，支持 "development" 和 "production"
//
// 返回：
//   - *Config: 针对指定环境优化的配置
//
// 环境配置说明：
//   - development: 控制台格式，调试级别，带颜色，适合开发调试
//   - production: JSON 格式，信息级别，无颜色，适合生产环境
//   - 其他: 默认配置，控制台格式，信息级别，带颜色
func GetDefaultConfig(env string) *Config {
	switch env {
	case "development":
		return &Config{
			Level:       "debug",
			Format:      "console",
			Output:      "stdout",
			AddSource:   true,
			EnableColor: true,
			RootPath:    "infra-kit",
		}
	case "production":
		return &Config{
			Level:       "info",
			Format:      "json",
			Output:      "stdout",
			AddSource:   true, // 确保 JSON 格式也显示源码信息
			EnableColor: false,
			RootPath:    "",
		}
	default:
		return &Config{
			Level:       "info",
			Format:      "console",
			Output:      "stdout",
			AddSource:   true,
			EnableColor: true,
			RootPath:    "infra-kit",
		}
	}
}

// Validate 验证配置的有效性
// 在初始化日志器之前调用，确保配置参数的正确性
//
// 验证项目：
//   - 日志级别：必须是 debug, info, warn, error, fatal 之一
//   - 日志格式：必须是 json 或 console
//   - 输出目标：不能为空
//   - 轮转配置：数值不能为负数
//
// 返回：
//   - error: 配置无效时返回具体的错误信息
//   - nil: 配置有效
func (c *Config) Validate() error {
	// 验证日志级别
	validLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true, "fatal": true,
	}
	if !validLevels[c.Level] {
		return fmt.Errorf("invalid log level: %s, must be one of: debug, info, warn, error, fatal", c.Level)
	}

	// 验证日志格式
	if c.Format != "json" && c.Format != "console" {
		return fmt.Errorf("invalid log format: %s, must be 'json' or 'console'", c.Format)
	}

	// 验证输出目标
	if c.Output == "" {
		return fmt.Errorf("log output cannot be empty")
	}

	// 验证轮转配置
	if c.Rotation != nil {
		if c.Rotation.MaxSize < 0 {
			return fmt.Errorf("rotation maxSize cannot be negative")
		}
		if c.Rotation.MaxBackups < 0 {
			return fmt.Errorf("rotation maxBackups cannot be negative")
		}
		if c.Rotation.MaxAge < 0 {
			return fmt.Errorf("rotation maxAge cannot be negative")
		}
	}

	return nil
}
