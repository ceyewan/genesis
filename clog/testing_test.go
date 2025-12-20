package clog

import (
	"bytes"
)

// withBuffer 是一个测试专用选项，用于将日志输出写入指定的缓冲区
//
// 此选项仅用于测试，不在生产代码中使用。
//
// 参数：
//
//	buf - 用于捕获日志输出的字节缓冲区
//
// 返回：
//
//	Option - 函数式选项
func withBuffer(buf *bytes.Buffer) Option {
	return func(o *options) {
		o.buffer = buf
	}
}
