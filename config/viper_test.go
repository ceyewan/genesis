package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
)

type spyLogger struct {
	mu       sync.Mutex
	warnMsgs []string
	base     clog.Logger
}

func newSpyLogger() *spyLogger {
	return &spyLogger{base: clog.Discard()}
}

func (l *spyLogger) warnings() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.warnMsgs...)
}

func (l *spyLogger) recordWarn(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warnMsgs = append(l.warnMsgs, msg)
}

func (l *spyLogger) Debug(msg string, fields ...clog.Field)                             {}
func (l *spyLogger) Info(msg string, fields ...clog.Field)                              {}
func (l *spyLogger) Warn(msg string, fields ...clog.Field)                              { l.recordWarn(msg) }
func (l *spyLogger) Error(msg string, fields ...clog.Field)                             {}
func (l *spyLogger) Fatal(msg string, fields ...clog.Field)                             {}
func (l *spyLogger) DebugContext(ctx context.Context, msg string, fields ...clog.Field) {}
func (l *spyLogger) InfoContext(ctx context.Context, msg string, fields ...clog.Field)  {}
func (l *spyLogger) WarnContext(ctx context.Context, msg string, fields ...clog.Field) {
	l.recordWarn(msg)
}
func (l *spyLogger) ErrorContext(ctx context.Context, msg string, fields ...clog.Field) {}
func (l *spyLogger) FatalContext(ctx context.Context, msg string, fields ...clog.Field) {}
func (l *spyLogger) With(fields ...clog.Field) clog.Logger                              { return l }
func (l *spyLogger) WithNamespace(parts ...string) clog.Logger                          { return l }
func (l *spyLogger) SetLevel(level clog.Level) error                                    { return nil }
func (l *spyLogger) Flush()                                                             {}
func (l *spyLogger) Close() error                                                       { return nil }

// TestLoaderLoad 测试配置加载的完整流程
func TestLoaderLoad(t *testing.T) {
	// 创建临时目录和配置文件
	tmpDir := t.TempDir()

	// 基础配置文件
	baseConfig := filepath.Join(tmpDir, "config.yaml")
	baseContent := `
app:
  name: "base-app"
  version: "1.0.0"
  debug: false
mysql:
  host: "localhost"
  port: 3306
redis:
  addr: "localhost:6379"
  db: 0
`

	// 开发环境配置文件
	devConfig := filepath.Join(tmpDir, "config.dev.yaml")
	devContent := `
app:
  debug: true
redis:
  db: 1
`

	// .env 文件
	envFile := filepath.Join(tmpDir, ".env")
	envContent := `
GENESIS_CLOG_LEVEL=debug
GENESIS_CLOG_FORMAT=json
`

	// 创建所有文件
	if err := os.WriteFile(baseConfig, []byte(baseContent), 0644); err != nil {
		t.Fatalf("Failed to create base config: %v", err)
	}
	if err := os.WriteFile(devConfig, []byte(devContent), 0644); err != nil {
		t.Fatalf("Failed to create dev config: %v", err)
	}
	if err := os.WriteFile(envFile, []byte(envContent), 0644); err != nil {
		t.Fatalf("Failed to create .env file: %v", err)
	}

	// 设置环境变量
	os.Setenv("GENESIS_ENV", "dev")
	os.Setenv("GENESIS_APP_NAME", "env-app")
	os.Setenv("GENESIS_MYSQL_PORT", "5432")
	defer func() {
		os.Unsetenv("GENESIS_ENV")
		os.Unsetenv("GENESIS_APP_NAME")
		os.Unsetenv("GENESIS_MYSQL_PORT")
	}()

	ctx := context.Background()
	loader, err := New(&Config{
		Name:      "config",
		Paths:     []string{tmpDir},
		FileType:  "yaml",
		EnvPrefix: "GENESIS",
	})
	if err != nil {
		t.Fatalf("Failed to create loader: %v", err)
	}

	if err := loader.Load(ctx); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// 验证配置优先级（通过公共接口）
	// 1. 环境变量（最高优先级）
	if appName := loader.Get("app.name"); appName != "env-app" {
		t.Errorf("app.name from env = %v, want env-app", appName)
	}

	if mysqlPort := loader.Get("mysql.port"); mysqlPort != "5432" {
		t.Errorf("mysql.port from env = %v, want 5432", mysqlPort)
	}

	// 2. .env 文件（高优先级）
	if logLevel := loader.Get("clog.level"); logLevel != "debug" {
		t.Errorf("clog.level from .env = %v, want debug", logLevel)
	}

	// 3. 环境特定配置（中等优先级）
	if appDebug := loader.Get("app.debug"); appDebug != true {
		t.Errorf("app.debug from dev config = %v, want true", appDebug)
	}

	if redisDb := loader.Get("redis.db"); redisDb != 1 {
		t.Errorf("redis.db from dev config = %v, want 1", redisDb)
	}

	// 4. 基础配置（最低优先级）
	if appVersion := loader.Get("app.version"); appVersion != "1.0.0" {
		t.Errorf("app.version from base config = %v, want 1.0.0", appVersion)
	}

	if mysqlHost := loader.Get("mysql.host"); mysqlHost != "localhost" {
		t.Errorf("mysql.host from base config = %v, want localhost", mysqlHost)
	}
}

