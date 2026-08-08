package auth_test

import "github.com/ceyewan/genesis/auth"

func Example() {
	_, _ = auth.New(&auth.Config{SecretKey: "replace-with-at-least-32-random-bytes"})
}
