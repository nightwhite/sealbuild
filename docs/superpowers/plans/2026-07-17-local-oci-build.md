# Sealbuild 本地 OCI 构建实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。所有生产代码严格遵循 `karpathy-guidelines` 和 TDD。

**目标：** 在当前 Apple Silicon Mac 上用本机 Docker 仅生成新版 Guest Runtime，并让自包含 `sealbuild build` 不依赖 Docker Engine、固定构建 `linux/amd64` Dockerfile Context，原子输出通过严格校验的 OCI Archive。

**架构：** 开发脚本在固定 Debian AMD64 容器和每次独立的大小写敏感 Docker volume 中构建 qcow2/fw_cfg Guest，成功后只导出最终 Artifact；产品 CLI 通过 BuildKit v0.31.1 官方 Go Client连接每次新启动的 QEMU Guest。`internal/build` 负责请求、匿名 Registry Token session、mTLS Probe、Solve、进度和输出，`internal/runtimeassets` 负责构建标签控制的内嵌资产，`internal/cli` 只解析参数并调用窄 Builder 接口。

**技术栈：** Go 1.26.1、BuildKit v0.31.1 Go Client、OCI Image Spec v1.1.1、fsutil、QEMU v11.0.2、Buildroot 2026.05.1、Docker `linux/amd64` 开发容器。新增 Go 依赖固定到 BuildKit 上游 `go.mod` 使用的版本；不新增其他框架。

**明确禁止：** Registry 推送、Docker socket 产品依赖、旧 raw-ext4 Guest、远程 builder、自动重试、自动切换端口或 accelerator、环境代理读取、Docker config 读取、fallback。

**版本控制约束：** 项目所有者没有要求提交。本计划不创建 commit；每个任务使用局部测试、全量测试和 `git diff --check` 验证。

---

## 文件结构

```text
scripts/dev/runtime-builder.Dockerfile
    固定 AMD64 Debian 基础镜像和 Linux Guest 构建依赖。

scripts/dev/build-guest-docker.sh
    显式 Docker/Proxy 参数、固定源码 checkout、QEMU qemu-img 和 Guest 构建。

internal/build/request.go
    Context、Dockerfile、build args、Proxy 和固定 linux/amd64 frontend attrs。

internal/build/probe.go
    BuildKit mTLS Client、唯一 AMD64 worker 语义和 vm.Probe 实现。

internal/build/auth.go
    不读 Docker config 的匿名 Registry Token session 和显式 Host Proxy HTTP client。

internal/build/output.go
    OCI Archive 临时文件、BuildKit FileOutputFunc、Sync、严格校验和原子发布。

internal/build/oci.go
    结构化校验 index、Manifest、Config、descriptor digest/size 和 linux/amd64。

internal/build/solve.go
    BuildKit SolveOpt、本地 fsutil mounts、plain progress 和 Solve 生命周期。

internal/build/runner.go
    Runtime 安装、代理材料、串口日志、独立 VM、Solve 和清理编排。

internal/runtimeassets/bundle_stub.go
    未带 Runtime 构建标签时返回明确错误，不下载或回退。

internal/runtimeassets/bundle_embedded.go
    `sealbuild_runtime` 标签下嵌入 Host/Guest tar.zst 并生成 runtime.Bundle。

scripts/dev/prepare-runtime-assets.sh
    严格校验并复制本地 Host/Guest Artifact 到被 Git 忽略的 embed 输入目录。

scripts/dev/verify-runtime/main.go
    使用产品 ExtractAsset 逻辑只读校验 Host/Guest Runtime Artifact。

scripts/dev/verify-oci/main.go
    使用产品 VerifyOCIArchive 逻辑只读校验真实 OCI 输出。

internal/cli/app.go
    `build` 参数、退出码、纯文本错误和 Builder 接口。

cmd/sealbuild/main.go
    signal Context、Default Cache、内嵌 Bundle 和真实 Runner 装配。

runtime/testdata/local-build/Dockerfile
runtime/testdata/local-build/message.txt
    FROM、COPY、联网 RUN 和多阶段真实验收 Context。
```

---

### 任务 1：Docker 本地生成新版 Guest Runtime

**文件：**

- 创建：`scripts/dev/runtime-builder.Dockerfile`
- 创建：`scripts/dev/build-guest-docker.sh`
- 创建：`scripts/dev/build-guest-docker_test.go`
- 修改：`.gitignore`

