// internal/connector/adapter/gorm_logger.go
package adapter

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm/logger"

	"github.com/ceyewan/genesis/pkg/clog"
)

// GormLogger 实现 GORM 的 logger.Interface，将日志转发到 clog
type GormLogger struct {
	logger clog.Logger
	level  logger.LogLevel
}

// NewGormLogger 创建一个新的 GORM 日志适配器
func NewGormLogger(logger clog.Logger, level logger.LogLevel) *GormLogger {
	return &GormLogger{
		logger: logger,
		level:  level,
	}
}

// LogMode 设置日志级别
func (l *GormLogger) LogMode(level logger.LogLevel) logger.Interface {
	newLogger := *l
	newLogger.level = level
	return &newLogger
}

// Info 记录信息日志
func (l *GormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Info {
		l.logger.InfoContext(ctx, fmt.Sprintf(msg, data...))
	}
}

// Warn 记录警告日志
func (l *GormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Warn {
		l.logger.WarnContext(ctx, fmt.Sprintf(msg, data...))
	}
}

// Error 记录错误日志
func (l *GormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Error {
		l.logger.ErrorContext(ctx, fmt.Sprintf(msg, data...))
	}
}

// Trace 记录 SQL 跟踪日志
func (l *GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.level <= logger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	fields := []clog.Field{
		clog.String("sql", sql),
		clog.Duration("elapsed", elapsed),
		clog.String("source", "gorm"),
	}

	if err != nil {
		fields = append(fields, clog.Error(err))
		l.logger.ErrorContext(ctx, "SQL execution failed", fields...)
	} else {
		fields = append(fields, clog.Int64("rows", rows))
		l.logger.DebugContext(ctx, "SQL executed", fields...)
	}
}
