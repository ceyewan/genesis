package serializer

import (
	"encoding/json"
	"fmt"

	"github.com/vmihailenco/msgpack/v5"
)

// Serializer 定义序列化接口
type Serializer interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
}

// JSONSerializer 实现 JSON 序列化
type JSONSerializer struct{}

func (s *JSONSerializer) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (s *JSONSerializer) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// MsgPackSerializer 实现 MessagePack 序列化
type MsgPackSerializer struct{}

func (s *MsgPackSerializer) Marshal(v any) ([]byte, error) {
	return msgpack.Marshal(v)
}

func (s *MsgPackSerializer) Unmarshal(data []byte, v any) error {
	return msgpack.Unmarshal(data, v)
}

// New 创建序列化器
func New(name string) (Serializer, error) {
	switch name {
	case "json":
		return &JSONSerializer{}, nil
	case "msgpack":
		return &MsgPackSerializer{}, nil
	default:
		return nil, fmt.Errorf("不支持的序列化器: %s", name)
	}
}
