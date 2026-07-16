# Sealbuild Go 基础框架实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法跟踪进度。

**目标：** 建立可测试、可编译、无第三方依赖的 Sealbuild Go CLI 基础框架。

**架构：** `cmd/sealbuild` 只负责进程入口，`internal/cli` 负责命令分发和退出码，`internal/version` 负责可注入版本元数据。所有 CLI 输出通过 `io.Writer` 注入，行为使用单元测试定义。

**技术栈：** Go 1.26、Go 标准库、`go test`、`go vet`。

---

### 任务 1：初始化 Go Module

**文件：**
- 创建：`go.mod`
- 创建：`.gitignore`

- [x] 创建 `github.com/labring/sealbuild` Module，Go 基线设为 `1.26.0`。
- [x] 忽略构建产物、覆盖率文件、生成 Runtime 和 Reference clone，保留 `reference/index.md`。

### 任务 2：版本信息 TDD

**文件：**
- 创建：`internal/version/version_test.go`
- 创建：`internal/version/version.go`

- [x] 编写测试，要求版本文本包含产品名、版本、Commit 和构建时间。
- [x] 运行 `go test ./internal/version`，确认因缺少实现失败。
- [x] 实现 `Info`、`Current` 和格式化输出所需的最少代码。
- [x] 再次运行 `go test ./internal/version`，确认通过。

### 任务 3：CLI 行为 TDD

**文件：**
- 创建：`internal/cli/app_test.go`
- 创建：`internal/cli/app.go`
- 创建：`cmd/sealbuild/main.go`

- [x] 编写 version、help 和 unknown command 行为测试。
- [x] 运行 `go test ./internal/cli`，确认因缺少实现失败。
- [x] 实现最小命令分发、帮助文本、标准输出/错误输出和退出码。
- [x] 实现只负责调用 `cli.Run` 和 `os.Exit` 的进程入口。
- [x] 再次运行 `go test ./internal/cli`，确认通过。

### 任务 4：项目文档

**文件：**
- 创建：`README.md`

- [x] 说明 Sealbuild 的目标、当前基础框架状态、支持平台和唯一镜像目标。
- [x] 记录基础开发与验证命令，不描述尚未实现的 QEMU 或 BuildKit 使用方式。

### 任务 5：完成验证

**文件：**
- 修改：仅修复验证发现的本阶段问题。

- [x] 运行 `gofmt -l ./cmd ./internal`，预期无输出。
- [x] 运行 `go vet ./...`，预期退出 `0`。
- [x] 运行 `go test ./...`，预期全部通过。
- [x] 运行 `go build ./cmd/sealbuild`，预期退出 `0`。
- [x] 运行 version、help 和 unknown command 冒烟验证，核对输出与退出码。
