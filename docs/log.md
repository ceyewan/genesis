### **封装日志库的必备方法与设计规范**  
#### **1. 核心：定义抽象接口（项目级 Logger 接口）**  
在项目中创建 `logger/interface.go`，**永不暴露底层库细节**：  
```go
package logger

import "context"

// Field 用于标准化结构化字段
type Field func(*LogBuilder)

// LogBuilder 用于构建日志条目（避免 zap.Field 的类型泄漏）
type LogBuilder struct { 
    // 隐藏具体实现（如 *zap.SugaredLogger）
    data map[string]interface{} 
}

// Logger 是项目唯一日志入口
type Logger interface {
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)

    // 关键：支持 context 透传（自动注入 RequestID/TraceID）
    DebugContext(ctx context.Context, msg string, fields ...Field)
    InfoContext(ctx context.Context, msg string, fields ...Field)
    WarnContext(ctx context.Context, msg string, fields ...Field)
    ErrorContext(ctx context.Context, msg string, fields ...Field)

    // 用于添加上下文字段（如 With("user_id", userID)）
    With(fields ...Field) Logger

    // 动态调整日志级别（生产环境必备）
    SetLevel(level string) error

    // 确保日志持久化（进程退出前调用）
    Flush()
}
```

#### **2. 必须包含的 6 个核心方法组**  
| **方法类别**       | **具体方法**                          | **为什么必需**                                                                 |
|--------------------|---------------------------------------|-----------------------------------------------------------------------------|
| **基础日志**       | `Debug/Info/Warn/Error(msg, fields)`  | 标准分级日志，`fields` 必须是 `...Field` 类型（避免 `map[string]interface{}`） |
| **Context 集成**   | `*Context(ctx, msg, fields)`          | **99% 的线上问题靠此定位**：自动从 `context` 提取 `request_id`、`trace_id` 等 |
| **上下文增强**     | `With(fields ...Field)`               | 将通用字段（如 `service="order"`）绑定到子 Logger，避免重复传参               |
| **动态配置**       | `SetLevel(level string)`              | 允许运行时调整级别（如 `curl /debug/set-log-level?level=debug`）             |
| **生命周期管理**   | `Flush()`                             | 异步日志需强制刷盘（进程退出/滚动日志前调用）                                |
| **错误增强**       | `Error` 方法自动处理 `error` 类型     | 当字段中含 `error` 时，自动拆解为 `err_msg` + `err_stack`（见下文示例）      |

#### **3. 设计规范：字段处理与错误增强**  
**错误处理示例**（避免原始 `err.Error()` 丢失上下文）：  
```go
// 封装 error 字段的生成函数
func ErrorField(err error) Field {
    return func(b *LogBuilder) {
        if err == nil {
            return
        }
        b.data["err_msg"] = err.Error() // 标准化错误信息
        b.data["err_stack"] = extractStack(err) // 自定义：提取 stack trace
        // 可扩展：b.data["err_code"] = errorCodeMap[err]
    }
}

// 在业务代码中使用
logger.Error("DB query failed", logger.ErrorField(err), logger.String("sql", sql))
// 输出: {"level":"error", "err_msg":"timeout", "err_stack":"goroutine...", "sql":"SELECT ..."}
```

**Context 集成规范**：  
- 所有 `*Context` 方法需从 `context` 中提取以下字段：  
  ```go
  func getRequestID(ctx context.Context) string {
      return ctx.Value("request_id").(string) // 实际应使用 typed key 避免冲突
  }
  ```
- **禁止**在业务代码中手动添加 `request_id`：  
  ```go
  // 错误 ❌
  logger.Info("user login", logger.String("request_id", reqID))
  
  // 正确 ✅（中间件自动注入 context）
  logger.InfoContext(ctx, "user login")
  ```