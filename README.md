# Sealbuild

Sealbuild 是一个完全本地、免安装、自包含的镜像构建 CLI。它计划在 macOS、Linux 和 Windows 宿主上统一构建 `linux/amd64` OCI 镜像，不依赖 Docker Desktop、Docker Engine、WSL、Hyper-V 或远程构建服务。

## 当前状态

项目当前完成 Go 基础框架，并已进入 Guest Runtime 可行性验证。仓库内已有固定版本的 Runtime Lock、Buildroot External Tree、Guest init、mTLS、QEMU TCG Smoke Test 和 Linux GitHub Actions 定义。Linux Runner 已完成首次端到端 Runtime Spike，证明纯 TCG Guest 可以构建并导出 `linux/amd64` OCI 镜像。

Mac ARM 端到端 Smoke Test 尚未执行，正式 `sealbuild build` 命令也尚未实现，因此当前版本不得视为已经支持四种宿主。当前实测状态见 [`docs/runtime-spike-results.md`](docs/runtime-spike-results.md)。

## 支持范围

宿主平台：

- `darwin/arm64`
- `darwin/amd64`
- `linux/amd64`
- `windows/amd64`

镜像目标固定为：

- `linux/amd64`

## 基础命令

查看帮助：

```bash
go run ./cmd/sealbuild --help
```

查看版本：

```bash
go run ./cmd/sealbuild version
```

## 本地开发

环境要求：

- Go 1.26 或更高的兼容版本
- Guest Runtime 构建要求 Linux；macOS 只进行 Go 和配置层验证

基础验证：

```bash
gofmt -l ./cmd ./internal ./scripts/runtime
go vet ./...
go test ./...
go build ./cmd/sealbuild
```

## 项目结构

```text
cmd/sealbuild/       CLI 进程入口
internal/cli/        命令分发、输出和退出码
internal/runtime/    Runtime Lock 解析与约束校验
internal/version/    版本、Commit 和构建时间
runtime/buildroot/   Guest Runtime 的 Buildroot External Tree
runtime/smoke/       联网、多阶段构建 Smoke Fixture
scripts/runtime/     Guest 构建、打包和 QEMU TCG 验收脚本
reference/           固定版本的上游参考仓库
docs/superpowers/    已批准的设计规格和实现计划
```

参考项目的版本和用途见 [`reference/index.md`](reference/index.md)。工程约束见 [`AGENTS.md`](AGENTS.md)。
