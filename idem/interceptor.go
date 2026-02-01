package idem

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/ceyewan/genesis/clog"
)

// UnaryServerInterceptor 创建 gRPC 一元服务端拦截器
// 为每个 gRPC 调用提供幂等性保护
//
// 使用示例:
//
//	s := grpc.NewServer(
//	    grpc.UnaryInterceptor(idem.UnaryServerInterceptor()),
//	)
func (i *idem) UnaryServerInterceptor(opts ...InterceptorOption) grpc.UnaryServerInterceptor {
	// 应用选项
	opt := interceptorOptions{
		metadataKey: "x-idem-key",
	}
	for _, o := range opts {
		o(&opt)
	}

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// 从 metadata 获取幂等键
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			// 没有 metadata，直接调用 handler
			return handler(ctx, req)
		}

		keys := md.Get(opt.metadataKey)
		if len(keys) == 0 {
			// 没有幂等键，直接调用 handler
			return handler(ctx, req)
		}

		key := keys[0]
		if key == "" {
			// 幂等键为空，直接调用 handler
			return handler(ctx, req)
		}

		if i.logger != nil {
			i.logger.Debug("gRPC call with idem key",
				clog.String("key", key),
				clog.String("method", info.FullMethod))
		}

		cachedResp, token, locked, err := i.waitForResultOrLock(ctx, key)
		if err != nil {
			if i.logger != nil {
				i.logger.Error("failed to wait for gRPC idem result", clog.Error(err), clog.String("key", key))
			}
			return nil, err
		}
		if !locked {
			if i.logger != nil {
				i.logger.Debug("idem cache hit for gRPC call", clog.String("key", key))
			}
			if msg, ok := decodeCachedGRPCResponse(cachedResp); ok {
				return msg, nil
			}
			if i.logger != nil {
				i.logger.Error("failed to decode cached gRPC response", clog.String("key", key))
			}
			return nil, ErrResultNotFound
		}

		lockReleased := false
		defer func() {
			if lockReleased {
				return
			}
			if err := i.store.Unlock(ctx, key, token); err != nil {
				if i.logger != nil {
					i.logger.Error("failed to unlock idem key", clog.Error(err), clog.String("key", key))
				}
			}
		}()
		stopRefresh := i.startLockRefresh(key, token)
		defer stopRefresh()

		result, err := handler(ctx, req)

		if err == nil && result != nil {
			if msg, ok := result.(proto.Message); ok {
				if anyMsg, err := anypb.New(msg); err == nil {
					if respBytes, err := proto.Marshal(anyMsg); err == nil {
						if err := i.store.SetResult(ctx, key, respBytes, i.cfg.DefaultTTL, token); err != nil {
							if i.logger != nil {
								i.logger.Error("failed to cache gRPC response", clog.Error(err), clog.String("key", key))
							}
						} else {
							lockReleased = true
						}
					}
				} else if i.logger != nil {
					i.logger.Error("failed to wrap gRPC response", clog.Error(err), clog.String("key", key))
				}
			} else if i.logger != nil {
				i.logger.Warn("skip caching non-proto gRPC response", clog.String("key", key))
			}
		}

		return result, err
	}
}

func decodeCachedGRPCResponse(cachedResp []byte) (proto.Message, bool) {
	var anyMsg anypb.Any
	if err := proto.Unmarshal(cachedResp, &anyMsg); err == nil {
		msg, err := anypb.UnmarshalNew(&anyMsg, proto.UnmarshalOptions{})
		if err == nil {
			return msg, true
		}
	}
	return nil, false
}