- [ ] **步骤 1：编写开发脚本红灯测试**

测试读取 Dockerfile 和 Shell 脚本，固定要求：

```text
FROM --platform=linux/amd64 golang:1.26.1-bookworm@sha256:09fb8a652cf7a990b714c46a9f0f5fd2d5bc2222d995166b91907c1c05b7d0e8
Buildroot commit cb857ba4c87a93e5265a9e4a3f32071abf39e14a
QEMU release https://download.qemu.org/qemu-11.0.2.tar.xz
QEMU SHA-256 3745f6ea88e2e87fe0dc838b2b1d4e0a770bf48e01a1d5a186842a1fff76ccf5
docker --context default buildx build --builder default --platform linux/amd64 --load
docker --context default run --platform linux/amd64
显式可选 PROXY_URL 参数转换为 host.docker.internal
不读取 HTTP_PROXY/HTTPS_PROXY
不挂载 /var/run/docker.sock
输出固定为 out/guest-runtime-docker
```

- [ ] **步骤 2：确认红灯**

```bash
go test ./scripts/dev -run TestBuildGuestDocker -count=1
```

预期：FAIL，开发脚本不存在。

- [ ] **步骤 3：实现最小 Dockerfile 和脚本**

Dockerfile 安装 workflow 已证明需要的 Debian 包；脚本只接受：

```text
build-guest-docker.sh OUTPUT_DIR [PROXY_URL]
```

脚本验证输出目录不存在，通过 `--build-arg` 和 `--env` 显式传递代理，在容器内 checkout 固定 Commit、构建 `qemu-img`，然后调用现有 `build-guest.sh`。Linux 构建目录必须位于每次独立的 Docker volume，不能直接绑定 macOS 大小写不敏感目录；成功后只把 Guest archive 与 `artifact/` 导出到宿主。脚本不自动选择镜像、代理或架构。

- [ ] **步骤 4：验证脚本**

```bash
gofmt -w scripts/dev/build-guest-docker_test.go
sh -n scripts/dev/build-guest-docker.sh
go test ./scripts/dev -count=1
git diff --check
```

- [ ] **步骤 5：启动真实 Guest 构建**

```bash
./scripts/dev/build-guest-docker.sh \
  ./out/guest-runtime-docker \
  http://127.0.0.1:7890
```

该命令预计运行 40 至 90 分钟。运行期间继续任务 2 至任务 7；最终必须等待该进程退出 0，并验证：

```text
out/guest-runtime-docker/sealbuild-guest-runtime.tar.zst
out/guest-runtime-docker/artifact/bzImage
out/guest-runtime-docker/artifact/rootfs.ext4
out/guest-runtime-docker/artifact/buildkit-state.qcow2
out/guest-runtime-docker/artifact/manifest.json
out/guest-runtime-docker/artifact/checksums.txt
```

---

### 任务 2：BuildKit mTLS Client 与 Worker Probe

**文件：**

- 修改：`go.mod`
- 修改：`go.sum`
- 创建：`internal/build/probe.go`
- 创建：`internal/build/probe_test.go`

- [ ] **步骤 1：固定官方依赖**

```bash
go get github.com/moby/buildkit@v0.31.1
go get github.com/tonistiigi/fsutil@v0.0.0-20260609091201-0257b3308df4
go get github.com/opencontainers/image-spec@v1.1.1
```

不得使用 `latest`，不得通过 `replace` 指向 `reference/buildkit`。

- [ ] **步骤 2：编写 Worker 语义红灯测试**

固定 API：

```go
type Probe struct {
	Open ClientFactory
}

func (probe Probe) Ready(ctx context.Context, address string, tls tlsmaterial.Paths) error
func ValidateWorkers(workers []*client.WorkerInfo) error
```

测试覆盖：唯一 worker 且包含基线 `linux/amd64` 成功；零 worker、多个 worker、只有 `amd64/v2`、ARM、Windows 或混合 OS/Architecture 失败。Probe 必须把以下选项传给 `client.New`：

```go
client.WithServerConfig("sealbuild-runtime", tls.CA)
client.WithCredentials(tls.ClientCert, tls.ClientKey)
```

- [ ] **步骤 3：确认红灯**

