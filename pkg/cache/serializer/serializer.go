package serializer

import (
	"encoding/json"
	"fmt"
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

// New 创建序列化器
func New(serializerType string) (Serializer, error) {
	switch serializerType {
	case "json":
		return &JSONSerializer{}, nil
	case "msgpack":
		return nil, fmt.Errorf("msgpack serializer not implemented yet")
	default:
		return nil, fmt.Errorf("unsupported serializer type: %s", serializerType)
	}
}
