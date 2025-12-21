package idgen

import (
	"fmt"
)

// format64 将 int64 转换为字符串
func format64(id int64) string {
	return fmt.Sprintf("%d", id)
}
