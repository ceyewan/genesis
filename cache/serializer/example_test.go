package serializer_test

import "github.com/ceyewan/genesis/cache/serializer"

func Example() {
	codec, err := serializer.New("json")
	if err != nil {
		return
	}
	_, _ = codec.Marshal(map[string]string{"status": "ok"})
}
