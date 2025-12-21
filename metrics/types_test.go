package metrics

import (
	"testing"
)

// TestLabel 测试 Label 结构体和 L 函数
func TestLabel(t *testing.T) {
	// 测试 L 函数
	label := L("method", "GET")

	if label.Key != "method" {
		t.Errorf("L().Key = %v, want %v", label.Key, "method")
	}

	if label.Value != "GET" {
		t.Errorf("L().Value = %v, want %v", label.Value, "GET")
	}

	// 测试直接创建结构体
	label2 := Label{
		Key:   "status",
		Value: "200",
	}

	if label2.Key != "status" {
		t.Errorf("Label.Key = %v, want %v", label2.Key, "status")
	}

	if label2.Value != "200" {
		t.Errorf("Label.Value = %v, want %v", label2.Value, "200")
	}
}

// TestWithUnit 测试 WithUnit 函数
func TestWithUnit(t *testing.T) {
	opts := &MetricOptions{}

	// 测试 WithUnit
	WithUnit("seconds")(opts)

	if opts.Unit != "seconds" {
		t.Errorf("WithUnit() Unit = %v, want %v", opts.Unit, "seconds")
	}

	// 测试不同的单位
	opts2 := &MetricOptions{}
	WithUnit("bytes")(opts2)

	if opts2.Unit != "bytes" {
		t.Errorf("WithUnit() Unit = %v, want %v", opts2.Unit, "bytes")
	}
}

// TestEmptyLabels 测试空标签
func TestEmptyLabels(t *testing.T) {
	// 测试 L 函数的边界情况
	label := L("", "")

	if label.Key != "" {
		t.Errorf("L() with empty strings should have empty Key, got %v", label.Key)
	}

	if label.Value != "" {
		t.Errorf("L() with empty strings should have empty Value, got %v", label.Value)
	}
}

// TestLabelWithSpecialChars 测试包含特殊字符的标签
func TestLabelWithSpecialChars(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{"test_key", "test-value"},
		{"method", "GET/POST"},
		{"status_code", "404 Not Found"},
		{"endpoint", "/api/v1/users/{id}"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			label := L(tt.key, tt.value)

			if label.Key != tt.key {
				t.Errorf("L().Key = %v, want %v", label.Key, tt.key)
			}

			if label.Value != tt.value {
				t.Errorf("L().Value = %v, want %v", label.Value, tt.value)
			}
		})
	}
}

// TestMetricOptionsStruct 测试 MetricOptions 结构体
func TestMetricOptionsStruct(t *testing.T) {
	opts := &MetricOptions{
		Unit: "seconds",
	}

	if opts.Unit != "seconds" {
		t.Errorf("MetricOptions.Unit = %v, want %v", opts.Unit, "seconds")
	}

	// 测试空的 MetricOptions
	emptyOpts := &MetricOptions{}

	if emptyOpts.Unit != "" {
		t.Errorf("Empty MetricOptions.Unit should be empty, got %v", emptyOpts.Unit)
	}
}

// TestMetricOptionChaining 测试多个 MetricOption 的链式调用
func TestMetricOptionChaining(t *testing.T) {
	opts := &MetricOptions{}

	// 虽然目前只有一个选项，但测试为将来扩展做准备
	WithUnit("bytes")(opts)

	if opts.Unit != "bytes" {
		t.Errorf("Chained WithUnit() Unit = %v, want %v", opts.Unit, "bytes")
	}

	// 再次调用应该覆盖之前的值
	WithUnit("requests")(opts)

	if opts.Unit != "requests" {
		t.Errorf("Overridden WithUnit() Unit = %v, want %v", opts.Unit, "requests")
	}
}
