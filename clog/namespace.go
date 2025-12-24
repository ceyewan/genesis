package clog

import (
	"log/slog"
	"strings"
)

// namespaceKey 是日志中命名空间的字段名，用于标识服务模块（内部使用）
const namespaceKey = "namespace"

// getNamespaceString 根据 options 中的 parts 和 joiner 生成完整的命名空间字符串。
func getNamespaceString(options *options) string {
	if options == nil || len(options.namespaceParts) == 0 {
		return ""
	}
	return strings.Join(options.namespaceParts, options.namespaceJoiner)
}

// addNamespaceFields 将命名空间字段追加到 attrs 切片中。
func addNamespaceFields(options *options, attrs *[]slog.Attr) {
	ns := getNamespaceString(options)
	if ns != "" {
		*attrs = append(*attrs, slog.String(namespaceKey, ns))
	}
}
