package serializer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestJSONSerializer 测试 JSON 序列化器
func TestJSONSerializer(t *testing.T) {
	t.Run("Marshal and Unmarshal string", func(t *testing.T) {
		s := &JSONSerializer{}

		original := "hello world"
		data, err := s.Marshal(original)
		require.NoError(t, err)

		var got string
		err = s.Unmarshal(data, &got)
		require.NoError(t, err)
		require.Equal(t, original, got)
	})

	t.Run("Marshal and Unmarshal map", func(t *testing.T) {
		s := &JSONSerializer{}

		original := map[string]string{"name": "alice", "city": "NYC"}
		data, err := s.Marshal(original)
		require.NoError(t, err)

		var got map[string]string
		err = s.Unmarshal(data, &got)
		require.NoError(t, err)
		require.Equal(t, "alice", got["name"])
		require.Equal(t, "NYC", got["city"])
	})

	t.Run("Marshal and Unmarshal struct", func(t *testing.T) {
		s := &JSONSerializer{}

		type User struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}

		original := User{ID: 1, Name: "alice"}
		data, err := s.Marshal(original)
		require.NoError(t, err)

		var got User
		err = s.Unmarshal(data, &got)
		require.NoError(t, err)
		require.Equal(t, 1, got.ID)
		require.Equal(t, "alice", got.Name)
	})

	t.Run("Marshal and Unmarshal slice", func(t *testing.T) {
		s := &JSONSerializer{}

		original := []int{1, 2, 3, 4, 5}
		data, err := s.Marshal(original)
		require.NoError(t, err)

		var got []int
		err = s.Unmarshal(data, &got)
		require.NoError(t, err)
		require.Equal(t, []int{1, 2, 3, 4, 5}, got)
	})

	t.Run("Marshal and Unmarshal nested struct", func(t *testing.T) {
		s := &JSONSerializer{}

		type Address struct {
			City  string `json:"city"`
			Street string `json:"street"`
		}

		type User struct {
			ID      int     `json:"id"`
			Name    string  `json:"name"`
			Address Address `json:"address"`
		}

		original := User{
			ID:   1,
			Name: "alice",
			Address: Address{
				City:   "NYC",
				Street: "5th Avenue",
			},
		}

		data, err := s.Marshal(original)
		require.NoError(t, err)

		var got User
		err = s.Unmarshal(data, &got)
		require.NoError(t, err)
		require.Equal(t, 1, got.ID)
		require.Equal(t, "alice", got.Name)
		require.Equal(t, "NYC", got.Address.City)
		require.Equal(t, "5th Avenue", got.Address.Street)
	})

	t.Run("Unmarshal to nil pointer", func(t *testing.T) {
		s := &JSONSerializer{}

		data := []byte(`{"name":"alice"}`)
		var got map[string]string
		err := s.Unmarshal(data, &got)
		require.NoError(t, err)
		require.Equal(t, "alice", got["name"])
	})
}

// TestMessagePackSerializer 测试 MessagePack 序列化器
func TestMessagePackSerializer(t *testing.T) {
	t.Run("Marshal and Unmarshal string", func(t *testing.T) {
		s := &MessagePackSerializer{}

		original := "hello world"
		data, err := s.Marshal(original)
		require.NoError(t, err)

		var got string
		err = s.Unmarshal(data, &got)
		require.NoError(t, err)
		require.Equal(t, original, got)
	})

	t.Run("Marshal and Unmarshal map", func(t *testing.T) {
		s := &MessagePackSerializer{}

		original := map[string]string{"name": "alice", "city": "NYC"}
		data, err := s.Marshal(original)
		require.NoError(t, err)

		var got map[string]string
		err = s.Unmarshal(data, &got)
		require.NoError(t, err)
		require.Equal(t, "alice", got["name"])
		require.Equal(t, "NYC", got["city"])
	})

	t.Run("Marshal and Unmarshal struct", func(t *testing.T) {
		s := &MessagePackSerializer{}

		type User struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}

		original := User{ID: 1, Name: "alice"}
		data, err := s.Marshal(original)
		require.NoError(t, err)

		var got User
		err = s.Unmarshal(data, &got)
		require.NoError(t, err)
		require.Equal(t, 1, got.ID)
		require.Equal(t, "alice", got.Name)
	})

	t.Run("Marshal and Unmarshal slice", func(t *testing.T) {
		s := &MessagePackSerializer{}

		original := []int{1, 2, 3, 4, 5}
		data, err := s.Marshal(original)
		require.NoError(t, err)

		var got []int
		err = s.Unmarshal(data, &got)
		require.NoError(t, err)
		require.Equal(t, []int{1, 2, 3, 4, 5}, got)
	})

	t.Run("Marshal and Unmarshal nested struct", func(t *testing.T) {
		s := &MessagePackSerializer{}

		type Address struct {
			City  string `json:"city"`
			Street string `json:"street"`
		}

		type User struct {
			ID      int     `json:"id"`
			Name    string  `json:"name"`
			Address Address `json:"address"`
		}

		original := User{
			ID:   1,
			Name: "alice",
			Address: Address{
				City:   "NYC",
				Street: "5th Avenue",
			},
		}

		data, err := s.Marshal(original)
		require.NoError(t, err)

		var got User
		err = s.Unmarshal(data, &got)
		require.NoError(t, err)
		require.Equal(t, 1, got.ID)
		require.Equal(t, "alice", got.Name)
		require.Equal(t, "NYC", got.Address.City)
		require.Equal(t, "5th Avenue", got.Address.Street)
	})

	t.Run("Marshal and Unmarshal time.Time", func(t *testing.T) {
		s := &MessagePackSerializer{}

		original := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		data, err := s.Marshal(original)
		require.NoError(t, err)

		var got time.Time
		err = s.Unmarshal(data, &got)
		require.NoError(t, err)
		require.True(t, original.Equal(got))
	})
}

