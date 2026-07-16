# Sealbuild 开发路线图

> **执行约束：** 本文件是多阶段路线图，不作为一次性编码计划。每个里程碑开始前，必须使用 Superpowers `writing-plans` 单独生成可执行计划，并按 TDD 推进。

**最终目标：** 在 `darwin/arm64`、`darwin/amd64`、`linux/amd64`、`windows/amd64` 宿主上，仅依赖单个自包含 Sealbuild 二进制，通过 QEMU TCG 完成本地 `linux/amd64` OCI 镜像构建，支持 OCI 文件导出和 Registry 推送。

**核心架构：** GitHub Actions 在 Linux 环境构建公共 `linux/amd64` Guest Runtime，并为四种宿主构建精简 QEMU。Sealbuild 将对应宿主的 QEMU 和公共 Guest Runtime 嵌入二进制，首次运行原子解压，启动 QEMU 后通过回环 TCP 连接 Guest 内的 BuildKit。

**技术栈：** Go 1.26、BuildKit v0.31.1、Buildx v0.35.0（仅参考）、QEMU v11.0.2、runc v1.5.1、Buildroot 2026.05.1。

---

## 推进原则

1. 先验证最危险的假设：Mac ARM 使用纯 TCG 运行 x86-64 Guest，能否完成可接受的标准 Dockerfile 构建。
2. Guest Runtime 可启动、可联网、可执行 `RUN` 后，再开发宿主 CLI 的构建能力。
3. 先完成 OCI 文件导出，再接 Registry 认证和推送，避免同时调试构建与鉴权。
4. 先完成单进程正确性，再实现持久缓存、并发锁和清理。
5. 最后做四平台自包含打包和 GitHub Release，不在核心构建未稳定前优化发布外观。
6. 任一验收门失败时停止推进并向项目所有者报告，不增加硬件加速、远程构建或其他 fallback。

## 前置依赖

- 项目需要初始化 Git，并创建 `github.com/labring/sealbuild` 远端仓库。
- Buildroot 明确要求 Linux 构建宿主；公共 Guest Runtime 应由 GitHub Actions 的 Linux Runner 可复现构建。
- Mac ARM、Intel Mac、x86-64 Linux、Windows Home 的真实端到端验证需要对应测试机器。GitHub Actions 构建成功不能代替真实宿主验收。
- Windows Home 验收必须确认未启用 WSL、Hyper-V、WHPX 等可选功能。

## 里程碑 0：Go 基础框架

**状态：** 已完成。

**已有能力：**

- `github.com/labring/sealbuild` Go Module。
- `cmd/sealbuild` 入口。
- `internal/cli` 命令分发。
- `internal/version` 版本注入。
- 单元测试、Race Test 和四平台 `CGO_ENABLED=0` 交叉编译。

## 里程碑 1：Guest Runtime 可行性验证

**目标：** 构建最小 `linux/amd64` Guest，并证明它能在 QEMU TCG 中启动 BuildKit、联网和执行 Dockerfile `RUN`。

**计划文件：** `docs/superpowers/plans/2026-07-16-guest-runtime-spike.md`

**主要产物：**

- `runtime/manifest.lock.json`：记录 Kernel、Buildroot、BuildKit、runc 和源码 SHA-256。
- `runtime/buildroot/`：独立的 `BR2_EXTERNAL` Tree，不修改 `reference/buildroot`。
- `sealbuild_x86_64_defconfig`：基于 `qemu_x86_64_defconfig` 收敛配置。
- Guest init：配置网络、挂载状态盘、启动 `buildkitd`、输出明确就绪信号。
- Runtime smoke fixture：包含 `FROM`、联网 `RUN`、`COPY` 和多阶段构建。
- Linux CI Artifact：Kernel、rootfs、状态盘模板和 Runtime Manifest。

**技术决策：**

- BuildKit v0.31.1、runc v1.5.1 和 CNI plugins v1.9.1 都使用独立的 Buildroot 外部 package，并校验固定 GitHub Release 资产；禁止静默使用 Buildroot 内置旧版本。
- BuildKit 使用 rootful OCI worker 和独立状态盘，不引入 containerd daemon。
- QEMU Guest 使用 DHCP 和 user-mode networking；BuildKit `RUN` 容器显式使用隔离的 bridge 网络。
- BuildKit 使用 mTLS 监听 Guest TCP 端口，由 QEMU `hostfwd` 映射到宿主回环地址。
- 不启用 KVM、HVF、WHPX 或任何 `-accel` 硬件加速参数。

**验收标准：**

