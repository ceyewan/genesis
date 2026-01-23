# xerrors 示例

演示 Genesis `xerrors` 包的用法。

## 运行

```bash
cd examples/xerrors
go run main.go
```

## 内容

1. **Wrap/Wrapf** - 错误包装，保留错误链
2. **Sentinel Errors** - 自定义哨兵错误 + `errors.Is()` 检查
3. **WithCode/GetCode** - 机器可读的错误码
4. **Collector** - 收集第一个错误（适合表单验证）
5. **Combine** - 合并多个错误
6. **Must** - 初始化时使用，运行时勿用