// TestLoaderValidate 测试配置验证
func TestLoaderValidate(t *testing.T) {
	tests := []struct {
		name        string
		setupLoader func() (Loader, error)
		wantErr     bool
	}{
		{
			name: "valid config",
			setupLoader: func() (Loader, error) {
				tmpDir := t.TempDir()
				configFile := filepath.Join(tmpDir, "config.yaml")
				content := `app: {name: test}`
				if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
					return nil, err
				}
				return New(&Config{
					Name:  "config",
					Paths: []string{tmpDir},
				})
			},
			wantErr: false,
		},
		{
			name: "empty config",
			setupLoader: func() (Loader, error) {
				return New(&Config{
					Name:      "nonexistent",
					Paths:     []string{"/nonexistent"},
					EnvPrefix: "GENESIS_TEST_EMPTY_CONFIG",
				})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, err := tt.setupLoader()
			if err != nil {
				t.Fatalf("Failed to setup loader: %v", err)
			}

			ctx := context.Background()
			if err := loader.Load(ctx); err != nil {
				if !tt.wantErr {
					t.Errorf("Load() error = %v, want no error", err)
				}
				return
			}

			if err := loader.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestLoaderWatch 测试配置监听功能
func TestLoaderWatch(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "watch-test.yaml")
	initialContent := `
test:
  value: "initial"
  counter: 1
`

	if err := os.WriteFile(configFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	loader, err := New(&Config{
		Name:     "watch-test",
		Paths:    []string{tmpDir},
		FileType: "yaml",
	})
	if err != nil {
		t.Fatalf("Failed to create loader: %v", err)
	}

	if err := loader.Load(ctx); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// 监听 test.value
	valueCh, err := loader.Watch(ctx, "test.value")
	if err != nil {
		t.Fatalf("Failed to watch test.value: %v", err)
	}

	// 监听 test.counter
	counterCh, err := loader.Watch(ctx, "test.counter")
	if err != nil {
		t.Fatalf("Failed to watch test.counter: %v", err)
	}

	// 修改配置文件
	updatedContent := `
test:
  value: "updated"
  counter: 2
`

	if err := os.WriteFile(configFile, []byte(updatedContent), 0644); err != nil {
		t.Fatalf("Failed to update config file: %v", err)
	}

	// 验证配置变更事件
	eventCount := 0
	timeout := time.After(5 * time.Second)

	for eventCount < 2 {
		select {
		case event := <-valueCh:
			if event.Key != "test.value" {
				t.Errorf("Event key = %v, want test.value", event.Key)
			}
			if event.Value != "updated" {
				t.Errorf("Event value = %v, want updated", event.Value)
			}
			if event.OldValue != "initial" {
				t.Errorf("Event oldValue = %v, want initial", event.OldValue)
			}
			if event.Source != EventSourceFile {
				t.Errorf("Event source = %v, want %v", event.Source, EventSourceFile)
			}
			eventCount++

		case event := <-counterCh:
			if event.Key != "test.counter" {
				t.Errorf("Event key = %v, want test.counter", event.Key)
			}
			if event.Value != 2 {
				t.Errorf("Event value = %v, want 2", event.Value)
			}
			if event.OldValue != 1 {
				t.Errorf("Event oldValue = %v, want 1", event.OldValue)
			}
			eventCount++

		case <-timeout:
			t.Errorf("Timeout waiting for config change events")
			return

		case <-ctx.Done():
			t.Errorf("Context cancelled while waiting for events")
			return
		}
	}

	if eventCount != 2 {
		t.Errorf("Received %d events, want 2", eventCount)
	}
}

// TestLoaderWatchCancel 测试监听取消
func TestLoaderWatchCancel(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "cancel-test.yaml")
	content := `test: {value: 1}`

	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	ctx := context.Background()
	loader, err := New(&Config{
		Name:  "cancel-test",
		Paths: []string{tmpDir},
	})
	if err != nil {
		t.Fatalf("Failed to create loader: %v", err)
	}

	if err := loader.Load(ctx); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// 创建可取消的上下文
	watchCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	ch, err := loader.Watch(watchCtx, "test.value")
	if err != nil {
		t.Fatalf("Failed to watch: %v", err)
	}

	// 等待上下文取消
	<-watchCtx.Done()

	// 验证通道已关闭
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("Watch channel should be closed after context cancellation")
		}
	case <-time.After(100 * time.Millisecond):
		// 通道应该已经关闭，如果没有则超时
	}
}

