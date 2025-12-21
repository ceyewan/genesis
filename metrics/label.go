package metrics

// Label 指标标签结构体
// 用于为指标添加维度信息，实现指标的细粒度分组和筛选
// 标签是 Prometheus 等监控系统的核心概念，允许对指标进行多维度的分析
//
// 标签的使用场景：
//   - HTTP 请求：method（GET/POST）、status（200/500）、endpoint（/api/users）
//   - 数据库操作：table（users）、operation（select）、result（success/error）
//   - 业务指标：user_type（premium/free）、region（us-east-1）、version（v1.0.0）
//
// 标签命名规范：
//   - 使用小写字母和下划线：user_id 而不是 userId
//   - 避免使用保留字：避免使用 "id"、"name" 等通用词汇
//   - 控制标签数量：每个指标的标签数量不宜过多（建议 < 10个）
//   - 标签值相对稳定：避免高基数标签，如用户ID、请求ID等
//
// 使用示例：
//
//	// 创建标签
//	methodLabel := metrics.L("method", "GET")
//	statusLabel := metrics.L("status", "200")
//	endpointLabel := metrics.L("endpoint", "/api/users")
//
//	// 在指标中使用
//	counter.Inc(ctx, methodLabel, statusLabel, endpointLabel)
type Label struct {
	// Key 标签键，表示指标的维度名称
	// 必须符合 Prometheus 标签命名规范
	// 建议：使用小写字母、数字和下划线，以字母开头
	Key string

	// Value 标签值，表示该维度的具体值
	// 可以是任意字符串，但建议使用有意义的值
	// 注意：高基数（大量唯一值）的标签会影响性能
	Value string
}

// L 便捷构造函数，创建一个 Label 实例
// 这个函数名简洁，便于在代码中频繁使用
//
// 参数：
//
//	key   - 标签键，应该描述指标的维度
//	value - 标签值，应该描述该维度的具体取值
//
// 使用示例：
//
//	// 方式1：使用便捷函数
//	counter.Inc(ctx, metrics.L("method", "GET"))
//
//	// 方式2：直接创建结构体
//	counter.Inc(ctx, metrics.Label{Key: "method", Value: "GET"})
//
// 返回：
//
//	Label - 标签实例
func L(key, value string) Label {
	return Label{
		Key:   key,
		Value: value,
	}
}
