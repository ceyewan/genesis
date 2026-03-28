package registry

// ServiceInstance 描述一个服务实例。
//
// Endpoints 不是通用 URL 列表，而是 gRPC 地址列表，只接受 `grpc://host:port` 或 `host:port`。
type ServiceInstance struct {
	ID        string            `json:"id"`        // 唯一实例 ID (通常是 UUID)
	Name      string            `json:"name"`      // 服务名称 (如 user-service)
	Version   string            `json:"version"`   // 版本号
	Metadata  map[string]string `json:"metadata"`  // 元数据 (Region, Zone, Weight, Group 等)
	Endpoints []string          `json:"endpoints"` // 服务地址列表 (如 grpc://192.168.1.10:9090)
}

// ServiceEvent 表示一次服务变化事件。
type ServiceEvent struct {
	Type    EventType        // 事件类型 (PUT/DELETE)
	Service *ServiceInstance // 服务实例信息
}

// EventType 表示服务事件类型。
type EventType string

const (
	EventTypePut    EventType = "PUT"    // 服务注册或更新
	EventTypeDelete EventType = "DELETE" // 服务注销
)
