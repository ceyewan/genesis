package idem

import (
	"context"
	"encoding/json"
	"reflect"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

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

		// 使用分布式锁确保同一时刻只有一个请求在执行
		locked, err := i.store.Lock(ctx, key, i.cfg.LockTTL)
		if err != nil || !locked {
			// 获取锁失败，可能其他请求正在执行，直接放行
			if i.logger != nil {
				i.logger.Debug("failed to acquire lock, proceeding without cache",
					clog.String("key", key),
					clog.Error(err))
			}
			// 直接执行，不阻塞
			return handler(ctx, req)
		}
		defer func() {
			if err := i.store.Unlock(ctx, key); err != nil {
				if i.logger != nil {
					i.logger.Error("failed to unlock idem key", clog.Error(err), clog.String("key", key))
				}
			}
		}()

		// 再次检查缓存（可能在等待锁的过程中，其他请求已经完成并缓存了结果）
		cachedResp, err := i.store.GetResult(ctx, key)
		if err == nil {
			// 缓存命中，返回缓存的响应
			if i.logger != nil {
				i.logger.Debug("idem cache hit for gRPC call", clog.String("key", key))
			}

			// 执行一次 handler 获取响应类型模板
			result, handlerErr := handler(ctx, req)
			if handlerErr == nil && result != nil {
				// 创建新实例用于反序列化
				cachedResult := reflect.New(reflect.TypeOf(result).Elem()).Interface()

				// 尝试使用 protobuf 反序列化
				if msg, ok := cachedResult.(proto.Message); ok {
					if err := proto.Unmarshal(cachedResp, msg); err == nil {
						return msg, nil
					}
				}

				// 回退到 JSON
				if err := json.Unmarshal(cachedResp, cachedResult); err == nil {
					return cachedResult, nil
				}
			}
		}

		// 缓存未命中，执行 handler
		result, err := handler(ctx, req)

		// 如果执行成功，缓存响应
		if err == nil && result != nil {
			// 优先使用 protobuf 序列化
			if msg, ok := result.(proto.Message); ok {
				if respBytes, err := proto.Marshal(msg); err == nil {
					if err := i.store.SetResult(ctx, key, respBytes, i.cfg.DefaultTTL); err != nil {
						if i.logger != nil {
							i.logger.Error("failed to cache gRPC response", clog.Error(err), clog.String("key", key))
						}
					}
				}
			} else if respBytes, err := json.Marshal(result); err == nil {
				// 回退到 JSON 序列化
				if err := i.store.SetResult(ctx, key, respBytes, i.cfg.DefaultTTL); err != nil {
					if i.logger != nil {
						i.logger.Error("failed to cache gRPC response", clog.Error(err), clog.String("key", key))
					}
				}
			}
		}

		return result, err
	}
}
