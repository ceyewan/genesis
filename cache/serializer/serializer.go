package serializer

import (
	"encoding/json"
	"fmt"

	"github.com/vmihailenco/msgpack/v5"
)

// 错误定义
var (
	// ErrUnsupportedSerializer 不支持的序列化器类型
	ErrUnsupportedSerializer = fmt.Errorf("unsupported serializer type")
)

// Serializer 定义序列化接口
type Serializer interface {
	Marshal(value any) ([]byte, error)
	Unmarshal(data []byte, dest any) error
}

// JSONSerializer JSON 序列化器
type JSONSerializer struct{}

// Marshal 序列化为 JSON
func (j *JSONSerializer) Marshal(value any) ([]byte, error) {
	return json.Marshal(value)
}

// Unmarshal 从 JSON 反序列化
func (j *JSONSerializer) Unmarshal(data []byte, dest any) error {
	return json.Unmarshal(data, dest)
}

// MessagePackSerializer MessagePack 序列化器
type MessagePackSerializer struct{}

// Marshal 序列化为 MessagePack
// MessagePack 比 JSON 更高效：序列化速度快 2-3 倍，数据体积小 20-30%
func (m *MessagePackSerializer) Marshal(value any) ([]byte, error) {
	return msgpack.Marshal(value)
}

// Unmarshal 从 MessagePack 反序列化
func (m *MessagePackSerializer) Unmarshal(data []byte, dest any) error {
	return msgpack.Unmarshal(data, dest)
}

// New 创建序列化器
//
// 支持的序列化器类型:
//   - "json": 标准库 JSON 序列化，兼容性最好
//   - "msgpack": MessagePack 二进制序列化，性能更优
func New(serializerType string) (Serializer, error) {
	switch serializerType {
	case "json", "":
		return &JSONSerializer{}, nil
	case "msgpack":
		return &MessagePackSerializer{}, nil
	default:
		return nil, ErrUnsupportedSerializer
	}
}