// TestNew 测试 New 函数
func TestNew(t *testing.T) {
	t.Run("New with json", func(t *testing.T) {
		s, err := New("json")
		require.NoError(t, err)
		require.IsType(t, &JSONSerializer{}, s)
	})

	t.Run("New with empty string defaults to json", func(t *testing.T) {
		s, err := New("")
		require.NoError(t, err)
		require.IsType(t, &JSONSerializer{}, s)
	})

	t.Run("New with msgpack", func(t *testing.T) {
		s, err := New("msgpack")
		require.NoError(t, err)
		require.IsType(t, &MessagePackSerializer{}, s)
	})

	t.Run("New with unsupported type returns error", func(t *testing.T) {
		s, err := New("unsupported")
		require.ErrorIs(t, err, ErrUnsupportedSerializer)
		require.Nil(t, s)
	})

	t.Run("New with gob returns error", func(t *testing.T) {
		s, err := New("gob")
		require.ErrorIs(t, err, ErrUnsupportedSerializer)
		require.Nil(t, s)
	})
}

// TestSerializer_Compatibility 测试序列化器兼容性
func TestSerializer_Compatibility(t *testing.T) {
	t.Run("JSON can unmarshal what JSON marshaled", func(t *testing.T) {
		json := &JSONSerializer{}

		original := map[string]any{
			"name": "alice",
			"age":  30,
			"tags": []string{"dev", "go"},
		}

		// JSON marshal -> JSON unmarshal
		jsonData, err := json.Marshal(original)
		require.NoError(t, err)

		var gotJSON map[string]any
		err = json.Unmarshal(jsonData, &gotJSON)
		require.NoError(t, err)
		require.Equal(t, "alice", gotJSON["name"])
		require.Equal(t, float64(30), gotJSON["age"])
	})

	t.Run("MessagePack produces smaller output than JSON", func(t *testing.T) {
		json := &JSONSerializer{}
		msgpack := &MessagePackSerializer{}

		original := map[string]any{
			"name": "alice",
			"age":  30,
			"tags": []string{"dev", "go"},
		}

		jsonData, err := json.Marshal(original)
		require.NoError(t, err)

		msgpackData, err := msgpack.Marshal(original)
		require.NoError(t, err)

		// MessagePack 应该比 JSON 小
		require.Less(t, len(msgpackData), len(jsonData))
	})
}

// TestSerializer_EdgeCases 测试边界情况
func TestSerializer_EdgeCases(t *testing.T) {
	t.Run("JSON marshal nil", func(t *testing.T) {
		s := &JSONSerializer{}
		data, err := s.Marshal(nil)
		require.NoError(t, err)
		require.Equal(t, []byte("null"), data)
	})

	t.Run("MessagePack marshal nil", func(t *testing.T) {
		s := &MessagePackSerializer{}
		data, err := s.Marshal(nil)
		require.NoError(t, err)
		require.Equal(t, []byte{0xc0}, data) // msgpack nil encoding
	})

	t.Run("JSON unmarshal to wrong type returns error", func(t *testing.T) {
		s := &JSONSerializer{}
		data := []byte(`{"name":"alice"}`)
		var got int
		err := s.Unmarshal(data, &got)
		require.Error(t, err)
	})

	t.Run("MessagePack unmarshal to wrong type returns error", func(t *testing.T) {
		s := &MessagePackSerializer{}
		data := []byte{0x81, 0xa4, 0x6e, 0x61, 0x6d, 0x65, 0xa5, 0x61, 0x6c, 0x69, 0x63, 0x65} // {"name":"alice"}
		var got int
		err := s.Unmarshal(data, &got)
		require.Error(t, err)
	})

	t.Run("JSON marshal invalid type returns error", func(t *testing.T) {
		s := &JSONSerializer{}
		// channel 不能被 JSON 序列化
		ch := make(chan int)
		_, err := s.Marshal(ch)
		require.Error(t, err)
	})
}