- Buildroot 在 Linux CI 中从固定版本和校验值构建成功。
- QEMU TCG 启动后，BuildKit Client 能列出唯一的 `linux/amd64` worker。
- Smoke Dockerfile 的联网 `RUN` 成功。
- 构建结果导出为 OCI Archive，Manifest 平台严格为 `linux/amd64`。
- 在 Mac ARM 上记录冷启动时间、首次构建时间和缓存构建时间，提交项目所有者确认性能后再进入里程碑 2。
- Guest Runtime 压缩体积需要记录；超过 85 MB 时先分析组成，不立即增加下载式 Runtime。

## 里程碑 2：四宿主 QEMU Runtime

**目标：** 为四种宿主生成可随 Sealbuild 发布的精简 QEMU Runtime，并证明同一 Guest 能在每个平台启动。

**计划文件：** `docs/superpowers/plans/2026-07-16-host-qemu-runtime.md`

**主要产物：**

- `scripts/runtime/build-qemu-*`：按宿主平台构建 QEMU 的脚本。
- `runtime/host/<goos>-<goarch>/manifest.json`：记录 QEMU 文件、动态库、版本和 SHA-256。
- QEMU 最小配置：只保留 x86 system emulation、TCG、virtio block、virtio net、user networking、serial 和必要镜像格式。
- 四个平台的 QEMU Artifact。

**验收标准：**

- 四个平台均能在不安装 Docker、WSL 或可选虚拟化功能的条件下启动同一 Guest。
- QEMU 只监听随机分配的宿主回环端口。
- QEMU 进程退出后不残留子进程、端口和临时文件。
- 每个平台记录 Runtime 文件清单和压缩体积。
- Windows Home 必须在普通用户权限下完成启动验证。

## 里程碑 3：Runtime 解压与 VM 生命周期

**目标：** 在 Go CLI 中可靠管理内嵌资产、首次解压、状态盘、QEMU 生命周期和 BuildKit 就绪。

**计划文件：** `docs/superpowers/plans/2026-07-16-runtime-vm-lifecycle.md`

**主要模块：**

- `internal/runtime`：Manifest、SHA-256、原子解压、版本目录和过期 Runtime 识别。
- `internal/cache`：用户缓存目录、状态盘、跨进程锁和安全清理原语。
- `internal/vm`：QEMU 参数、随机端口、进程组、串口日志、就绪探测、关闭和取消。
- 平台文件：Unix 与 Windows 的进程终止、文件锁和原子替换实现。

**验收标准：**

- 首次解压中断后不会留下可被误用的完整版本目录。
- 资产校验失败时拒绝启动，错误包含资产名和校验阶段。
- 两个并发 Sealbuild 进程不会同时修改同一 Runtime 或状态盘。
- Context 取消、Ctrl+C、QEMU 异常退出和 BuildKit 就绪超时都能清理资源。
- VM 生命周期单元测试不依赖真实 QEMU；独立集成测试使用里程碑 2 Artifact。

## 里程碑 4：`sealbuild build --output`

**目标：** 完成第一个用户可用闭环：本地 Dockerfile Context 构建为 `linux/amd64` OCI Archive。

**计划文件：** `docs/superpowers/plans/2026-07-16-oci-build-output.md`

**主要模块：**

- `internal/build`：BuildKit Client、session、filesync、Dockerfile frontend、SolveOpt 和进度事件。
- `internal/cli`：增加 `build` 命令、Context、Dockerfile、Tag、build args 和 `--output` 参数。
- BuildKit Client 直接连接 QEMU 映射端口，不内嵌或执行宿主 `buildctl`。
- 平台参数在内部固定为 `linux/amd64`，用户不能覆盖为 ARM。

**验收标准：**

- 支持默认和自定义 Dockerfile。
- 支持 `.dockerignore`、`COPY`、联网 `RUN`、多阶段构建和标准 build args。
- `--output image.tar` 生成有效 OCI Archive。
- OCI Index 或 Manifest 的 OS 为 `linux`、Architecture 为 `amd64`。
- 构建失败保留 Dockerfile step 和原始 BuildKit 错误，不输出无上下文的失败信息。
- 本里程碑不实现 Registry 推送和本地 Docker Load。

## 里程碑 5：Registry 认证与 `--push`

**目标：** 使用标准 Docker Registry 凭据完成直接推送，不要求安装 Docker CLI。

**计划文件：** `docs/superpowers/plans/2026-07-16-registry-push.md`

**主要模块：**

- `internal/registry`：读取 `DOCKER_CONFIG` 或平台默认 Docker config 路径。
- BuildKit session auth provider：仅在内存中向 Guest 转发所需凭据。
- `internal/cli`：增加 `--push`，校验 Tag 和输出组合。

