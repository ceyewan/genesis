package main

import (
	"errors"
	"fmt"

	"github.com/ceyewan/genesis/xerrors"
)

func main() {
	fmt.Println("=== xerrors 示例 ===")
	fmt.Println()

	// 示例 1: 基础错误包装
	fmt.Println("【示例 1】基础错误包装 (Wrap/Wrapf)")
	fmt.Println("------------------------------------")
	err1 := basicWrapping()
	fmt.Printf("Error chain: %v\n", err1)
	fmt.Println()

	// 示例 2: Sentinel Errors 检查
	fmt.Println("【示例 2】Sentinel Errors 检查")
	fmt.Println("------------------------------------")
	sentinelErrorsDemo()
	fmt.Println()

	// 示例 3: 带错误码的错误
	fmt.Println("【示例 3】带错误码的错误 (WithCode/GetCode)")
	fmt.Println("------------------------------------")
	codedErrorDemo()
	fmt.Println()

	// 示例 4: 错误收集（单个错误）
	fmt.Println("【示例 4】错误收集 - Collector（保留第一个错误）")
	fmt.Println("------------------------------------")
	collectorDemo()
	fmt.Println()

	// 示例 5: 多个错误合并
	fmt.Println("【示例 5】多个错误合并 - Combine")
	fmt.Println("------------------------------------")
	combineDemo()
	fmt.Println()

	// 示例 6: 初始化时使用 Must
	fmt.Println("【示例 6】初始化时使用 Must")
	fmt.Println("------------------------------------")
	mustDemo()
	fmt.Println()

	// 示例 7: 模拟实际场景 - API 错误处理
	fmt.Println("【示例 7】实战场景 - 模拟 API 错误处理")
	fmt.Println("------------------------------------")
	apiSceneDemo()
}

// basicWrapping 演示基础错误包装
func basicWrapping() error {
	// 模拟一个底层错误
	baseErr := errors.New("connection refused")

	// 使用 Wrap 添加上下文
	wrapped := xerrors.Wrap(baseErr, "failed to connect to database")

	// 使用 Wrapf 添加格式化上下文
	wrapped = xerrors.Wrapf(wrapped, "host: %s, port: %d", "localhost", 5432)

	return wrapped
}

// sentinelErrorsDemo 演示 Sentinel Errors 的检查
func sentinelErrorsDemo() {
	// 场景 1: 资源未找到
	err := xerrors.Wrap(xerrors.ErrNotFound, "user not found")
	if xerrors.Is(err, xerrors.ErrNotFound) {
		fmt.Println("✓ 检测到 ErrNotFound")
	}

	// 场景 2: 无效输入
	err = xerrors.ErrInvalidInput
	if xerrors.Is(err, xerrors.ErrInvalidInput) {
		fmt.Println("✓ 检测到 ErrInvalidInput")
	}

	// 场景 3: 超时
	err = xerrors.Wrap(xerrors.ErrTimeout, "request timeout after 30s")
	if xerrors.Is(err, xerrors.ErrTimeout) {
		fmt.Println("✓ 检测到 ErrTimeout")
	}

	// 场景 4: 冲突
	err = xerrors.Wrapf(xerrors.ErrConflict, "user %d already exists", 123)
	if xerrors.Is(err, xerrors.ErrConflict) {
		fmt.Println("✓ 检测到 ErrConflict")
	}

	// 使用 errors.As 提取具体错误
	var sentinel error
	wrappedErr := xerrors.Wrap(xerrors.ErrUnavailable, "service temporarily unavailable")
	if xerrors.As(wrappedErr, &sentinel) {
		fmt.Printf("✓ 提取底层错误: %v\n", sentinel)
	}
}

// codedErrorDemo 演示带错误码的错误处理
func codedErrorDemo() {
	// 场景 1: 缓存未命中
	cacheErr := xerrors.New("cache miss")
	codedErr := xerrors.WithCode(cacheErr, "CACHE_MISS")
	fmt.Printf("Error: %v\n", codedErr)

	code := xerrors.GetCode(codedErr)
	fmt.Printf("Code: %s\n", code)

	// 场景 2: 通过 WithCode 包装 Sentinel Error
	notFoundErr := xerrors.WithCode(xerrors.ErrNotFound, "USER_NOT_FOUND")
	fmt.Printf("Error: %v\n", notFoundErr)

	code = xerrors.GetCode(notFoundErr)
	fmt.Printf("Code: %s\n", code)

	// 场景 3: 在错误链中提取错误码
	wrappedCoded := xerrors.Wrap(codedErr, "operation failed")
	fmt.Printf("Wrapped error: %v\n", wrappedCoded)

	code = xerrors.GetCode(wrappedCoded)
	fmt.Printf("Code from wrapped error: %s\n", code)
}

