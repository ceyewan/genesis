package mq

import "context"

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func cloneHeaders(headers Headers) Headers {
	if len(headers) == 0 {
		return nil
	}
	clone := make(Headers, len(headers))
	for k, v := range headers {
		clone[k] = v
	}
	return clone
}
