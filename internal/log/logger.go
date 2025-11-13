package log

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// ExitFunc 退出函数类型，用于测试时模拟 os.Exit 行为
var ExitFunc = os.Exit

// SetExitFunc 设置退出函数，用于测试时替换默认的 os.Exit
func SetExitFunc(fn func(int)) {
	ExitFunc = fn
}

// Logger 定义统一的日志记录接口
// 封装 zap.Logger 提供类型安全的使用方式和命名空间支持
type Logger interface {
	// Debug 记录调试级别的日志
	Debug(msg string, fields ...zap.Field)

	// Info 记录信息级别的日志
	Info(msg string, fields ...zap.Field)

	// Warn 记录警告级别的日志
	Warn(msg string, fields ...zap.Field)

	// Error 记录错误级别的日志
	Error(msg string, fields ...zap.Field)

	// Fatal 记录致命错误级别的日志并退出程序
	Fatal(msg string, fields ...zap.Field)

	// With 创建带有额外字段的子日志器
	With(fields ...zap.Field) Logger

	// WithOptions 创建带有选项的子日志器
	WithOptions(opts ...zap.Option) Logger

	// Namespace 创建带有层次化命名空间的子日志器
	Namespace(name string) Logger
}

// zapLogger 封装 zap.Logger 的具体实现
// 添加命名空间支持和优化的字段管理
type zapLogger struct {
	*zap.Logger        // 底层的 zap.Logger 实例
	namespace   string // 层次化命名空间路径，如 "service.module.component"
}

// WithNamespaceField 创建命名空间字段（用于内部实现）
// 确保命名空间字段的一致性和类型安全
func WithNamespaceField(name string) zap.Field {
	return zap.String("namespace", name)
}

// rotationConfig 日志轮转配置
// 基于 lumberjack 库的配置结构
type rotationConfig struct {
	MaxSize    int  // 单个日志文件最大大小（MB）
	MaxBackups int  // 保留的备份文件数量
	MaxAge     int  // 日志文件最大保留天数
	Compress   bool // 是否压缩备份文件
}

// config 内部配置结构，避免循环依赖
// 通过反射从外部 Config 结构体解析而来
type config struct {
	Level       string          // 日志级别
	Format      string          // 输出格式
	Output      string          // 输出目标
	AddSource   bool            // 是否包含源码信息
	EnableColor bool            // 是否启用颜色
	RootPath    string          // 项目根路径
	Rotation    *rotationConfig // 日志轮转配置
}

// NewLogger 创建新的日志器实例
// 根据配置创建 zap.Logger 并封装为 clog.Logger 接口
//
// 参数：
//   - cfg: 配置对象，可以是 *clog.Config 或兼容的结构体
//   - namespace: 层次化命名空间路径
//
// 返回：
//   - Logger: 封装后的日志器实例
//   - error: 创建失败时的错误
//
// 功能特性：
//   - 支持文件输出和日志轮转
//   - 自动创建输出目录
//   - 优化性能的配置设置
func NewLogger(cfg interface{}, namespace string) (Logger, error) {
	// 类型断言获取配置
	config := parseConfig(cfg)

	// 创建 zap 配置
	zapConfig := zap.Config{
		Level:            zap.NewAtomicLevelAt(parseLevel(config.Level)),
		Encoding:         config.Format,
		OutputPaths:      []string{config.Output},
		ErrorOutputPaths: []string{"stderr"},
		EncoderConfig:    buildEncoderConfig(config.Format, config.EnableColor, config.RootPath, config.AddSource),
	}

	// 处理文件输出
	if config.Output != "stdout" && config.Output != "stderr" {
		if err := ensureDir(config.Output); err != nil {
			return nil, err
		}

		// 如果需要轮转，使用自定义的文件写入器
		if config.Rotation != nil {
			// 对于轮转文件，我们需要使用自定义的核心
			return buildLoggerWithRotation(config, namespace)
		}
	}

	// 构建 logger
	buildOptions := []zap.Option{
		zap.AddStacktrace(zapcore.ErrorLevel),
	}
	if config.AddSource {
		// 只添加 AddCaller，不设置固定的 CallerSkip
		buildOptions = append(buildOptions, zap.AddCaller())
	}

	baseLogger, err := zapConfig.Build(buildOptions...)
	if err != nil {
		return nil, err
	}

	// 不再在初始化时添加 namespace 字段，而是在日志记录时动态添加
	return &zapLogger{
		Logger:    baseLogger,
		namespace: namespace,
	}, nil
}