// TestLoaderMultipleWatches 测试多个监听器
func TestLoaderMultipleWatches(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "multi-watch.yaml")
	content := `test: {value: "initial"}`

	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	ctx := context.Background()
	loader, err := New(&Config{
		Name:  "multi-watch",
		Paths: []string{tmpDir},
	})
	if err != nil {
		t.Fatalf("Failed to create loader: %v", err)
	}

	if err := loader.Load(ctx); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// 创建多个监听器
	watchCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch1, err := loader.Watch(watchCtx, "test.value")
	if err != nil {
		t.Fatalf("Failed to create watch 1: %v", err)
	}

	ch2, err := loader.Watch(watchCtx, "test.value")
	if err != nil {
		t.Fatalf("Failed to create watch 2: %v", err)
	}

	// 修改配置
	updatedContent := `test: {value: "updated"}`
	if err := os.WriteFile(configFile, []byte(updatedContent), 0644); err != nil {
		t.Fatalf("Failed to update config: %v", err)
	}

	// 两个监听器都应该收到事件
	eventCount := 0
	timeout := time.After(3 * time.Second)

	for eventCount < 2 {
		select {
		case event := <-ch1:
			if event.Value != "updated" {
				t.Errorf("ch1 event value = %v, want updated", event.Value)
			}
			eventCount++

		case event := <-ch2:
			if event.Value != "updated" {
				t.Errorf("ch2 event value = %v, want updated", event.Value)
			}
			eventCount++

		case <-timeout:
			t.Errorf("Timeout waiting for events from both channels")
			return

		case <-watchCtx.Done():
			t.Errorf("Context cancelled while waiting")
			return
		}
	}
}

// TestLoaderEnvLoading 测试环境变量加载
func TestLoaderEnvLoading(t *testing.T) {
	// 设置测试环境变量
	testVars := map[string]string{
		"TEST_APP_NAME":     "env-test-app",
		"TEST_APP_DEBUG":    "true",
		"TEST_MYSQL_HOST":   "env-host",
		"TEST_REDIS_ADDR":   "env-redis:6380",
		"TEST_NESTED_VALUE": "nested-env-value",
	}

	// 设置环境变量
	for k, v := range testVars {
		os.Setenv(k, v)
	}
	defer func() {
		for k := range testVars {
			os.Unsetenv(k)
		}
	}()

	ctx := context.Background()
	loader, err := New(&Config{
		Name:      "config",
		Paths:     []string{"./nonexistent"}, // 配置文件不存在，只使用环境变量
		EnvPrefix: "TEST",
	})
	if err != nil {
		t.Fatalf("Failed to create loader: %v", err)
	}

	if err := loader.Load(ctx); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// 验证环境变量被正确加载（通过公共接口）
	if appName := loader.Get("app.name"); appName != "env-test-app" {
		t.Errorf("app.name = %v, want env-test-app", appName)
	}

	if appDebug := loader.Get("app.debug"); appDebug != "true" {
		t.Errorf("app.debug = %v, want true", appDebug)
	}

	if mysqlHost := loader.Get("mysql.host"); mysqlHost != "env-host" {
		t.Errorf("mysql.host = %v, want env-host", mysqlHost)
	}

	if redisAddr := loader.Get("redis.addr"); redisAddr != "env-redis:6380" {
		t.Errorf("redis.addr = %v, want env-redis:6380", redisAddr)
	}
}

