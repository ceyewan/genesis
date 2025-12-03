# Auth 组件示例

这个示例展示了如何使用 Genesis Auth 组件构建一个简单的 JWT 认证系统。

## 功能特性

- ✅ JWT Token 生成与验证
- ✅ Token 刷新
- ✅ 集成 clog 日志
- ✅ 集成 xerrors 错误处理
- ✅ Gin 中间件支持
- ✅ 角色授权检查

## 快速开始

### 1. 运行示例

```bash
cd examples/auth
go run main.go
```

服务器将在 `http://localhost:8080` 启动。

### 2. 登录获取 Token

```bash
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user123",
    "username": "Alice"
  }'
```

响应：
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_in": 900
}
```

### 3. 使用 Token 访问受保护的路由

```bash
curl -X GET http://localhost:8080/api/profile \
  -H "Authorization: Bearer <YOUR_TOKEN>"
```

响应：
```json
{
  "user_id": "user123",
  "username": "Alice",
  "roles": ["user"]
}
```

### 4. 刷新 Token

```bash
curl -X POST http://localhost:8080/refresh \
  -H "Content-Type: application/json" \
  -d '{
    "token": "<YOUR_TOKEN>"
  }'
```

## API 端点

| 方法 | 端点 | 描述 |
|------|------|------|
| POST | `/login` | 登录，生成 Token |
| POST | `/refresh` | 刷新 Token |
| GET | `/api/profile` | 获取个人资料（需要认证） |
| GET | `/api/admin` | 管理员接口（需要 admin 角色） |

## 核心代码解析

### 初始化认证器

```go
cfg := &auth.Config{
  SecretKey:      "your-secret-key-must-be-at-least-32-characters-long-here",
  AccessTokenTTL: 15 * time.Minute,
  TokenLookup:    "header:Authorization",
  TokenHeadName:  "Bearer",
}

authenticator, err := auth.New(cfg, auth.WithLogger(logger))
```

### 生成 Token

```go
claims := auth.NewClaims("user123",
  auth.WithUsername("Alice"),
  auth.WithRoles("user"),
)

token, err := authenticator.GenerateToken(ctx, claims)
```

### 验证 Token

```go
claims, err := authenticator.ValidateToken(ctx, token)
if err != nil {
  // Token 验证失败
  return err
}
```

## 配置说明

| 字段 | 说明 | 默认值 |
|------|------|--------|
| `SecretKey` | JWT 签名密钥（至少 32 字符） | 无默认值 |
| `SigningMethod` | 签名方法 | HS256 |
| `AccessTokenTTL` | Token 有效期 | 15 分钟 |
| `TokenLookup` | Token 提取方式 | header:Authorization |
| `TokenHeadName` | Header 前缀 | Bearer |

## 生产环境建议

1. **密钥管理**：
   - 使用环境变量或密钥管理系统存储 SecretKey
   - 定期轮换密钥

2. **HTTPS**：
   - 生产环境必须使用 HTTPS
   - 防止 Token 被窃听

3. **Token 有效期**：
   - Access Token：15 分钟 - 1 小时
   - Refresh Token：7 - 30 天

4. **错误处理**：
   - 记录认证失败的详细信息
   - 防止信息泄露

## 文件结构

```
examples/auth/
├── main.go                  # 示例服务器
├── grafana-dashboard.json   # Grafana 仪表盘配置
└── README.md                # 本文件
```

## 组件集成

该示例演示了 Auth 与 Genesis 其他组件的集成：

- **clog**：结构化日志输出
- **xerrors**：统一错误处理
- **config**：配置管理

## 监控

程序启动后，可以通过以下地址访问监控数据：

- **Prometheus Metrics**: http://localhost:9091/metrics

### 内置指标

| 指标名 | 类型 | 描述 |
|--------|------|------|
| `auth_tokens_generated_total` | Counter | 生成的 Token 数 |
| `auth_tokens_validated_total` | Counter | 验证的 Token 数 |
| `auth_tokens_refreshed_total` | Counter | 刷新的 Token 数 |
| `auth_access_denied_total` | Counter | 访问拒绝数 |
| `auth_token_generation_duration_seconds` | Histogram | Token 生成耗时 |
| `auth_token_validation_duration_seconds` | Histogram | Token 验证耗时 |

**Label 维度**：`status` (success/error), `error_type` (expired/invalid/revoked), `reason` (missing_token/invalid_token)

## Prometheus + Grafana 可视化

Genesis 项目在根目录提供了统一的 Docker Compose 配置，包含 Prometheus 和 Grafana。

### 快速启动

#### 1. 启动监控服务（根目录）

```bash
# 在项目根目录
docker network create genesis-net 2>/dev/null || true
docker-compose -f docker-compose.dev.yml up prometheus grafana -d
```

Docker 容器启动后：
- **Prometheus** - http://localhost:9090
- **Grafana** - http://localhost:3000

#### 2. 启动示例应用

在另一个终端：

```bash
cd examples/auth
go run main.go
```

应用会在以下端口运行：
- **Gin 服务** - http://localhost:8080
- **Prometheus 指标** - http://localhost:9091/metrics

### Prometheus 查询

访问 http://localhost:9090，在查询框中输入以下 PromQL 表达式查看指标：

**Token 生成速率**
```promql
rate(auth_tokens_generated_total[1m])
```

**Token 验证速率（按状态分组）**
```promql
sum(rate(auth_tokens_validated_total[1m])) by (status)
```

**Token 验证耗时（P95）**
```promql
histogram_quantile(0.95, rate(auth_token_validation_duration_seconds_bucket[1m]))
```

**访问拒绝速率（按原因分组）**
```promql
sum(rate(auth_access_denied_total[1m])) by (reason)
```

### Grafana 仪表盘

示例包含了完整的 Grafana 仪表盘配置文件 `grafana-dashboard.json`，包含以下面板：

1. **Auth Operations Rate** - 认证操作速率图
2. **Auth Operations Total** - 认证操作总数统计
3. **Auth Operations Latency** - 认证操作延迟分布（P50/P95/P99）
4. **Token Validation Status Distribution** - 验证状态分布饼图
5. **Token Validation Errors Distribution** - 验证错误类型分布饼图
6. **Access Denied Rate** - 访问拒绝速率图

#### 🚀 快速方法（一键导入）

**第 1 步：登录 Grafana**
1. 访问 http://localhost:3000
2. 用户名: `admin` | 密码: `admin`

**第 2 步：添加 Prometheus 数据源**
1. 左侧菜单 → **Connections** → **Data sources**
2. 点击 **Add data source**
3. 选择 **Prometheus**
4. URL: `http://prometheus:9090`
5. 点击 **Save & test**