// NewFallbackLogger 创建备用日志器
// 在初始化失败时提供基本的日志功能，确保系统可用性
// 使用 zap.NewProduction 创建生产环境配置的日志器
func NewFallbackLogger() Logger {
	logger, _ := zap.NewProduction()
	return &zapLogger{Logger: logger}
}

// With 添加字段
// With 创建带有额外字段的子日志器
// 自动过滤掉 namespace 字段以避免重复
func (l *zapLogger) With(fields ...zap.Field) Logger {
	// 过滤掉 namespace 字段，避免重复
	var filteredFields []zap.Field
	for _, field := range fields {
		if field.Key != "namespace" {
			filteredFields = append(filteredFields, field)
		}
	}

	return &zapLogger{
		Logger:    l.Logger.With(filteredFields...),
		namespace: l.namespace,
	}
}

// WithOptions 创建带有 zap 选项的子日志器
// 保留原有的命名空间配置
func (l *zapLogger) WithOptions(opts ...zap.Option) Logger {
	// 应用选项，不再自动添加 namespace 字段
	newLogger := l.Logger.WithOptions(opts...)

	return &zapLogger{
		Logger:    newLogger,
		namespace: l.namespace,
	}
}

// Debug 记录 Debug 级别的日志
// 自动添加命名空间字段并调整调用栈信息
func (l *zapLogger) Debug(msg string, fields ...zap.Field) {
	logger := l.Logger.WithOptions(zap.AddCallerSkip(1))
	if l.namespace != "" {
		allFields := make([]zap.Field, len(fields)+1)
		allFields[0] = WithNamespaceField(l.namespace)
		copy(allFields[1:], fields)
		logger.Debug(msg, allFields...)
	} else {
		logger.Debug(msg, fields...)
	}
}

// Info 记录 Info 级别的日志
func (l *zapLogger) Info(msg string, fields ...zap.Field) {
	logger := l.Logger.WithOptions(zap.AddCallerSkip(1))
	if l.namespace != "" {
		allFields := make([]zap.Field, len(fields)+1)
		allFields[0] = WithNamespaceField(l.namespace)
		copy(allFields[1:], fields)
		logger.Info(msg, allFields...)
	} else {
		logger.Info(msg, fields...)
	}
}

// Warn 记录 Warn 级别的日志
func (l *zapLogger) Warn(msg string, fields ...zap.Field) {
	logger := l.Logger.WithOptions(zap.AddCallerSkip(1))
	if l.namespace != "" {
		allFields := make([]zap.Field, len(fields)+1)
		allFields[0] = WithNamespaceField(l.namespace)
		copy(allFields[1:], fields)
		logger.Warn(msg, allFields...)
	} else {
		logger.Warn(msg, fields...)
	}
}

// Error 记录 Error 级别的日志
func (l *zapLogger) Error(msg string, fields ...zap.Field) {
	logger := l.Logger.WithOptions(zap.AddCallerSkip(1))
	if l.namespace != "" {
		allFields := make([]zap.Field, len(fields)+1)
		allFields[0] = WithNamespaceField(l.namespace)
		copy(allFields[1:], fields)
		logger.Error(msg, allFields...)
	} else {
		logger.Error(msg, fields...)
	}
}

// Fatal 记录 Fatal 级别的日志并退出程序
func (l *zapLogger) Fatal(msg string, fields ...zap.Field) {
	logger := l.Logger.WithOptions(zap.AddCallerSkip(1))
	if l.namespace != "" {
		allFields := make([]zap.Field, len(fields)+1)
		allFields[0] = WithNamespaceField(l.namespace)
		copy(allFields[1:], fields)
		logger.Fatal(msg, allFields...)
	} else {
		logger.Fatal(msg, fields...)
	}
	ExitFunc(1)
}

