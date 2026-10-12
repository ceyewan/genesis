package cache

import (
	"testing"

	"github.com/ceyewan/genesis/cache/serializer"
)

func FuzzCacheSliceDestination(f *testing.F) {
	for _, value := range []string{`{"id":1}`, `null`, `[1,2]`, `"text"`, `{`} {
		f.Add(value, uint8(0))
		f.Add(value, uint8(4))
	}
	f.Fuzz(func(t *testing.T, value string, kind uint8) {
		cache := &redisCache{serializer: &serializer.JSONSerializer{}}
		var dest any
		switch kind % 6 {
		case 0:
			dest = new([]map[string]any)
		case 1:
			dest = new([]*map[string]any)
		case 2:
			dest = new([]string)
		case 3:
			dest = nil
		case 4:
			dest = (*[]string)(nil)
		case 5:
			dest = 42
		}
		// 畸形数据/目标可以返回错误，不能触发反射 panic。
		_ = cache.unmarshalSlice([]string{value}, dest)
	})
}
