package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNew 测试创建配置加载器
func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		opts    []Option
		wantErr bool
	}{
		{
			name:    "default options",
			opts:    []Option{},
			wantErr: false,
		},
		{
			name: "with config name",
			opts: []Option{
				WithConfigName("test"),
			},
			wantErr: false,
		},
		{
			name: "with config path",
			opts: []Option{
				WithConfigPath("./test-config"),
			},
			wantErr: false,
		},
		{
			name: "with config paths",
			opts: []Option{
				WithConfigPaths("./config", "./test"),
			},
			wantErr: false,
		},
		{
			name: "with config type",
			opts: []Option{
				WithConfigType("json"),
			},
			wantErr: false,
		},
		{
			name: "with env prefix",
			opts: []Option{
				WithEnvPrefix("TEST"),
			},
			wantErr: false,
		},
		{
			name: "with remote options",
			opts: []Option{
				WithRemote("etcd", "localhost:2379"),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, err := New(tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && loader == nil {
				t.Error("New() returned nil loader")
			}
		})
	}
}

// TestMustLoad 测试 MustLoad 函数
func TestMustLoad(t *testing.T) {
	// 创建临时配置文件
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	configContent := `
app:
  name: "test-app"
`
	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	// 正常情况应该不 panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MustLoad() panicked unexpectedly: %v", r)
		}
	}()

	loader := MustLoad(
		WithConfigName("test"),
		WithConfigPaths(tmpDir),
		WithConfigType("yaml"),
	)

	if loader == nil {
		t.Error("MustLoad() returned nil loader")
	}
}

// TestMustLoadPanic 测试 MustLoad 在错误时 panic
func TestMustLoadPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustLoad() should have panicked")
		}
	}()

	// 使用不存在的配置路径，应该导致错误并 panic
	MustLoad(
		WithConfigName("nonexistent"),
		WithConfigPaths("/nonexistent/path"),
	)
}

