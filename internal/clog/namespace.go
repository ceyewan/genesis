package clog

import (
	"strings"

	"github.com/ceyewan/genesis/clog/types"
)

const NamespaceKey = "namespace"

// getNamespaceString 根据 Option 中的 parts 和 joiner 生成完整的命名空间字符串。
func getNamespaceString(option *types.Option) string {
	if option == nil || len(option.NamespaceParts) == 0 {
		return ""
	}
	return strings.Join(option.NamespaceParts, option.NamespaceJoiner)
}

// addNamespaceFields 将命名空间字段添加到 LogBuilder 中。
func addNamespaceFields(option *types.Option, builder *types.LogBuilder) {
	ns := getNamespaceString(option)
	if ns != "" {
		builder.Data[NamespaceKey] = ns
	}
}