```bash
go test ./internal/build -run 'TestProbe|TestValidateWorkers' -count=1
```

- [ ] **步骤 4：实现并验证绿灯**

ClientFactory 和最小 Client 接口定义在 `internal/build` 使用方；Probe 每次调用只创建一个 Client、ListWorkers 一次并 Close，关闭错误与主错误合并。

```bash
gofmt -w internal/build/probe.go internal/build/probe_test.go
go test ./internal/build -run 'TestProbe|TestValidateWorkers' -count=1
go test -race ./internal/build -run 'TestProbe|TestValidateWorkers' -count=1
```

---

### 任务 3：显式 Host Proxy 匿名 Registry Token Session

**文件：**

- 创建：`internal/build/auth.go`
- 创建：`internal/build/auth_test.go`

- [ ] **步骤 1：编写匿名认证红灯测试**

固定 API：

```go
func NewAnonymousAuth(proxyURL string) (session.Attachable, error)
```

测试使用 `httptest` Token Server 和显式 Proxy Server，覆盖：

```text
空 Proxy 时直连测试 Token Server
显式 Proxy 时请求只经过指定 Proxy
设置 HTTP_PROXY/HTTPS_PROXY 环境变量不会被读取
Credentials 永远返回空用户名和 Secret
FetchToken 只接受 http/https Realm
FetchToken 返回 Token、ExpiresIn 和 IssuedAt
Proxy URL userinfo/query/fragment 继续由 internal/proxy 拒绝
```

- [ ] **步骤 2：确认红灯**

```bash
go test ./internal/build -run TestAnonymousAuth -count=1
```

- [ ] **步骤 3：实现最小 Auth Server**

实现 `auth.AuthServer` 与 `session.Attachable`，使用 containerd `auth.FetchToken` 和私有 `http.Client`。Transport 从 `http.DefaultTransport.Clone()` 创建，但强制：

```go
transport.Proxy = nil // direct
transport.Proxy = http.ProxyURL(explicitURL) // explicit only
```

不使用 BuildKit `authprovider.NewDockerAuthProvider`，因为它读取 Docker config 并使用环境代理。

- [ ] **步骤 4：验证绿灯**

```bash
gofmt -w internal/build/auth.go internal/build/auth_test.go
go test ./internal/build -run TestAnonymousAuth -count=1
go test -race ./internal/build -run TestAnonymousAuth -count=1
```

---

### 任务 4：构建请求与固定 Dockerfile Frontend

**文件：**

- 创建：`internal/build/request.go`
- 创建：`internal/build/request_test.go`

- [ ] **步骤 1：编写请求红灯测试**

固定模型：

```go
type Request struct {
	ContextDir string
	Dockerfile string
	OutputPath string
	BuildArgs  map[string]string
	Proxy      *proxy.Config
}

type PreparedRequest struct {
	ContextDir    string
	DockerfileDir string
	OutputPath    string
	FrontendAttrs map[string]string
	HostProxy     string
}

func Prepare(request Request) (PreparedRequest, error)
```

测试覆盖 Context 必须是绝对或可转绝对的真实目录；Dockerfile 默认值、显式文件必须位于 Context 内且为普通文件；输出父目录存在且输出文件不存在；build arg 名非空、确定性排序/复制；固定 attrs：

```text
filename=<relative Dockerfile filename>
platform=linux/amd64
build-arg:<name>=<value>
```

Proxy 存在时加入四个 Guest 地址 proxy build args，HostProxy 保留原始地址；用户显式同名 proxy build arg 立即失败，不覆盖。

- [ ] **步骤 2：确认红灯**

```bash
go test ./internal/build -run TestPrepare -count=1
```

- [ ] **步骤 3：实现并验证绿灯**

使用 `filepath.Rel` 和结构化路径检查，不使用字符串前缀判断；返回 map 必须复制，调用方修改不得改变 Request。

```bash
gofmt -w internal/build/request.go internal/build/request_test.go
go test ./internal/build -run TestPrepare -count=1
```

---

### 任务 5：OCI Archive 严格校验与原子输出

**文件：**

- 创建：`internal/build/oci.go`
- 创建：`internal/build/oci_test.go`
- 创建：`internal/build/output.go`
- 创建：`internal/build/output_test.go`
- 修改：`scripts/runtime/inspect.go`
- 修改：`scripts/runtime/inspect-oci-platform_test.go`