// TestLoaderInterface 测试 Loader 接口的完整实现
func TestLoaderInterface(t *testing.T) {
	// 创建临时配置文件
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	configContent := `
app:
  name: "test-app"
  version: "1.0.0"
  debug: true
mysql:
  host: "localhost"
  port: 3306
  username: "root"
  database: "testdb"
redis:
  addr: "localhost:6379"
  db: 0
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	ctx := context.Background()
	loader, err := New(
		WithConfigName("test"),
		WithConfigPaths(tmpDir),
		WithConfigType("yaml"),
	)
	if err != nil {
		t.Fatalf("Failed to create loader: %v", err)
	}

	// 测试 Load
	if err := loader.Load(ctx); err != nil {
		t.Errorf("Load() error = %v", err)
		return
	}

	// 测试 Get
	if appName := loader.Get("app.name"); appName != "test-app" {
		t.Errorf("Get(app.name) = %v, want test-app", appName)
	}

	if mysqlPort := loader.Get("mysql.port"); mysqlPort != 3306 {
		t.Errorf("Get(mysql.port) = %v, want 3306", mysqlPort)
	}

	// 测试 Unmarshal
	type AppConfig struct {
		App struct {
			Name    string `mapstructure:"name"`
			Version string `mapstructure:"version"`
			Debug   bool   `mapstructure:"debug"`
		} `mapstructure:"app"`
		MySQL struct {
			Host     string `mapstructure:"host"`
			Port     int    `mapstructure:"port"`
			Username string `mapstructure:"username"`
			Database string `mapstructure:"database"`
		} `mapstructure:"mysql"`
		Redis struct {
			Addr string `mapstructure:"addr"`
			DB   int    `mapstructure:"db"`
		} `mapstructure:"redis"`
	}

	var cfg AppConfig
	if err := loader.Unmarshal(&cfg); err != nil {
		t.Errorf("Unmarshal() error = %v", err)
		return
	}

	if cfg.App.Name != "test-app" {
		t.Errorf("Unmarshal() app.name = %v, want test-app", cfg.App.Name)
	}

	if cfg.MySQL.Port != 3306 {
		t.Errorf("Unmarshal() mysql.port = %v, want 3306", cfg.MySQL.Port)
	}

	// 测试 UnmarshalKey
	type MySQLConfig struct {
		Host     string `mapstructure:"host"`
		Port     int    `mapstructure:"port"`
		Username string `mapstructure:"username"`
		Database string `mapstructure:"database"`
	}

	var mysqlCfg MySQLConfig
	if err := loader.UnmarshalKey("mysql", &mysqlCfg); err != nil {
		t.Errorf("UnmarshalKey() error = %v", err)
		return
	}

	if mysqlCfg.Host != "localhost" {
		t.Errorf("UnmarshalKey() mysql.host = %v, want localhost", mysqlCfg.Host)
	}

	// 测试 Validate
	if err := loader.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

// TestWatch 测试配置监听功能
func TestWatch(t *testing.T) {
	// 创建临时配置文件
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test.yaml")

	configContent := `
app:
  name: "test-app"
  debug: true
`

	err := os.WriteFile(configFile, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loader, err := New(
		WithConfigName("test"),
		WithConfigPaths(tmpDir),
		WithConfigType("yaml"),
	)
	if err != nil {
		t.Fatalf("Failed to create loader: %v", err)
	}

	if err := loader.Load(ctx); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// 监听 app.debug 配置项
	ch, err := loader.Watch(ctx, "app.debug")
	if err != nil {
		t.Errorf("Watch() error = %v", err)
		return
	}

	// 修改配置文件
	newConfigContent := `
app:
  name: "test-app"
  debug: false
`

	err = os.WriteFile(configFile, []byte(newConfigContent), 0644)
	if err != nil {
		t.Fatalf("Failed to update config file: %v", err)
	}

	// 等待配置变更事件
	select {
	case event := <-ch:
		if event.Key != "app.debug" {
			t.Errorf("Watch() event.key = %v, want app.debug", event.Key)
		}
		if event.Value != false {
			t.Errorf("Watch() event.value = %v, want false", event.Value)
		}
		if event.OldValue != true {
			t.Errorf("Watch() event.oldValue = %v, want true", event.OldValue)
		}
		if event.Source != "file" {
			t.Errorf("Watch() event.source = %v, want file", event.Source)
		}
	case <-time.After(5 * time.Second):
		t.Error("Watch() timeout waiting for config change event")
	case <-ctx.Done():
		t.Error("Watch() context cancelled")
	}
}

// TestDefaultOptions 测试默认选项
func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Name != "config" {
		t.Errorf("defaultOptions().Name = %v, want config", opts.Name)
	}

	if len(opts.Paths) != 2 || opts.Paths[0] != "." || opts.Paths[1] != "./config" {
		t.Errorf("defaultOptions().Paths = %v, want [., ./config]", opts.Paths)
	}

	if opts.FileType != "yaml" {
		t.Errorf("defaultOptions().FileType = %v, want yaml", opts.FileType)
	}

	if opts.EnvPrefix != "GENESIS" {
		t.Errorf("defaultOptions().EnvPrefix = %v, want GENESIS", opts.EnvPrefix)
	}
}

// TestOptionsApply 测试选项应用
func TestOptionsApply(t *testing.T) {
	opts := defaultOptions()

	// 应用各种选项
	WithConfigName("test")(opts)
	WithConfigPath("./test")(opts)
	WithConfigType("json")(opts)
	WithEnvPrefix("TEST")(opts)
	WithRemote("etcd", "localhost:2379")(opts)

	if opts.Name != "test" {
		t.Errorf("After WithConfigName, Name = %v, want test", opts.Name)
	}

	if len(opts.Paths) != 3 || opts.Paths[2] != "./test" {
		t.Errorf("After WithConfigPath, Paths = %v, want [., ./config, ./test]", opts.Paths)
	}

	if opts.FileType != "json" {
		t.Errorf("After WithConfigType, FileType = %v, want json", opts.FileType)
	}

	if opts.EnvPrefix != "TEST" {
		t.Errorf("After WithEnvPrefix, EnvPrefix = %v, want TEST", opts.EnvPrefix)
	}

	if opts.RemoteOpts == nil || opts.RemoteOpts.Provider != "etcd" || opts.RemoteOpts.Endpoint != "localhost:2379" {
		t.Errorf("After WithRemote, RemoteOpts = %+v, want etcd/localhost:2379", opts.RemoteOpts)
	}
}

// TestWithConfigPaths 测试 WithConfigPaths 覆盖默认路径
func TestWithConfigPaths(t *testing.T) {
	opts := defaultOptions()

	// 覆盖默认路径
	WithConfigPaths("/etc/app", "./local")(opts)

	if len(opts.Paths) != 2 || opts.Paths[0] != "/etc/app" || opts.Paths[1] != "./local" {
		t.Errorf("WithConfigPaths() Paths = %v, want [/etc/app, ./local]", opts.Paths)
	}
}
