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
		cfg     *Config
		wantErr bool
	}{
		{
			name:    "nil config with defaults",
			cfg:     nil,
			wantErr: false,
		},
		{
			name: "with config struct",
			cfg: &Config{
				Name:     "test",
				FileType: "yaml",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader, err := New(tt.cfg)
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
	loader, err := New(&Config{
		Name:     "test",
		FileType: "yaml",
		Paths:    []string{tmpDir},
	})
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

	loader, err := New(&Config{
		Name:     "test",
		FileType: "yaml",
		Paths:    []string{tmpDir},
	})
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

// TestConfigDefaults 测试配置默认值
func TestConfigDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.validate()

	if cfg.Name != "config" {
		t.Errorf("Config.validate() Name = %v, want config", cfg.Name)
	}

	if len(cfg.Paths) != 2 || cfg.Paths[0] != "." || cfg.Paths[1] != "./config" {
		t.Errorf("Config.validate() Paths = %v, want [., ./config]", cfg.Paths)
	}

	if cfg.FileType != "yaml" {
		t.Errorf("Config.validate() FileType = %v, want yaml", cfg.FileType)
	}

	if cfg.EnvPrefix != "GENESIS" {
		t.Errorf("Config.validate() EnvPrefix = %v, want GENESIS", cfg.EnvPrefix)
	}
}

// TestNewWithConfig 测试使用 Config 创建
func TestNewWithConfig(t *testing.T) {
	// 创建临时配置文件
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "app.json")

	configContent := `{"app": {"name": "test"}}`
	if err := os.WriteFile(configFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	loader, err := New(&Config{
		Name:      "app",
		Paths:     []string{tmpDir},
		FileType:  "json",
		EnvPrefix: "MYAPP",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if loader == nil {
		t.Fatal("New() returned nil loader")
	}

	// 验证 loader 可以正常工作
	ctx := context.Background()
	if err := loader.Load(ctx); err != nil {
		t.Errorf("Load() error = %v", err)
	}
}