- [ ] **步骤 1：编写 OCI 严格校验红灯测试**

固定 API：

```go
func VerifyOCIArchive(path string) error
```

使用 OCI Image Spec 结构体和真实 tar fixture，测试：

```text
唯一 linux/amd64 index + manifest + config 成功
缺 index/manifest/config
重复 tar entry
非普通 tar entry
descriptor digest 或 size 不匹配
多 manifest
ARM/Windows/variant 平台
Config os/architecture 与 index 不一致
JSON 超过 1 MiB
```

校验分三次流式扫描 tar，只读取 index、目标 Manifest 和 Config，不把 Layer 载入内存。

- [ ] **步骤 2：确认红灯并实现 VerifyOCIArchive**

```bash
go test ./internal/build -run TestVerifyOCIArchive -count=1
```

将 Spike `inspectOCIArchive` 改为调用产品 API，删除重复 JSON 模型，但保留 Spike CLI 输出。

- [ ] **步骤 3：编写原子输出红灯测试**

固定 API：

```go
type ArchiveOutput struct { /* temp file ownership */ }

func NewArchiveOutput(finalPath string) (*ArchiveOutput, error)
func (output *ArchiveOutput) Writer(map[string]string) (io.WriteCloser, error)
func (output *ArchiveOutput) Publish() error
func (output *ArchiveOutput) Abort() error
```

测试 mode 0600、目标已存在拒绝、Writer 仅获取一次、Close Sync、无效 OCI 不发布、成功硬链接或 rename 原子发布、Abort 幂等、清理错误不覆盖主错误。

- [ ] **步骤 4：实现并验证绿灯**

```bash
gofmt -w internal/build/oci.go internal/build/oci_test.go internal/build/output.go internal/build/output_test.go scripts/runtime/inspect.go scripts/runtime/inspect-oci-platform_test.go
go test ./internal/build ./scripts/runtime -run 'TestVerifyOCIArchive|TestArchiveOutput|TestInspectOCIArchive' -count=1
go test -race ./internal/build -run 'TestVerifyOCIArchive|TestArchiveOutput' -count=1
```

---

### 任务 6：BuildKit Solve 与纯文本进度

**文件：**

- 创建：`internal/build/solve.go`
- 创建：`internal/build/solve_test.go`

- [ ] **步骤 1：编写 SolveOpt 红灯测试**

固定 API：

```go
type Solver struct {
	Open ClientFactory
}

func (solver Solver) Solve(ctx context.Context, address string, tls tlsmaterial.Paths, request PreparedRequest, progress io.Writer) error
```

Fake Client 捕获并断言：

```text
Frontend == dockerfile.v0
LocalMounts 只有 context 和 dockerfile
Exports 只有 client.ExporterOCI
Output == ArchiveOutput.Writer
Session 只有 AnonymousAuth
platform 固定 linux/amd64
status channel 被并发消费且不会阻塞 Solve
Client、Output 在成功/失败/取消全部关闭
Solve 只调用一次
```

- [ ] **步骤 2：确认红灯**

```bash
go test ./internal/build -run TestSolver -count=1
```

- [ ] **步骤 3：实现最小 Solver**

使用：

```go
fsutil.NewFS(request.ContextDir)
fsutil.NewFS(request.DockerfileDir)
progressui.NewDisplay(progress, progressui.PlainMode)
client.Solve(ctx, nil, solveOpt, status)
```

Solve 与 Display 使用同一取消 Context 并发运行；Solve 成功后 Publish，任何错误 Abort；错误使用 `errors.Join` 保留 Client Close 与输出清理。

- [ ] **步骤 4：验证绿灯**

```bash
gofmt -w internal/build/solve.go internal/build/solve_test.go
go test ./internal/build -run TestSolver -count=1
go test -race ./internal/build -run TestSolver -count=1
```

---

### 任务 7：内嵌 Runtime、真实 Runner 与 CLI

**文件：**

- 创建：`internal/runtimeassets/bundle_stub.go`
- 创建：`internal/runtimeassets/bundle_embedded.go`
- 创建：`internal/runtimeassets/bundle_test.go`
- 创建：`scripts/dev/prepare-runtime-assets.sh`
- 创建：`scripts/dev/prepare-runtime-assets_test.go`
- 创建：`scripts/dev/verify-runtime/main.go`
- 创建：`scripts/dev/verify-oci/main.go`
- 创建：`internal/build/runner.go`
- 创建：`internal/build/runner_test.go`
- 修改：`internal/cli/app.go`
- 修改：`internal/cli/app_test.go`
- 修改：`cmd/sealbuild/main.go`

