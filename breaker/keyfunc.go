package breaker

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
)

// KeyFunc 从 gRPC 调用上下文中提取熔断 Key
type KeyFunc func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string

// ========================================
// 内置 KeyFunc 实现
// ========================================

// ServiceLevelKey 服务级别 Key（原有行为）
// 使用服务名作为熔断维度
// 返回示例: "etcd:///logic-service"
func ServiceLevelKey() KeyFunc {
	return func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
		return cc.Target()
	}
}

// MethodLevelKey 方法级别 Key
// 按方法进行熔断
// 返回示例: "/pkg.Service/Method"
func MethodLevelKey() KeyFunc {
	return func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
		return fullMethod
	}
}

// BackendLevelKey 后端级别 Key
// 尝试从 Peer 信息中提取真实后端地址
// 返回示例: "10.0.0.1:9001"
// 注意: 需要等连接建立后才能获取 Peer 信息，第一次调用可能回退到服务名
func BackendLevelKey() KeyFunc {
	return func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
		// 尝试从 Peer 信息中获取真实后端地址
		if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
			addr := p.Addr.String()
			if addr != "" {
				return addr
			}
		}
		// 回退到服务名
		return cc.Target()
	}
}

// CompositeKey 组合 Key
// 组合多个 KeyFunc，使用 @ 分隔
// 返回示例: "etcd:///logic-service@10.0.0.1:9001"
func CompositeKey(primary KeyFunc, secondary ...KeyFunc) KeyFunc {
	return func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
		result := primary(ctx, fullMethod, cc)
		for _, kf := range secondary {
			result += "@" + kf(ctx, fullMethod, cc)
		}
		return result
	}
}

// CompositeKeyWithSeparator 使用自定义分隔符组合 Key
func CompositeKeyWithSeparator(separator string, keyFuncs ...KeyFunc) KeyFunc {
	if len(keyFuncs) == 0 {
		return ServiceLevelKey()
	}
	if len(keyFuncs) == 1 {
		return keyFuncs[0]
	}

	return func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
		result := keyFuncs[0](ctx, fullMethod, cc)
		for i := 1; i < len(keyFuncs); i++ {
			result += separator + keyFuncs[i](ctx, fullMethod, cc)
		}
		return result
	}
}