**验收标准：**

- 能向带认证的 OCI Registry 推送 `linux/amd64` 镜像。
- 错误输出、日志、进度和测试失败信息中不出现密码、Token 或完整认证头。
- Registry 拒绝认证时返回明确错误，不静默改为 OCI 文件输出。
- `--push` 与 `--output` 可在一次构建中同时使用，并分别验证结果。

## 里程碑 6：持久缓存与 `sealbuild clean`

**目标：** 复用 BuildKit Cache，并提供不会误删用户文件的清理能力。

**计划文件：** `docs/superpowers/plans/2026-07-16-cache-clean.md`

**主要模块：**

- 可增长的 BuildKit 状态磁盘和版本兼容策略。
- `internal/cache` 的占用统计、互斥锁和清理事务。
- `sealbuild clean` 的明确范围和输出。

**验收标准：**

- 相同 Context 重复构建能看到 BuildKit Cache 命中。
- Runtime 升级不会静默复用不兼容状态盘。
- `sealbuild clean` 只删除 Sealbuild 管理的路径。
- 清理进行中遇到活跃构建时返回明确冲突错误，不强制终止构建。

## 里程碑 7：自包含打包与 GitHub Release

**目标：** 自动生成四个平台的单文件发布产物、版本信息和校验文件。

**计划文件：** `docs/superpowers/plans/2026-07-16-release-packaging.md`

**主要产物：**

- 平台 Build Tags 和 `go:embed` 资产入口。
- Runtime Artifact 合并和发布打包工具。
- GitHub Actions 测试、构建和 Tag Release Workflow。
- `sealbuild-darwin-arm64`、`sealbuild-darwin-amd64`、`sealbuild-linux-amd64`、`sealbuild-windows-amd64.exe`、`checksums.txt`。

**验收标准：**

- 四个平台发布文件均包含正确宿主 QEMU 和相同 Guest Runtime。
- `sealbuild version` 输出 Git Tag、Commit 和全部 Runtime 组件版本。
- 同一源码和 Lock Manifest 重建得到一致的 Runtime 内容校验值。
- 单个平台发布文件不超过 150 MB；超过时停止发布并提交体积明细给项目所有者确认。
- Release 缺少任一平台产物或 SHA-256 时整体失败。
- GitHub Actions 只作为项目测试、Runtime 构建和发布系统，不作为 Sealbuild 的远程镜像构建后端。

## 里程碑 8：真实宿主验收与首个稳定版本

**目标：** 在四种真实宿主上完成干净环境端到端验收，再发布首个稳定版本。

**验收矩阵：**

| 宿主 | 启动 | OCI 输出 | Registry Push | 缓存 | Clean | 无额外系统功能 |
| --- | --- | --- | --- | --- | --- | --- |
| Mac ARM | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 |
| Intel Mac | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 |
| Linux AMD64 | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 |
| Windows AMD64 Home | 必须 | 必须 | 必须 | 必须 | 必须 | 必须 |

**最终验收标准：**

- 干净宿主只下载一个 Sealbuild 发布文件即可构建。
- 首次运行不下载 Runtime 组件。
- 构建镜像全部为 `linux/amd64`。
- 标准 Smoke Dockerfile 在四平台结果一致。
- 没有管理员权限、Docker、WSL、Hyper-V、WHPX、KVM 和远程构建依赖。
- 已记录四平台冷启动、首次构建、缓存构建、发布体积和磁盘占用。

## 推荐立即执行的下一步

只启动里程碑 1「Guest Runtime 可行性验证」，不要同时开发 CLI `build` 命令。

原因：如果纯 TCG 在 Mac ARM 上无法以可接受速度运行 BuildKit，或 Kernel/OCI worker 能力无法在目标体积内满足，后续 CLI、Registry 和发布开发都会失去基础。里程碑 1 完成后，把真实时间、体积和兼容结果提交项目所有者，再决定是否进入四宿主 QEMU Runtime。

## 粗略工作量

- 里程碑 1：3～5 个工程日。
- 里程碑 2：4～7 个工程日。
- 里程碑 3：3～5 个工程日。
- 里程碑 4：4～6 个工程日。
- 里程碑 5：2～4 个工程日。
- 里程碑 6：2～4 个工程日。
- 里程碑 7：3～6 个工程日。
- 里程碑 8：取决于四种真实测试机器的可用性。

上述估算不包含等待 GitHub 仓库、签名证书或测试机器的时间。任何里程碑出现技术门失败时，先讨论需求取舍，不把时间转移到未经确认的替代路径。