- [ ] **步骤 1：编写 Runtime Asset 红灯测试**

未带 `sealbuild_runtime` 标签时：

```go
func Bundle() (runtime.Bundle, error)
```

必须返回“Runtime assets are not embedded”明确错误，不联网、不读取环境、不搜索磁盘。准备脚本只接受 Host/Guest archive 路径，调用现有 Artifact 解析验证 kind/platform 后复制到 `internal/runtimeassets/generated/`。

- [ ] **步骤 2：确认红灯并实现资产适配器**

```bash
go test ./internal/runtimeassets ./scripts/dev -run 'TestBundle|TestPrepareRuntimeAssets' -count=1
```

`bundle_embedded.go` 只在 `//go:build sealbuild_runtime` 下编译，用 `go:embed generated/*.tar.zst`，启动时计算 SHA/Size 并返回独立 Reader。

`scripts/dev/verify-runtime` 接受 Host/Guest archive 路径，计算 Asset SHA/Size，分别调用 `runtime.ExtractAsset` 到临时目录；`scripts/dev/verify-oci` 只调用 `build.VerifyOCIArchive`。两个命令都是只读开发验收工具，不成为产品 CLI 命令。

- [ ] **步骤 3：编写 Runner 红灯测试**

固定 API：

```go
type Runner struct {
	Bundle runtime.Bundle
	Layout cache.Layout
	Probe  vm.Probe
	Solver Solver
}

func (runner Runner) Build(ctx context.Context, request Request, progress io.Writer) error
```

Fake Installer/VM seam 验证：安装 Runtime、日志文件、显式代理文件、QEMU 固定路径、StateLockPath、每次一个 VM、Solve 后 Close、错误与 Close 合并。不得调用 Docker 或 shell。

- [ ] **步骤 4：实现 Runner 并验证**

真实路径固定为：

```text
host/bin/qemu-system-x86_64
guest/bzImage
guest/rootfs.ext4
state/buildkit-state.qcow2
```

Ready 30 秒、Probe 250 毫秒、Shutdown 5 秒。它们是产品固定值，不做 CLI 配置。

- [ ] **步骤 5：编写 CLI 红灯测试**

CLI Builder 接口：

```go
type Builder interface {
	Build(ctx context.Context, request build.Request, progress io.Writer) error
}
```

测试 help、必填 Context/Output、重复 build args、Dockerfile、显式 Proxy、未知参数、Builder 错误、stdout/stderr 和退出码。CLI 不显示架构或代理内部实现说明。

- [ ] **步骤 6：实现 CLI 和 main 装配**

`main` 使用 `signal.NotifyContext`，创建 `cache.DefaultLayout()`、`runtimeassets.Bundle()`、`build.Probe`、`build.Solver` 和 `build.Runner`。只有入口调用 `os.Exit`。

- [ ] **步骤 7：验证任务 7**

```bash
gofmt -w internal/runtimeassets internal/build/runner.go internal/build/runner_test.go internal/cli cmd/sealbuild scripts/dev/prepare-runtime-assets_test.go
go test ./internal/runtimeassets ./internal/build ./internal/cli ./cmd/sealbuild -count=1
go test -race ./internal/runtimeassets ./internal/build ./internal/cli -count=1
git diff --check
```

---

### 任务 8：Apple Silicon Mac 真实端到端验收

**文件：**

- 创建：`runtime/testdata/local-build/Dockerfile`
- 创建：`runtime/testdata/local-build/message.txt`
- 修改：`docs/runtime-spike-results.md`
- 修改：`README.md`

- [ ] **步骤 1：等待并验证 Docker Guest 构建**

确认任务 1 长任务退出 0，然后执行：

```bash
go run ./scripts/dev/verify-runtime \
  --guest ./out/guest-runtime-docker/sealbuild-guest-runtime.tar.zst
```

命令必须通过产品 `internal/runtime.ExtractAsset` 校验 Guest Manifest 是 `guest/linux/amd64`、checksums 全部匹配、没有 TLS 私钥、状态盘是 qcow2；不得把 OCI 检查器用于 Runtime tar.zst。

