# API 检查输入

这些文件是兼容性检查的数据，不是第二套接口手册。日常查询使用 `go doc -all ./<包>`。

| 文件 | 作用 | 更新规则 |
| :--- | :--- | :--- |
| [inventory.md](./inventory.md) | 当前源码的导出声明快照 | 由 `make api-inventory` 生成，不手改声明 |
| [baselines/v1.0.0-rc.1.md](./baselines/v1.0.0-rc.1.md) | 固定 RC1 提交的声明基线 | 不随当前源码更新 |
| [compat-allowlist.md](./compat-allowlist.md) | 已审阅变更的原声明 | 必须准确对应基线，过时条目会失败 |
| [compat-expected.md](./compat-expected.md) | 对应变更的批准替换声明 | 必须准确对应当前源码，防止再次漂移 |
| [compat-removals.md](./compat-removals.md) | 已批准且没有替换的删除声明 | 不得与替换条目重叠或伪装替换 |

```bash
make api-inventory-check
make api-baseline-check
make api-compat-check
```

三项检查分别核对当前快照、从固定提交重建基线、检测相对基线的删除与签名变化。
移动文件或整理文档不改变批准范围。不能通过重建历史基线或随意扩大例外来使检查通过。

声明比较不等于语义兼容：TTL、序列化、消息投递和资源所有权变化仍需回归测试与
[迁移说明](../migration.md)。第三方类型出现在公共 API 时，上游不兼容升级也需要审阅。

当前预稳定版本的例外需单独批准；稳定版本遵守 SemVer，不以是否有已知下游调用为
判断标准。发布操作见[发布流程](../release.md)。
