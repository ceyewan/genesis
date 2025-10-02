# Config 组件实施手册

## 功能边界
- 提供统一配置加载：文件、环境变量、远程配置中心。
- 支持热更新回调，保证依赖组件感知变更。
- 通过 `fx` 注入，业务层仅依赖接口。

## 代码位置
- 接口：`pkg/config/provider.go`
- 默认实现：`internal/config/viper`
- 配置描述：`configs/config.yaml`

## 开发步骤
1. **接口定义**：
   - `UnmarshalKey(key string, out interface{}) error`
   - `Watch(ctx, key, onChange)` 或统一的 `WatchPrefix`。
2. **本地配置**：
   - 支持 JSON/YAML；可通过环境变量覆盖。
   - 提供默认配置样例（`configs/config.yaml`）。
3. **Viper 实现**：
   - 支持多路径加载（`configs/`, `.`）。
   - 统一 key 命名：`service.section.field`。
4. **远程配置**：
   - 结合 `coord` 实现 etcd Watch。
   - 变更后触发回调，提供 debounce 机制。
5. **DI Module**：
   - `fx` Module 中加载配置，暴露为单例。
   - 在 `OnStop` 时清理 Watch 资源。

## 测试要点
- 单测：基本读取、缺失键、类型转换、环境变量覆盖。
- 集成：模拟 etcd 配置变更，校验 Watch 回调触发。
- 观测：记录加载失败、Watch 重连等日志。

## 验收清单
- 未找到配置时返回明确错误，附带 key 信息。
- Watch 回调串行执行，避免竞态。
- 在 `usage_guide.md` 写入初始化示例。

## 后续演进
- 支持 `consul`、`apollo` 等配置中心。
- 提供配置校验插件，启动前进行 schema 校验。