// Namespace 创建子命名空间的 Logger 实例，支持链式调用
// 子命名空间会与父命名空间组合形成完整的层次化路径
func (l *zapLogger) Namespace(name string) Logger {
	var fullNamespace string
	if l.namespace == "" {
		fullNamespace = name
	} else {
		fullNamespace = l.namespace + "." + name
	}

	// 不再在 logger 实例中添加 namespace 字段，避免重复
	// namespace 字段会在日志记录时动态添加
	return &zapLogger{
		Logger:    l.Logger,
		namespace: fullNamespace,
	}
}

// parseConfig 解析配置
func parseConfig(cfg interface{}) *config {
	// 使用反射来解析配置，避免循环依赖
	if cfg == nil {
		return getDefaultConfig()
	}

	// 尝试使用反射获取字段值
	config := &config{
		Level:       getStringField(cfg, "Level", "info"),
		Format:      getStringField(cfg, "Format", "json"),
		Output:      getStringField(cfg, "Output", "stdout"),
		AddSource:   getBoolField(cfg, "AddSource", true),
		EnableColor: getBoolField(cfg, "EnableColor", false),
		RootPath:    getStringField(cfg, "RootPath", ""),
	}

	// 处理轮转配置
	if rotationField := getField(cfg, "Rotation"); rotationField != nil {
		config.Rotation = &rotationConfig{
			MaxSize:    getIntField(rotationField, "MaxSize", 100),
			MaxBackups: getIntField(rotationField, "MaxBackups", 3),
			MaxAge:     getIntField(rotationField, "MaxAge", 7),
			Compress:   getBoolField(rotationField, "Compress", false),
		}
	}

	return config
}

// getDefaultConfig 返回默认配置
func getDefaultConfig() *config {
	return &config{
		Level:       "info",
		Format:      "json",
		Output:      "stdout",
		AddSource:   true,
		EnableColor: false,
		RootPath:    "",
	}
}

// parseLevel 解析日志级别
func parseLevel(level string) zapcore.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// buildLoggerWithRotation 构建带轮转的日志器
func buildLoggerWithRotation(config *config, namespace string) (Logger, error) {
	// 创建编码器
	encoderConfig := buildEncoderConfig(config.Format, config.EnableColor, config.RootPath, config.AddSource)
	encoder := createEncoder(config.Format, encoderConfig)

	// 创建轮转写入器
	rotatingWriter := &lumberjack.Logger{
		Filename:   config.Output,
		MaxSize:    config.Rotation.MaxSize,
		MaxBackups: config.Rotation.MaxBackups,
		MaxAge:     config.Rotation.MaxAge,
		Compress:   config.Rotation.Compress,
		LocalTime:  true,
	}

	// 创建核心
	core := zapcore.NewCore(
		encoder,
		zapcore.AddSync(rotatingWriter),
		parseLevel(config.Level),
	)

	// 构建选项
	opts := []zap.Option{
		zap.AddStacktrace(zapcore.ErrorLevel),
	}

	if config.AddSource {
		// 只添加 AddCaller，不设置固定的 CallerSkip
		opts = append(opts, zap.AddCaller())
	}

	// 创建 logger
	logger := zap.New(core, opts...)

	// 不再在初始化时添加 namespace 字段，而是在日志记录时动态添加
	return &zapLogger{
		Logger:    logger,
		namespace: namespace,
	}, nil
}

func ensureDir(filename string) error {
	dir := filepath.Dir(filename)
	return os.MkdirAll(dir, 0755)
}

// 反射辅助函数
func getField(obj interface{}, fieldName string) interface{} {
	if obj == nil {
		return nil
	}

	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil
	}

	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return nil
	}

	return field.Interface()
}

func getStringField(obj interface{}, fieldName, defaultValue string) string {
	field := getField(obj, fieldName)
	if field == nil {
		return defaultValue
	}

	if str, ok := field.(string); ok {
		return str
	}

	return defaultValue
}

func getBoolField(obj interface{}, fieldName string, defaultValue bool) bool {
	field := getField(obj, fieldName)
	if field == nil {
		return defaultValue
	}

	if b, ok := field.(bool); ok {
		return b
	}

	return defaultValue
}

func getIntField(obj interface{}, fieldName string, defaultValue int) int {
	field := getField(obj, fieldName)
	if field == nil {
		return defaultValue
	}

	if i, ok := field.(int); ok {
		return i
	}

	return defaultValue
}
