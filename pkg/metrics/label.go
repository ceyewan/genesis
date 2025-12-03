package metrics

// Label 定义指标的维度
type Label struct {
	Key   string
	Value string
}

// L 便捷构造函数，创建一个 Label
func L(key, value string) Label {
	return Label{
		Key:   key,
		Value: value,
	}
}
