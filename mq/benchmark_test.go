package mq

import (
	"testing"

	"github.com/ceyewan/genesis/clog"
)

func BenchmarkMQMiddleware(b *testing.B) {
	handler := Chain(WithRecover(clog.Discard()), WithLogging(clog.Discard()), WithRetry(DefaultRetryConfig(), clog.Discard()))(func(Message) error { return nil })
	message := &mockMessage{}
	b.ReportAllocs()
	for b.Loop() {
		if err := handler(message); err != nil {
			b.Fatal(err)
		}
	}
}
