package clog

import (
	"fmt"
	"strings"
)

const timeFormat = "2006-01-02T15:04:05.000Z07:00"

// Config 日志配置结构
type Config struct {
	Level       string `json:"level" yaml:"level"`             // debug|info|warn|error|fatal
	Format      string `json:"format" yaml:"format"`           // json|console
	Output      string `json:"output" yaml:"output"`           // stdout|stderr|<file path>
	EnableColor bool   `json:"enableColor" yaml:"enableColor"` // 仅在 console 格式下有效，开发环境可启用彩色输出
	AddSource   bool   `json:"addSource" yaml:"addSource"`     // 是否添加调用源信息
	SourceRoot  string `json:"sourceRoot" yaml:"sourceRoot"`   // 用于裁剪文件路径
}

// NewDevDefaultConfig 创建开发环境的默认日志配置
// 参数 sourceRoot 推荐设置为你的项目根目录，例如 genesis，以获得更简洁的调用源信息。
func NewDevDefaultConfig(sourceRoot string) *Config {
	return &Config{
		Level:       "debug",
		Format:      "console",
		Output:      "stdout",
		EnableColor: true,
		AddSource:   true,
		SourceRoot:  sourceRoot,
	}
}

// NewProdDefaultConfig 创建生产环境的默认日志配置
// 参数 sourceRoot 推荐设置为你的项目根目录，例如 genesis，以获得更简洁的调用源信息。
func NewProdDefaultConfig(sourceRoot string) *Config {
	return &Config{
		Level:       "info",
		Format:      "json",
		Output:      "stdout",
		EnableColor: false,
		AddSource:   true,
		SourceRoot:  sourceRoot,
	}
}

// validate 验证配置的有效性（内部使用）
//
// 检查 Level 和 Format 是否在有效范围内，并为空值设置默认值。
//
// 返回的错误：
//   - invalid log level: 不支持的日志级别
//   - invalid format: 不支持的输出格式
func (c *Config) validate() error {
	// 设置默认值
	if c.Level == "" {
		c.Level = "info"
	}
	if c.Format == "" {
		c.Format = "console"
	}
	if c.Output == "" {
		c.Output = "stdout"
	}

	if _, err := ParseLevel(c.Level); err != nil {
		return err
	}
	format := strings.ToLower(c.Format)
	if format != "json" && format != "console" {
		return fmt.Errorf("invalid format: %s, must be json or console", c.Format)
	}
	// Output 字段可以是 stdout, stderr 或文件路径，不做严格校验
	return nil
}
