package types

// Generator 通用 ID 生成器接口
type Generator interface {
	// String 返回字符串形式的 ID (UUID / Snowflake string)
	String() string
}

// Int64Generator 支持数字 ID 的生成器 (主要用于 Snowflake)
type Int64Generator interface {
	Generator
	// Int64 返回 int64 形式的 ID
	Int64() (int64, error)
}
