package clog

import (
	"fmt"
	"strings"
)

const TimeFormat = "2006-01-02T15:04:05.000Z07:00"

// Config 日志配置结构，定义日志的基本行为
//
// 支持的配置项：
//
//	Level: 日志级别 (debug|info|warn|error|fatal)
//	Format: 输出格式 (json|console)
//	Output: 输出目标 (stdout|stderr|文件路径)
//	EnableColor: 是否启用彩色输出（仅 console 格式）
//	AddSource: 是否显示调用位置信息
//	SourceRoot: 源代码路径前缀，用于裁剪显示的文件路径
//
// 示例：
//
//	config := &clog.Config{
//	    Level:     "info",
//	    Format:    "json",
//	    Output:    "/var/log/app.log",
//	    AddSource: true,
//	}
type Config struct {
	Level       string `json:"level" yaml:"level"`             // debug|info|warn|error|fatal
	Format      string `json:"format" yaml:"format"`           // json|console
	Output      string `json:"output" yaml:"output"`           // stdout|stderr|<file path>
	EnableColor bool   `json:"enableColor" yaml:"enableColor"` // 仅在 console 格式下有效，未实现
	AddSource   bool   `json:"addSource" yaml:"addSource"`
	SourceRoot  string `json:"sourceRoot" yaml:"sourceRoot"` // 用于裁剪文件路径
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
