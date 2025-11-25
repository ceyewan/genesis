package xerrors

import (
	"errors"
	"testing"
)

func TestWrap(t *testing.T) {
	// nil 错误应返回 nil
	if err := Wrap(nil, "context"); err != nil {
		t.Errorf("Wrap(nil) = %v，期望 nil", err)
	}

	// 包装后的错误应包含消息
	base := errors.New("base error")
	wrapped := Wrap(base, "context")
	if wrapped == nil {
		t.Fatal("Wrap(err) = nil，期望非 nil")
	}
	if wrapped.Error() != "context: base error" {
		t.Errorf("Wrap(err).Error() = %q，期望 %q", wrapped.Error(), "context: base error")
	}

	// 应保留错误链
	if !errors.Is(wrapped, base) {
		t.Error("errors.Is(wrapped, base) = false，期望 true")
	}
}

func TestWrapf(t *testing.T) {
	// nil 错误应返回 nil
	if err := Wrapf(nil, "user %d", 123); err != nil {
		t.Errorf("Wrapf(nil) = %v，期望 nil", err)
	}

	// 包装后的错误应包含格式化消息
	base := errors.New("not found")
	wrapped := Wrapf(base, "user %d", 123)
	if wrapped.Error() != "user 123: not found" {
		t.Errorf("Wrapf(err).Error() = %q，期望 %q", wrapped.Error(), "user 123: not found")
	}
}

func TestWithCode(t *testing.T) {
	// nil 错误应返回 nil
	if err := WithCode(nil, "CODE"); err != nil {
		t.Errorf("WithCode(nil) = %v，期望 nil", err)
	}

	// 带码错误应包含 code
	base := errors.New("cache miss")
	coded := WithCode(base, "CACHE_MISS")
	if coded.Error() != "[CACHE_MISS] cache miss" {
		t.Errorf("WithCode(err).Error() = %q，期望 %q", coded.Error(), "[CACHE_MISS] cache miss")
	}

	// GetCode 应能提取 code
	if code := GetCode(coded); code != "CACHE_MISS" {
		t.Errorf("GetCode(coded) = %q，期望 %q", code, "CACHE_MISS")
	}

	// 包装后的带码错误依然应有 code
	wrapped := Wrap(coded, "operation failed")
	if code := GetCode(wrapped); code != "CACHE_MISS" {
		t.Errorf("GetCode(wrapped) = %q，期望 %q", code, "CACHE_MISS")
	}
}

func TestMust(t *testing.T) {
	// 无错误应返回值
	v := Must(42, nil)
	if v != 42 {
		t.Errorf("Must(42, nil) = %d，期望 42", v)
	}

	// 有错误应 panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("Must(_, err) 未触发 panic")
		}
	}()
	Must(0, errors.New("error"))
}

func TestMustOK(t *testing.T) {
	// ok=true 应返回值
	v := MustOK(42, true)
	if v != 42 {
		t.Errorf("MustOK(42, true) = %d，期望 42", v)
	}

	// ok=false 应 panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustOK(_, false) 未触发 panic")
		}
	}()
	MustOK(0, false)
}

func TestCollector(t *testing.T) {
	var c Collector

	// 未收集到错误
	if err := c.Err(); err != nil {
		t.Errorf("Collector.Err() = %v，期望 nil", err)
	}

	// 收集 nil（应被忽略）
	c.Collect(nil)
	if err := c.Err(); err != nil {
		t.Errorf("Collect(nil) 后 Err() = %v，期望 nil", err)
	}

	// 收集第一个错误
	err1 := errors.New("error 1")
	c.Collect(err1)
	if err := c.Err(); err != err1 {
		t.Errorf("Collect(err1) 后 Err() = %v，期望 %v", err, err1)
	}

	// 收集第二个错误（应被忽略）
	err2 := errors.New("error 2")
	c.Collect(err2)
	if err := c.Err(); err != err1 {
		t.Errorf("Collect(err2) 后 Err() = %v，期望 %v（第一个错误）", err, err1)
	}
}

func TestCombine(t *testing.T) {
	// 无错误
	if err := Combine(); err != nil {
		t.Errorf("Combine() = %v，期望 nil", err)
	}

	// 全为 nil
	if err := Combine(nil, nil); err != nil {
		t.Errorf("Combine(nil, nil) = %v，期望 nil", err)
	}

	// 单个错误
	err1 := errors.New("error 1")
	if err := Combine(nil, err1, nil); err != err1 {
		t.Errorf("Combine(nil, err1, nil) = %v，期望 %v", err, err1)
	}

	// 多个错误
	err2 := errors.New("error 2")
	combined := Combine(err1, err2)
	multi, ok := combined.(*MultiError)
	if !ok {
		t.Fatalf("Combine(err1, err2) 类型 = %T，期望 *MultiError", combined)
	}
	if len(multi.Errors) != 2 {
		t.Errorf("multi.Errors 长度 = %d，期望 2", len(multi.Errors))
	}

	// errors.Is 应能匹配 MultiError
	if !errors.Is(combined, err1) {
		t.Error("errors.Is(combined, err1) = false，期望 true")
	}
	if !errors.Is(combined, err2) {
		t.Error("errors.Is(combined, err2) = false，期望 true")
	}
}

func TestSentinelErrors(t *testing.T) {
	// 哨兵错误应可用 errors.Is 匹配
	err := Wrap(ErrNotFound, "user lookup")
	if !errors.Is(err, ErrNotFound) {
		t.Error("errors.Is(wrapped, ErrNotFound) = false，期望 true")
	}

	// 不同的哨兵错误不应匹配
	if errors.Is(err, ErrTimeout) {
		t.Error("errors.Is(wrapped, ErrTimeout) = true，期望 false")
	}
}

func TestReExports(t *testing.T) {
	// New 应能正常工作
	err := New("test error")
	if err.Error() != "test error" {
		t.Errorf("New().Error() = %q，期望 %q", err.Error(), "test error")
	}

	// Is 应能正常工作
	if !Is(Wrap(err, "ctx"), err) {
		t.Error("Is(Wrap(err), err) = false，期望 true")
	}

	// Join 应能正常工作
	err1 := New("err1")
	err2 := New("err2")
	joined := Join(err1, err2)
	if !Is(joined, err1) || !Is(joined, err2) {
		t.Error("Join 合并的错误应能被 Is 匹配")
	}
}