// collectorDemo 演示错误收集器
func collectorDemo() {
	var collector xerrors.Collector

	// 收集第一个错误
	err1 := errors.New("error 1")
	collector.Collect(err1)
	fmt.Printf("After Collect(err1): %v\n", collector.Err())

	// 尝试收集第二个错误（会被忽略）
	err2 := errors.New("error 2")
	collector.Collect(err2)
	fmt.Printf("After Collect(err2): %v (第一个错误仍被保留)\n", collector.Err())

	// 收集 nil（被忽略）
	collector.Collect(nil)
	fmt.Printf("After Collect(nil): %v\n", collector.Err())

	// 实际应用: 验证多个字段
	fmt.Println("\n实际应用 - 表单验证:")
	if validationErr := validateUserForm("", "invalid-email", -5); validationErr != nil {
		fmt.Printf("Validation error: %v\n", validationErr)
	}
}

// validateUserForm 演示使用 Collector 进行多字段验证
func validateUserForm(name, email string, age int) error {
	var errs xerrors.Collector

	if name == "" {
		errs.Collect(errors.New("name is required"))
	}
	if email == "" || !isValidEmail(email) {
		errs.Collect(errors.New("email is invalid"))
	}
	if age < 0 || age > 150 {
		errs.Collect(errors.New("age must be between 0 and 150"))
	}

	return errs.Err()
}

func isValidEmail(email string) bool {
	return len(email) > 5 && email != "invalid-email"
}

// combineDemo 演示错误合并
func combineDemo() {
	// 场景 1: 无错误
	result := xerrors.Combine()
	fmt.Printf("Combine(): %v\n", result)

	// 场景 2: 全为 nil
	result = xerrors.Combine(nil, nil)
	fmt.Printf("Combine(nil, nil): %v\n", result)

	// 场景 3: 单个错误
	err1 := errors.New("database error")
	result = xerrors.Combine(nil, err1, nil)
	fmt.Printf("Combine(nil, err1, nil): %v\n", result)

	// 场景 4: 多个错误
	err2 := errors.New("cache error")
	err3 := errors.New("validation error")
	combined := xerrors.Combine(err1, err2, err3)
	fmt.Printf("Combine(err1, err2, err3): %v\n", combined)

	// 场景 5: 使用 errors.Is 检查 MultiError 中的具体错误
	if xerrors.Is(combined, err2) {
		fmt.Println("✓ MultiError 中包含 cache error")
	}
	if xerrors.Is(combined, err3) {
		fmt.Println("✓ MultiError 中包含 validation error")
	}
}

// mustDemo 演示 Must 函数（仅用于初始化）
func mustDemo() {
	// 使用 Must 确保值非 nil 且无错误
	value := xerrors.Must(parseInteger("42"))
	fmt.Printf("Parsed value: %d\n", value)

	// 使用 MustOK 进行类型断言
	var i interface{} = 100
	num := xerrors.MustOK(i.(int), true)
	fmt.Printf("Type asserted value: %d\n", num)

	fmt.Println("\n注意: Must 仅应在初始化阶段使用")
	fmt.Println("      在运行时业务逻辑中使用 Must 会导致服务 panic")
}

func parseInteger(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}

// apiSceneDemo 演示实际 API 场景的错误处理
func apiSceneDemo() {
	// 场景: 获取用户信息的 API

	userID := int64(123)

	// 步骤 1: 从数据库查询
	user, err := getUserFromDB(userID)
	if err != nil {
		if xerrors.Is(err, xerrors.ErrNotFound) {
			// 返回 404
			fmt.Printf("返回 HTTP 404: 用户不存在\n")
			fmt.Printf("错误码: %s\n", xerrors.GetCode(err))
			return
		}
		if xerrors.Is(err, xerrors.ErrTimeout) {
			// 返回 503
			fmt.Printf("返回 HTTP 503: 数据库超时\n")
			return
		}
		// 返回 500
		fmt.Printf("返回 HTTP 500: 内部错误: %v\n", err)
		return
	}

	fmt.Printf("✓ 返回 HTTP 200: %s (ID: %d)\n", user.Name, user.ID)
}

type User struct {
	ID   int64
	Name string
}

func getUserFromDB(id int64) (*User, error) {
	// 模拟不同的错误场景
	switch id {
	case 123:
		// 成功
		return &User{ID: 123, Name: "Alice"}, nil
	case 404:
		// 不存在
		return nil, xerrors.WithCode(xerrors.ErrNotFound, "USER_NOT_FOUND")
	case 500:
		// 数据库超时
		return nil, xerrors.Wrap(xerrors.ErrTimeout, "database query timeout")
	default:
		// 未知错误
		return nil, xerrors.WithCode(
			errors.New("unknown user id"),
			"UNKNOWN_ERROR",
		)
	}
}