func TestLoaderDotEnvDoesNotOverrideProcessEnv(t *testing.T) {
	tmpDir := t.TempDir()

	configFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte("app: {name: file-app}\n"), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	envFile := filepath.Join(tmpDir, ".env")
	envContent := "TEST_APP_NAME=dotenv-app\nTEST_APP_DEBUG=true\n"
	if err := os.WriteFile(envFile, []byte(envContent), 0644); err != nil {
		t.Fatalf("Failed to create .env file: %v", err)
	}

	os.Setenv("TEST_APP_NAME", "process-app")
	defer os.Unsetenv("TEST_APP_NAME")
	defer os.Unsetenv("TEST_APP_DEBUG")

	loader, err := New(&Config{
		Name:      "config",
		Paths:     []string{tmpDir},
		EnvPrefix: "TEST",
	})
	if err != nil {
		t.Fatalf("Failed to create loader: %v", err)
	}

	if err := loader.Load(context.Background()); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if appName := loader.Get("app.name"); appName != "process-app" {
		t.Fatalf("app.name = %v, want process-app", appName)
	}
	if appDebug := loader.Get("app.debug"); appDebug != "true" {
		t.Fatalf("app.debug = %v, want true", appDebug)
	}
}

func TestLoaderWatchBeforeLoad(t *testing.T) {
	loader, err := New(&Config{})
	if err != nil {
		t.Fatalf("Failed to create loader: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err = loader.Watch(ctx, "app.debug")
	if !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("Watch() error = %v, want ErrNotLoaded", err)
	}
}

func TestLoaderLoadIsIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	if err := os.WriteFile(configFile, []byte("app: {name: first}\n"), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	loader, err := New(&Config{
		Name:  "config",
		Paths: []string{tmpDir},
	})
	if err != nil {
		t.Fatalf("Failed to create loader: %v", err)
	}

	if err := loader.Load(context.Background()); err != nil {
		t.Fatalf("First Load() error = %v", err)
	}
	if name := loader.Get("app.name"); name != "first" {
		t.Fatalf("app.name after first load = %v, want first", name)
	}

	if err := os.WriteFile(configFile, []byte("app: {name: second}\n"), 0644); err != nil {
		t.Fatalf("Failed to update config file: %v", err)
	}

	if err := loader.Load(context.Background()); err != nil {
		t.Fatalf("Second Load() error = %v", err)
	}
	if name := loader.Get("app.name"); name != "second" {
		t.Fatalf("app.name after second load = %v, want second", name)
	}
}

func TestLoaderReloadValidationFailureLogsWarning(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")

	if err := os.WriteFile(configFile, []byte("app: {name: valid}\n"), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	logger := newSpyLogger()
	loader, err := New(&Config{
		Name:      "config",
		Paths:     []string{tmpDir},
		EnvPrefix: "CONFIG_RELOAD_VALIDATION_TEST",
	}, WithLogger(logger))
	if err != nil {
		t.Fatalf("Failed to create loader: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := loader.Load(ctx); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	ch, err := loader.Watch(ctx, "app.name")
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	if err := os.WriteFile(configFile, []byte("{}\n"), 0644); err != nil {
		t.Fatalf("Failed to update config file: %v", err)
	}

	select {
	case event := <-ch:
		t.Fatalf("unexpected event after invalid reload: %+v", event)
	case <-time.After(800 * time.Millisecond):
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(logger.warnings()) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("expected reload warning log, got none")
}

func TestLoaderDotEnvReadFailureReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte("app: {name: file-app}\n"), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	envFile := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envFile, []byte("INVALID LINE\n"), 0644); err != nil {
		t.Fatalf("Failed to create .env file: %v", err)
	}

	loader, err := New(&Config{
		Name:      "config",
		Paths:     []string{tmpDir},
		EnvPrefix: "TEST",
	})
	if err != nil {
		t.Fatalf("Failed to create loader: %v", err)
	}

	if err := loader.Load(context.Background()); err == nil {
		t.Fatalf("Load() error = nil, want read .env error")
	}
}
