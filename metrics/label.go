package metrics

// Label 指标标签
type Label struct {
	Key   string
	Value string
}

// L 创建标签实例
func L(key, value string) Label {
	return Label{Key: key, Value: value}
}