- [ ] **步骤 2：准备内嵌资产并构建 CLI**

```bash
./scripts/dev/prepare-runtime-assets.sh \
  ./out/sealbuild-host-runtime-darwin-arm64-fresh.tar.zst \
  ./out/guest-runtime-docker/sealbuild-guest-runtime.tar.zst

CGO_ENABLED=0 go build \
  -tags sealbuild_runtime \
  -o ./out/sealbuild-darwin-arm64 \
  ./cmd/sealbuild
```

记录二进制大小，必须小于 150 MiB。

- [ ] **步骤 3：创建真实测试 Context**

Dockerfile 固定覆盖：

```dockerfile
FROM alpine:3.22@sha256:7c8cb692ae09657cbc4a3f3cbd0e8d5a2690ba38386aaaf252dbb060bf5eb2e6 AS fetch
ARG HTTP_PROXY
ARG HTTPS_PROXY
RUN wget -qO /downloaded.txt https://example.com/
COPY message.txt /message.txt

FROM scratch
COPY --from=fetch /downloaded.txt /downloaded.txt
COPY --from=fetch /message.txt /message.txt
```

- [ ] **步骤 4：第一次本地构建**

新版 Guest Artifact 产生新的 Compatibility ID，因此默认 Sealbuild Cache 下会自然创建全新状态盘，不新增隐藏环境变量或测试专用产品参数。实际命令：

```bash
time ./out/sealbuild-darwin-arm64 build \
  --proxy http://127.0.0.1:7890 \
  --output ./out/local-build-first.oci.tar \
  ./runtime/testdata/local-build
```

确认输出存在、QEMU 进程退出，并运行产品 `VerifyOCIArchive` 的只读开发命令：

```bash
go run ./scripts/dev/verify-oci ./out/local-build-first.oci.tar
```

- [ ] **步骤 5：第二次独立 VM 缓存构建**

```bash
time ./out/sealbuild-darwin-arm64 build \
  --proxy http://127.0.0.1:7890 \
  --output ./out/local-build-cached.oci.tar \
  ./runtime/testdata/local-build
```

确认第二次再次启动并关闭独立 QEMU，BuildKit progress 包含 cached 结果，两份 OCI 均通过严格平台校验。

```bash
go run ./scripts/dev/verify-oci ./out/local-build-cached.oci.tar
```

- [ ] **步骤 6：最终完整验证**

```bash
test -z "$(gofmt -l $(rg --files cmd internal scripts -g '*.go'))"
sh -n scripts/runtime/*.sh scripts/dev/*.sh
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
CGO_ENABLED=0 go build -tags sealbuild_runtime -o ./out/sealbuild-darwin-arm64 ./cmd/sealbuild
./out/tools/actionlint .github/workflows/runtime-spike.yml
git diff --check
```

- [ ] **步骤 7：记录真实证据**

只在上述命令全部退出 0 后更新 README 和 Spike Results，记录：Guest/Host/CLI 大小、Cold Boot、首次构建、缓存构建、OCI SHA-256、平台和 QEMU 退出。明确其他三个宿主仍未真实验收。

---

## 完成标准

必须同时满足：

```text
新版 qcow2/fw_cfg Guest 在本机 Docker Linux/AMD64 容器生成并通过 Artifact 校验
sealbuild CLI 内嵌 Darwin ARM Host Runtime 与公共 linux/amd64 Guest Runtime
sealbuild build 产品路径不连接 Docker socket、不调用 docker 命令
BuildKit mTLS Probe 只接受唯一 baseline linux/amd64 worker
匿名 Registry Token 和 Guest 网络都只使用同一个显式 Proxy 的正确宿主地址
FROM、COPY、联网 RUN 和多阶段 Dockerfile 首次构建成功
第二次独立 VM 构建成功并命中持久缓存
两份 OCI Archive 的 index、Manifest、Config、digest 和 size 全部有效且严格 linux/amd64
每次构建后 QEMU 退出，状态盘锁和敏感临时文件释放
CLI 小于 150 MiB
go vet、go test、go test -race、CGO_ENABLED=0 build、actionlint 和 git diff --check 全部退出 0
```

完成前不得宣称 Darwin ARM 本地镜像构建可用。
