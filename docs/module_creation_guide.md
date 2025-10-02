# 新组件创建指南

## 适用场景
当需要为 Genesis 增加新的基础设施能力（如新的存储、治理手段、外部服务适配器）时，遵循本指南确保质量一致。

## Checklist
1. **需求确认**
   - 描述目标、边界、依赖与非目标。
   - 确认是否复用现有抽象或需新增接口。
2. **接口设计**
   - 在 `pkg/<component>` 中定义接口、配置、错误常量。
   - 编写 `doc.go` 说明使用方式。
3. **默认实现**
   - 放在 `internal/<component>/<impl>`。
   - 构造函数采用 `New(ctx context.Context, cfg *Config, opts ...Option)` 模式。
   - 提供 `fx` Module，处理依赖注入。
4. **测试**
   - 单测覆盖核心逻辑与错误分支。
   - 如依赖外部服务，准备 docker-compose 集成测试脚本。
5. **文档**
   - 在 `docs/<component>.md` 记录边界、实现步骤与测试要点。
   - 在 `usage_guide.md` 添加最小示例。
6. **验证**
   - 执行 `make lint test`（后续补充命令）确保通过。
   - 更新 `CHANGELOG`（规划阶段可暂记在 README 中）。

## 编码风格
- 文件命名使用蛇形，接口/结构体使用驼峰。
- 遵循 Go 官方 `golangci-lint` 默认配置。
- 错误使用 `errors.Join` 或 `fmt.Errorf("...: %w", err)` 包装。

## 提交产物
- 代码实现 + 测试 + 文档。
- 示例或脚手架代码放在 `examples/`（如需）。
- 在 PR 中附带组件自检结果与测试截图或日志。