**第 3 步：导入预配置仪表板**
1. 左侧菜单 → **Dashboards** → 点击 **+ 导入**
2. 选择 **上传 JSON 文件**
3. 选择 `examples/auth/grafana-dashboard.json`
4. 点击 **导入**

✅ 完成！仪表板包含 6 个面板，覆盖所有 auth 相关指标。

#### 手动验证 Metrics

```bash
# 查看所有 auth 相关指标
curl -s http://localhost:9091/metrics | grep auth

# 查看特定指标
curl -s http://localhost:9091/metrics | grep auth_tokens_generated_total

# 按行排序查看
curl -s http://localhost:9091/metrics | grep "^auth_" | sort -u
```

### 配置文件说明

监控服务配置位于项目根目录：

- **docker-compose.dev.yml** - Docker Compose 配置（包含 Prometheus 和 Grafana）
- **config/prometheus.yml** - Prometheus 采集配置

#### Prometheus 配置

```yaml
scrape_configs:
  - job_name: 'genesis-app'
    static_configs:
      - targets: ['host.docker.internal:9091']  # 宿主机上的应用
```

### 停止容器

```bash
# 在项目根目录
docker-compose -f docker-compose.dev.yml down
```

移除数据卷：
```bash
docker-compose -f docker-compose.dev.yml down -v
```

### 完整工作流程

```bash
# 终端 1：启动监控服务（在项目根目录）
docker network create genesis-net 2>/dev/null || true
docker-compose -f docker-compose.dev.yml up prometheus grafana -d

# 终端 2：启动示例应用
cd examples/auth
go run main.go

# 浏览器：
# 1. http://localhost:9090 - Prometheus 原生 UI
# 2. http://localhost:3000 - Grafana 仪表板（admin/admin）

# 观看指标更新（负载测试每 100ms 发送一批请求）
```

### 故障排除

**Prometheus 无法连接到应用**

如果在 Prometheus 中看到 "DOWN" 状态，检查：
1. 应用是否正在运行（http://localhost:8080）
2. Prometheus 指标是否可访问（http://localhost:9091/metrics）
3. Docker 网络配置（使用 `host.docker.internal` 连接宿主机）

**Grafana 无法连接到 Prometheus**

1. 检查数据源配置中的 URL 是否为 `http://prometheus:9090`
2. 确保 Prometheus 容器在运行
3. 重启 Grafana 容器

**查看实时指标**

访问 http://localhost:9090 在 Graph 标签查看实时指标变化。

## 更多信息

详见 `docs/governance/auth-design.md`。
