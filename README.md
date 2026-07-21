# Sealbuild

Sealbuild 是一个完全本地、免安装、自包含的镜像构建 CLI。它计划在 macOS、Linux 和 Windows 宿主上统一构建 `linux/amd64` OCI 镜像，不依赖 Docker Desktop、Docker Engine、WSL、Hyper-V 或远程构建服务。

## 当前状态

Darwin ARM 本地 OCI 构建链路已经完成真实验收。当前单文件 CLI 内嵌 QEMU Host Runtime 与 Linux AMD64 Guest Runtime，不调用 Docker Engine，不读取 Docker socket，不需要远程 builder；每次构建启动一个新的 QEMU TCG VM，完成后由 Guest 停止 BuildKit、同步并卸载持久缓存盘，再正常关机。

2026-07-20 在 Apple Silicon Mac 上连续完成两组固定 `linux/amd64` 构建。轻量验收 Context 首次 37.52 秒、缓存构建 12.66 秒；真实 `aws-account` Node + Rust + Debian 多阶段 Dockerfile 首次 28 分 16 秒、缓存构建 11.84 秒，18 个步骤命中 `CACHED`。

Windows AMD64 候选已在 GitHub-hosted Windows Server 2025 x64 Runner 完成真实验收。单文件 EXE 自带 TCG-only QEMU Host Runtime 和 Linux AMD64 Guest Runtime；独立产品 Job 在不安装 MSYS2、Docker、WSL 或 QEMU 且第三方 PATH 被清空的环境中连续完成两次标准 Dockerfile 构建，第二次命中缓存，两个 OCI 输出均为 `linux/amd64`。候选 EXE 为 82,245,120 字节，SHA-256 为 `22ea2e33f9784746d53b55d4fb0e278c25f6d6bff0ee836b922f0c01dc92324a`。Windows 10/11 Home 普通用户实机尚未验收，因此当前只能把它标记为 Windows CI 候选，不能宣称家庭版正式支持。Registry Push、Darwin Intel 和 Linux AMD64 仍未完成真实端到端验收。完整证据见 [`docs/runtime-spike-results.md`](docs/runtime-spike-results.md)。

## 支持范围

产品目标宿主平台：

- `darwin/arm64`
- `darwin/amd64`
- `linux/amd64`
- `windows/amd64`

镜像目标固定为：

- `linux/amd64`

## 构建命令

查看帮助：

```bash
sealbuild --help
```

查看版本：

```bash
sealbuild version
```

构建本地 Dockerfile Context；输出固定为 `linux/amd64` OCI Archive：

```bash
sealbuild build \
  --proxy http://127.0.0.1:7890 \
  --no-proxy deb.debian.org,.debian.org \
  --output image.oci.tar \
  .
```

`--no-proxy` 是可选参数。仅当代理无法访问部分 Dockerfile 下载域名时使用；Sealbuild 会同时传入标准的 `NO_PROXY` 与 `no_proxy` 构建参数。

## 本地开发

环境要求：

- Go 1.26 或更高的兼容版本
- Guest Runtime 构建要求 Linux；本地开发可用固定 AMD64 Docker 容器生成 Artifact，用户构建镜像时不依赖 Docker
- Darwin ARM Host Runtime 冷构建固定使用 QEMU v11.0.2、Python 3.14.6 和离线 setuptools wheel

基础验证：

```bash
gofmt -l ./cmd ./internal ./scripts
go vet ./...
go test ./...
CGO_ENABLED=0 go build -tags sealbuild_runtime ./cmd/sealbuild
```

Windows 候选验收由 [`.github/workflows/windows-amd64.yml`](.github/workflows/windows-amd64.yml) 执行：Linux Job 构建公共 Guest Runtime，Windows Runtime Job 在 MSYS2 CLANG64 中从固定 QEMU Revision 构建 TCG-only Host Runtime，独立 Windows 产品 Job 生成单文件 EXE 并连续构建两次 Dockerfile。该工作流只上传候选 Artifact，不创建不完整的单平台 GitHub Release。

已通过的 Windows 候选运行：[Windows AMD64 candidate #29813868783](https://github.com/nightwhite/sealbuild/actions/runs/29813868783)，Commit `1f1558c4548a935cc036a5f77de9758aacb42a25`。

## 项目结构

```text
cmd/sealbuild/       CLI 进程入口
internal/cli/        命令分发、输出和退出码
internal/build/      BuildKit Client、Solve 与 OCI Archive 校验
internal/runtime/    Runtime 安装、解包和完整性校验
internal/vm/         QEMU 参数、独立 VM 与正常关机生命周期
internal/version/    版本、Commit 和构建时间
runtime/buildroot/   Guest Runtime 的 Buildroot External Tree
runtime/testdata/    本地真实构建与关机验收 Fixture
scripts/runtime/     Host/Guest Runtime 构建与打包脚本
scripts/dev/         开发期 Runtime、OCI 验证与资产嵌入工具
reference/           固定版本的上游参考仓库
docs/superpowers/    已批准的设计规格和实现计划
```

参考项目的版本和用途见 [`reference/index.md`](reference/index.md)。工程约束见 [`AGENTS.md`](AGENTS.md)。
