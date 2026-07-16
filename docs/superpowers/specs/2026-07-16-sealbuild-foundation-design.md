# Sealbuild Go 基础框架设计

## 目标

建立一个可编译、可测试、可执行的 Go CLI 基础框架，为后续 QEMU、Guest Runtime 和 BuildKit 集成提供稳定入口。本阶段只实现已有真实行为，不创建 `build`、`clean`、VM 或 Runtime 占位实现。

## 范围

- Go Module 使用 `github.com/labring/sealbuild`。
- Go 语言基线使用 `1.26.0`。
- 建立 `cmd/sealbuild` 进程入口。
- 建立 `internal/cli`，统一处理参数、输出和退出码。
- 建立 `internal/version`，承载版本、Commit 和构建时间注入。
- 实现 `sealbuild version`、`sealbuild help`、`-h`、`--help` 和无参数帮助。
- 未知命令写入标准错误并返回退出码 `2`。
- 建立 README、Git 忽略规则和基础验证命令。

## 非目标

- 不启动 QEMU。
- 不调用 BuildKit。
- 不创建 `build` 或 `clean` 的空命令。
- 不增加 Cobra、urfave/cli 等第三方 CLI 框架。
- 不创建 GitHub Release 工作流；发布矩阵在 Runtime 打包设计完成后实现。
- 不初始化 Git、不创建分支、不提交 Commit。

## 架构

`cmd/sealbuild/main.go` 只负责把 `os.Args`、标准输出、标准错误和版本信息传入 `internal/cli`，再根据返回值调用 `os.Exit`。

`internal/cli` 使用标准库进行最小命令分发。它不保存全局状态，所有输出通过 `io.Writer` 注入，保证单元测试不修改真实终端。

`internal/version` 保存可由 `-ldflags -X` 注入的包级字符串，并把它们转换为不可变的 `Info` 值。开发构建使用明确的 `dev`、`unknown` 默认值。

## CLI 行为

```text
sealbuild version
```

输出 Sealbuild 版本、Commit 和构建时间，退出码为 `0`。

```text
sealbuild
sealbuild help
sealbuild -h
sealbuild --help
```

输出帮助，退出码为 `0`。

```text
sealbuild unknown
```

向标准错误输出未知命令和帮助提示，退出码为 `2`。

## 错误与输出

- 正常结果和帮助写入标准输出。
- 参数与命令错误写入标准错误。
- `internal/cli` 不调用 `os.Exit`。
- `cmd/sealbuild` 是唯一决定进程退出码的位置。
- 版本输出不读取 Git、网络或宿主环境，保证发布产物可复现。

## 测试

- 先写 `internal/version` 格式化测试并确认缺少实现时失败。
- 先写 `internal/cli` 的 version、help、unknown command 行为测试并确认缺少实现时失败。
- 实现最少代码使测试通过。
- 最终运行 `gofmt -l ./cmd ./internal`、`go vet ./...`、`go test ./...` 和 `go build ./cmd/sealbuild`。

## 成功标准

- 基础验证命令全部退出 `0`。
- `go run ./cmd/sealbuild version` 输出版本、Commit 和构建时间。
- `go run ./cmd/sealbuild unknown` 返回退出码 `2`，错误写入标准错误。
- 项目没有第三方 Go 依赖。
