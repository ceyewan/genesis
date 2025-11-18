package types

import (
	"fmt"
	"strings"
)

const TimeFormat = "2006-01-02T15:04:05.000Z07:00"

// Config 是全局配置 (设计文档 4.1)
type Config struct {
	Level       string `json:"level" yaml:"level"`   // debug|info|warn|error|fatal
	Format      string `json:"format" yaml:"format"` // json|console
	Output      string `json:"output" yaml:"output"` // stdout|stderr|<file path>
	EnableColor bool   `json:"enableColor" yaml:"enableColor"`
	AddSource   bool   `json:"addSource" yaml:"addSource"`
	SourceRoot  string `json:"sourceRoot" yaml:"sourceRoot"` // 用于裁剪文件路径
}

// Validate 验证配置的有效性
func (c *Config) Validate() error {
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

// ContextField 定义 Context 字段提取规则 (设计文档 4.2)
type ContextField struct {
	Key       any                   // Context 中存储的键
	FieldName string                // 输出的最终字段名，如 "ctx.trace_id"
	Required  bool                  // 是否必须存在
	Extract   func(any) (any, bool) // 可选的自定义提取函数
}

// Option 是实例选项 (设计文档 4.2)
type Option struct {
	NamespaceParts  []string       // 多级命名空间，如 ["order-service", "handler"]
	ContextFields   []ContextField // Context 字段提取规则
	ContextPrefix   string         // Context 字段前缀，默认 "ctx."
	NamespaceJoiner string         // 命名空间连接符，默认 "."
}

// SetDefaults 设置 Option 的默认值
func (o *Option) SetDefaults() {
	if o.ContextPrefix == "" {
		o.ContextPrefix = "ctx."
	}
	if o.NamespaceJoiner == "" {
		o.NamespaceJoiner = "."
	}
}
