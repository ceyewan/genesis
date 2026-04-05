package metrics

import (
	"testing"
)

// TestWithUnit 测试 WithUnit 函数
func TestWithUnit(t *testing.T) {
	opts := &metricOptions{}

	// 测试 WithUnit
	WithUnit("seconds")(opts)

	if opts.Unit != "seconds" {
		t.Errorf("WithUnit() Unit = %v, want %v", opts.Unit, "seconds")
	}

	// 测试不同的单位
	opts2 := &metricOptions{}
	WithUnit("bytes")(opts2)

	if opts2.Unit != "bytes" {
		t.Errorf("WithUnit() Unit = %v, want %v", opts2.Unit, "bytes")
	}
}

// TestMetricOptionsStruct 测试 metricOptions 结构体
func TestMetricOptionsStruct(t *testing.T) {
	opts := &metricOptions{
		Unit: "seconds",
	}

	if opts.Unit != "seconds" {
		t.Errorf("metricOptions.Unit = %v, want %v", opts.Unit, "seconds")
	}

	// 测试空的 metricOptions
	emptyOpts := &metricOptions{}

	if emptyOpts.Unit != "" {
		t.Errorf("Empty metricOptions.Unit should be empty, got %v", emptyOpts.Unit)
	}
}

// TestMetricOptionChaining 测试多个 MetricOption 的链式调用
func TestMetricOptionChaining(t *testing.T) {
	opts := &metricOptions{}

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
