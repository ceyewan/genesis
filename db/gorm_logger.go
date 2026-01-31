package db

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/clog"
	"gorm.io/gorm/logger"
)

// gormLogger 将 GORM 日志适配到 clog
type gormLogger struct {
	logger clog.Logger
	level  logger.LogLevel
}

// newGormLogger 创建 GORM logger 适配器
// silent 参数控制是否禁用日志输出
func newGormLogger(log clog.Logger, silent bool) logger.Interface {
	level := logger.Info
	if silent {
		level = logger.Silent
	}
	return &gormLogger{
		logger: log,
		level:  level,
	}
}

// LogMode 设置日志级别
func (l *gormLogger) LogMode(level logger.LogLevel) logger.Interface {
	newLogger := *l
	newLogger.level = level
	return &newLogger
}

// Info 记录 info 级别日志
func (l *gormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Info {
		l.logger.Info(fmt.Sprintf(msg, data...))
	}
}

// Warn 记录 warn 级别日志
func (l *gormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Warn {
		l.logger.Warn(fmt.Sprintf(msg, data...))
	}
}

// Error 记录 error 级别日志
func (l *gormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.level >= logger.Error {
		l.logger.Error(fmt.Sprintf(msg, data...))
	}
}

// Trace 记录 SQL 执行日志
func (l *gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)

	sql, rows := fc()

	switch {
	case err != nil && l.level >= logger.Error:
		l.logger.Error("sql error",
			clog.String("duration", elapsed.String()),
			clog.String("sql", sql),
			clog.Int64("rows", rows),
			clog.Error(err),
		)
	case elapsed > 200*time.Millisecond && l.level >= logger.Warn:
		l.logger.Warn("slow sql",
			clog.String("duration", elapsed.String()),
			clog.String("sql", sql),
			clog.Int64("rows", rows),
		)
	case l.level >= logger.Info:
		l.logger.Debug("sql",
			clog.String("duration", elapsed.String()),
			clog.String("sql", sql),
			clog.Int64("rows", rows),
		)
	}
}
