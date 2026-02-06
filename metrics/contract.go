package metrics

import (
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
)

const (
	// 常见的标签
	LabelService     = "service"
	LabelOperation   = "operation"
	LabelMethod      = "method"
	LabelRoute       = "route"
	LabelStatusClass = "status_class"
	LabelOutcome     = "outcome"
	LabelGRPCCode    = "grpc_code"
)

const (
	// 常见的操作
	OperationHTTPServer = "http.server"
	OperationGRPCServer = "grpc.server"
)

const (
	// 常见的结果
	OutcomeSuccess = "success"
	OutcomeError   = "error"
)

const (
	// 未知路由
	UnknownRoute = "unknown"
)

// HTTPStatusClass 返回 HTTP 状态类标签值：1xx/2xx/3xx/4xx/5xx/unknown
func HTTPStatusClass(status int) string {
	if status < 100 || status > 599 {
		return "unknown"
	}
	return strconv.Itoa(status/100) + "xx"
}

// HTTPOutcome 将 HTTP 状态代码映射到常见的结果
func HTTPOutcome(status int) string {
	if status >= 200 && status < 400 {
		return OutcomeSuccess
	}
	return OutcomeError
}

// GRPCStatusClass 将 gRPC 状态代码转换为稳定的小写类标签
func GRPCStatusClass(code codes.Code) string {
	if code == codes.OK {
		return "ok"
	}
	return strings.ToLower(code.String())
}

// GRPCOutcome 将 gRPC 状态代码映射到常见的结果
func GRPCOutcome(code codes.Code) string {
	if code == codes.OK {
		return OutcomeSuccess
	}
	return OutcomeError
}
