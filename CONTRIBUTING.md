# Contributing to proxyhub

感谢对 proxyhub 的兴趣！

## 开发准备

```bash
git clone https://github.com/jiusanzhou/proxyhub
cd proxyhub
go mod download
make test
make build
```

需要 Go 1.25+。

## 提交流程

1. Fork 仓库 → 在 feature 分支开发
2. 跑 `make test` 确保测试通过
3. 跑 `golangci-lint run` 看 lint 错误
4. 提交 PR，描述改动动机和测试方式

## Commit Message

遵循 [Conventional Commits](https://www.conventionalcommits.org/)：

- `feat:` 新功能
- `fix:` bug 修复
- `docs:` 文档
- `refactor:` 重构（不改外部行为）
- `test:` 测试
- `chore:` 杂项

例：`feat(session): add rotation API`

## 加新代理源

在 `internal/source/` 新建实现 `Source` 接口的文件：

```go
type Source interface {
    Name() string
    Fetch(ctx context.Context) ([]*pool.Proxy, error)
}
```

参考 `proxifly.go` 实现。

## 报问题

[GitHub Issues](https://github.com/jiusanzhou/proxyhub/issues)
- 复现步骤
- 预期 vs 实际
- proxyhub 版本（`proxyhub version`）
- Go 版本 / OS

## License

代码贡献按 [MIT License](LICENSE) 授权。
