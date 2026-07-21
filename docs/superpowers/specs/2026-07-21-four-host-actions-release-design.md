# Sealbuild 四宿主 Actions 与 RC 发布设计

## 目标

在 GitHub Actions 的四种真实宿主 Runner 上构建并验收 Sealbuild 发布候选：

- Windows AMD64：`windows/amd64`。
- Linux AMD64：`linux/amd64`。
- Apple Silicon Mac：`darwin/arm64`。
- Intel Mac：`darwin/amd64`。

四个 Sealbuild 候选都只能构建 `linux/amd64` OCI 镜像。每个平台候选必须内嵌对应宿主的 QEMU Host Runtime 和同一份 Linux AMD64 Guest Runtime；最终用户不安装 Docker、WSL、QEMU、MSYS2、Homebrew、KVM、HVF、WHPX、驱动、服务或其他第三方组件。

四个平台在同一 Git Commit 上全部通过后，项目所有者手动创建 `v0.1.0-rc.1` Git Tag。该 Tag 触发同一套构建和验收流程重新生成四个平台产物，并在所有门禁通过后创建 GitHub Pre-release。最终支持声明仍以用户在多个真实平台上测试这批 RC 原始产物为准，GitHub Actions 不替代最终实机验收。

## 当前基线

当前提交 `f3fdd7b` 已完成 Windows AMD64 候选闭环：Windows Host Runtime、单文件 EXE、标准 Dockerfile 首次构建、全新 VM 缓存构建、严格 `linux/amd64` OCI 校验、QEMU 清理和 150 MiB 体积门禁均已在 GitHub Actions 通过。

当前缺口：

- 没有 Linux AMD64 Host Runtime 构建锁、打包器和内嵌资产入口。
- 没有 Darwin AMD64 Host Runtime 构建锁、明确的 Intel Homebrew 路径和内嵌资产入口。
- Darwin ARM64 当前只有本地验收，尚未纳入与 Windows 同等级的统一 Actions 产品门禁。
- Guest Runtime 在现有工作流中重复构建，四平台产物还没有统一来源。
- 没有四平台聚合、统一 checksums、Tag 版本注入和 GitHub Pre-release 发布流程。
- `AGENTS.md` 中“首期 Actions 不要求执行镜像构建”的旧边界已经被本设计取代；实现阶段必须同步改为四平台真实产品构建是 RC 发布硬门禁。

## 方案选择

采用单一四平台候选工作流，不采用四个完全独立工作流，也不让四个平台分别重建 Guest Runtime。

原因：

- Guest Runtime 与宿主无关，只构建一次才能证明四个平台嵌入的是完全相同的 Guest 字节。
- 一个工作流天然绑定同一 Commit 或 Tag，避免跨工作流拼接不同源码版本的产物。
- 四个平台 Host Runtime 和产品验收可以并行，Guest 的高成本构建不会重复四次。
- 聚合 Job 可以在发布前客观检查四个精确文件名、大小和 SHA-256，缺少任何一项立即失败。

不采用以下方案：

- 不通过 `workflow_run` 跨多个工作流寻找“最近成功”的平台产物，因为“最近”不能证明产物来自同一 Commit。
- 不在每个平台重复构建 Guest，因为它放大构建时间和外部源码下载失败面，也无法证明 Guest 字节一致。
- 不复用旧 Workflow Run 的 Runtime Artifact 发布 Tag；Tag 必须从自身指向的源码重新完整构建和验收。

## GitHub Runner

工作流使用 GitHub 官方当前明确提供的固定标签：

| 候选 | Runner | 架构 |
| --- | --- | --- |
| Linux AMD64 | `ubuntu-24.04` | x64 |
| Windows AMD64 | `windows-2025` | x64 |
| Darwin ARM64 | `macos-15` | arm64 |
| Darwin AMD64 | `macos-15-intel` | Intel |

标签依据 GitHub 官方 [GitHub-hosted runners reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners) 和 [runner-images](https://github.com/actions/runner-images)。不使用 `*-latest`，避免 Runner 架构或系统版本在无代码变更时切换。

Runner 镜像和预装包仍可能变化。工作流必须主动验证 `RUNNER_ARCH`、`uname -m`、工具版本和依赖版本；不符合构建锁时失败，不能自动改用另一个 Runner、包版本或下载源。

## 工作流拓扑

新增统一工作流 `.github/workflows/four-host-candidate.yml`，逻辑顺序如下：

```text
quality
  |
  +--> build-guest-runtime -----------------------------+
  |                                                     |
  +--> build-host-linux-amd64 ----> test-linux-amd64 ---+
  +--> build-host-windows-amd64 --> test-windows-amd64 -+
  +--> build-host-darwin-arm64 ---> test-darwin-arm64 ---+--> aggregate
  +--> build-host-darwin-amd64 ---> test-darwin-amd64 ---+       |
                                                                  +--> publish-rc
```

`quality` 先运行格式、静态检查、单元测试和 Race Test。Host Runtime Jobs 可以与 Guest Runtime Job 并行；每个平台产品 Job 同时依赖自己的 Host Runtime 和公共 Guest Runtime。`aggregate` 只依赖四个平台产品 Job。`publish-rc` 只在严格 RC Tag 事件且 `aggregate` 成功时运行。

所有第三方 Actions 使用完整 Commit SHA 固定，不使用浮动主版本标签。除 `publish-rc` 外全部 Job 使用 `contents: read`；发布 Job 单独使用 `contents: write`。

## 触发规则

统一工作流响应：

- `pull_request`：相关源码、Runtime、脚本、工作流或依赖变化时执行完整四平台候选验收。
- `push` 到 `main`：执行完整四平台候选验收，不创建 Release。
- `workflow_dispatch`：允许项目所有者从指定分支或 Commit 手动执行候选验收，不创建 Release。
- `push` 精确 RC Tag：执行完整四平台候选验收，全部通过后创建 Pre-release。

GitHub Tag glob 只能做粗筛选，因此工作流必须再次使用明确正则验证 Tag：

```text
^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[1-9][0-9]*$
```

首个候选为 `v0.1.0-rc.1`。Tag 由项目所有者在已全绿的 Commit 上手动创建并推送，Actions 不自动决定版本、不自动创建 Tag、不移动已有 Tag。

## 公共 Guest Runtime

`build-guest-runtime` 在 `ubuntu-24.04` 上复用当前已验证流程：

1. Checkout 当前 Commit 或 Tag。
2. 安装明确列出的构建依赖。
3. Checkout 固定 Buildroot Commit。
4. 从唯一固定官方 URL 下载 QEMU v11.0.2 源码并校验 SHA-256。
5. 构建当前 Guest Runtime 所需的 `qemu-img`。
6. 运行 `scripts/runtime/build-guest.sh`。
7. 使用 `scripts/dev/verify-runtime` 验证 Guest Manifest、checksums、平台和内容。
8. 上传唯一 Artifact `sealbuild-guest-runtime-linux-amd64`。

不添加自动重试、备用 URL、镜像站、旧 Artifact 复用或失败后下载预编译 Guest。四个平台产品 Job 下载同一个 Workflow Run 中的这一个 Artifact。

## Windows AMD64 Host Runtime

Windows 继续使用已通过的设计和实现：

- Runner 固定为 `windows-2025`。
- QEMU 固定为 v11.0.2 和 Revision `e545d8bb9d63e9dd61542b88463183314cff9482`。
- MSYS2 CLANG64、Clang、LLD 和 MinGW-w64 只作为 Actions 构建工具，不进入用户系统。
- 打包器递归解析 PE Import Directory，复制所有非系统 DLL、固定固件和许可证。
- Windows Host Build Lock 扩展为记录每个最终打包 DLL 对应的 MSYS2 包名、精确版本和许可证；Runner 实际包版本与 Lock 不一致时失败，不能继续生成漂移产物。
- 打包后从 PATH 移除 MSYS2 路径，验证 QEMU 版本、AMD64 PE 和 TCG-only accelerator。

统一工作流迁移现有 `.github/workflows/windows-amd64.yml` 的有效步骤，不并行维护两套不同的 Windows 发布定义。迁移完成且统一流程通过后，旧候选工作流删除，避免同一平台出现两个事实来源。

## Darwin Host Runtime

### 公共打包逻辑

Darwin ARM64 和 Darwin AMD64 共用同一个 Mach-O 依赖闭包、重定位、Manifest、checksums、许可证和归档实现。现有 `scripts/runtime/packagehost` 从只接受 `darwin/arm64` 改为明确接受调用方指定并验证的 Darwin 架构：

- ARM64 只接受 `darwin/arm64` 和 `/opt/homebrew`。
- AMD64 只接受 `darwin/amd64` 和 `/usr/local`。
- QEMU 及每个递归依赖的 Mach-O 架构必须与构建锁一致。
- `/System/Library/Frameworks` 和 `/usr/lib` 视为 macOS 系统依赖，不复制。
- Homebrew 路径中的全部非系统 dylib 递归复制并重写为 `@loader_path` 相对依赖。
- 重定位后对 QEMU 和 dylib 执行 ad-hoc codesign，并验证签名和 TCG-only accelerator。

Homebrew 根目录和目标架构必须由工作流明确传入并由脚本校验。不能在一个架构失败后探测并改用另一个 Homebrew 前缀。

### Darwin ARM64

ARM64 Job 在 `macos-15` 上运行，要求：

- `uname -m` 必须为 `arm64`。
- Homebrew 根目录必须为 `/opt/homebrew`。
- QEMU 构建结果必须为 Mach-O 64-bit arm64。
- 使用 `runtime/host/darwin-arm64/build.lock.json`。

现有本地 Darwin ARM64 Host Runtime 流程迁移到 Actions，并执行与 Windows 相同级别的产品双构建验收。

### Darwin AMD64

Intel Job 在 `macos-15-intel` 上运行，要求：

- `uname -m` 必须为 `x86_64`。
- Homebrew 根目录必须为 `/usr/local`。
- QEMU 构建结果必须为 Mach-O 64-bit x86_64，禁止生成 Universal 或 arm64 QEMU 后假定可运行。
- 新增 `runtime/host/darwin-amd64/build.lock.json`，组件版本、来源、SHA-256、许可证和 QEMU Revision 全部明确记录。

Darwin AMD64 不通过交叉编译或 Rosetta 生成。Host Runtime 必须在真实 Intel Runner 上构建、打包和执行。

## Linux AMD64 Host Runtime

Linux Job 在 `ubuntu-24.04` x64 Runner 上构建原生 AMD64 QEMU，固定 QEMU 版本、Revision、构建选项和依赖版本。配置只保留 Sealbuild 所需能力：

- `x86_64-softmmu`。
- TCG 和 user-mode networking。
- q35、virtio block、virtio net、virtio serial、qcow2 和固定 PC firmware。
- 禁用 KVM、Xen、GUI、音频、文档、Guest Agent、Tools、user-mode emulation 和运行时下载。

新增 Linux Host Build Lock 和专用 ELF 打包器。打包器使用 Go 标准库 `debug/elf` 读取 ELF 架构、Interpreter、`DT_NEEDED`、RPATH 和 RUNPATH，递归形成依赖闭包：

- QEMU、动态加载器和所有实际使用的共享库都进入 Host Runtime。
- 依赖只从工作流显式提供的固定目录解析，不读取任意用户 PATH。
- 同名不同内容、缺失依赖、非 AMD64 ELF、路径逃逸和未锁定组件立即失败。
- 固定固件、许可证、Manifest 和 checksums 与其他 Host Runtime 使用同一归档规范。
- Linux 启动路径显式使用内嵌动态加载器及内嵌 library path，避免要求用户发行版预装 QEMU 依赖，也使静态 Go CLI 可以在没有 glibc 用户空间的 AMD64 Linux 上启动内嵌 QEMU。

Linux 产品 Job 清空第三方工具 PATH 和 `LD_LIBRARY_PATH` 后执行打包产物。它不能调用系统 `qemu-system-x86_64`、Docker、Podman 或其他容器工具。是否覆盖特定旧内核版本属于最终兼容矩阵；本轮 Actions 门禁证明当前 GitHub x64 Linux Kernel 上的原生产品路径。

## Runtime 资产选择

内嵌 Runtime build tags 扩展为且仅为四个平台：

```text
sealbuild_runtime && darwin && arm64
sealbuild_runtime && darwin && amd64
sealbuild_runtime && linux && amd64
sealbuild_runtime && windows && amd64
```

每个平台使用独立的 `Bundle` 文件并返回精确 Host Artifact 名称。公共 `embedded.go` 只在以上四个平台编译；其他平台或未启用 `sealbuild_runtime` 时继续返回明确错误，不提供平台 fallback。

每个平台构建前都运行 `prepare-runtime-assets.sh` 写入本 Job 的 Host Archive 和同一公共 Guest Archive。Tagged CLI 启动前必须校验 Host Manifest 平台与编译目标完全一致，Guest Manifest 必须严格为 `linux/amd64`。

## 产品候选构建

四个平台产品 Job 都在新的 Runner 中运行，不能继承 Host Runtime 构建 Job 的工具进程和工作目录。每个 Job：

1. Checkout 完全相同的 Commit 或 Tag。
2. 下载本平台 Host Runtime 和公共 Guest Runtime。
3. 分别运行 Runtime verifier。
4. 准备内嵌资产。
5. 使用本平台原生 Go、`CGO_ENABLED=0`、`-tags sealbuild_runtime` 构建候选。
6. 注入 Version、Commit 和 BuiltAt。
7. 清理可能提供系统 QEMU、Docker、MSYS2 或 Homebrew 工具的第三方 PATH；只保留运行测试所需的系统路径。
8. 在包含空格的临时目录内执行两次产品构建。
9. 验证两个 OCI Archive、缓存证据、QEMU 清理和体积。
10. 生成候选 SHA-256 并上传平台 Artifact。

候选文件名固定为：

```text
sealbuild-darwin-arm64
sealbuild-darwin-amd64
sealbuild-linux-amd64
sealbuild-windows-amd64.exe
```

不额外生成安装器、ZIP、DMG、MSI、Deb、RPM 或自解压包装。

## 版本注入与可复现元数据

Tag 构建注入完整 Tag，例如 `v0.1.0-rc.1`；非 Tag 构建注入 `dev`。所有构建注入完整 `GITHUB_SHA`。

`BuiltAt` 不使用每个 Job 的当前时间，因为四个平台会得到不同且不可复现的元数据。它使用当前 Commit 的固定提交时间，并统一格式化为 UTC RFC3339。Go 构建使用 `-trimpath`，并显式控制 VCS 元数据，确保版本来源只有工作流传入值。

四个平台执行 `sealbuild version`，验收 Version、Commit 和 BuiltAt 与工作流预期完全一致。Tag 名称、Commit SHA 或时间不一致时不聚合、不发布。

## 标准 Dockerfile 双构建验收

所有平台使用仓库内同一固定测试 Context `runtime/testdata/local-build`。它包含：

- 固定 digest 的 `FROM alpine:3.22`。
- 联网 `RUN wget`。
- 本地 `COPY`。
- 多阶段构建。
- 最终 `FROM scratch`。

每个平台连续执行：

```text
第一次 build -> first.oci.tar
第二次 build -> cached.oci.tar
```

两次调用是两个独立 Sealbuild 进程；每次调用必须启动全新的 QEMU VM，但顺序复用同一 compatibility ID 的 qcow2 BuildKit 状态盘。第二次日志必须包含 BuildKit `CACHED` 证据。不能用同一常驻 VM、Actions cache 或宿主 Docker cache代替 Sealbuild 持久状态盘。

每次构建后必须验证：

- 命令退出码为零。
- OCI verifier 证明唯一镜像平台严格为 `linux/amd64`。
- 本次 QEMU 进程已经退出。
- 状态锁已释放，第二次进程可以正常获取。
- 输出文件存在且不是半成品。

产品测试环境不能暴露 Docker Socket，也不能把 Docker、Podman、系统 QEMU、WSL、KVM、HVF 或 WHPX 作为产品依赖。GitHub Runner 自身预装这些工具不构成失败，前提是 Sealbuild 进程无法通过 PATH 或已知 Socket 使用它们，且测试证据证明实际启动的是解压后的内嵌 QEMU。

网络失败、Registry 限流或上游连接失败直接导致 Job 失败。不添加自动重试、镜像加速站、代理发现、备用 Registry 或基础镜像替换。

## 平台候选门禁

每个平台 Artifact 上传前必须全部满足：

- Host 和 Guest Runtime Manifest、SHA-256、平台、固件和许可证验证通过。
- Host QEMU 架构正确、版本为 v11.0.2、accelerator 只包含 TCG。
- Go 单元测试和平台测试通过。
- 候选可以在清理第三方 PATH 后运行。
- 两次标准 Dockerfile 构建成功。
- 第二次构建存在明确缓存命中。
- 两个 OCI Archive 都严格为 `linux/amd64`。
- 两次构建后均无 QEMU 进程残留。
- 候选文件严格小于 150 MiB。
- 候选 SHA-256 已生成。

任何检查失败都禁止上传“可发布候选”。诊断日志可以通过 `if: always()` 单独上传，但不能与成功候选使用同一个 Artifact 名称。

## 聚合与统一校验

`aggregate` 在新的 Linux Runner 上下载四个平台成功候选，只接受精确文件名。它必须：

1. 证明四个文件各存在且都是普通非空文件。
2. 拒绝任何重复文件名或额外发布文件。
3. 重新计算四个 SHA-256。
4. 生成按文件名排序的统一 `checksums.txt`。
5. 比对每个平台 Job 提供的 SHA-256。
6. 再次检查每个文件小于 150 MiB。
7. 生成包含 Tag、Commit、BuiltAt、四个平台和 Runtime 版本的候选元数据。
8. 上传唯一聚合 Artifact `sealbuild-four-host-candidate`。

聚合不能选择性忽略失败平台，也不能发布三平台或单平台 Release。

## `v0.1.0-rc.1` Pre-release

`publish-rc` 只有在以下条件同时满足时运行：

- 事件是 `push` Tag。
- Tag 通过严格 RC 正则。
- Tag 指向的 Commit 就是本 Workflow Run 的 `GITHUB_SHA`。
- `quality`、Guest、四个 Host、四个产品和 `aggregate` 全部成功。
- GitHub 中不存在同名 Release。

发布 Job 下载聚合 Artifact，重新校验 `checksums.txt`，然后创建 GitHub Pre-release：

```text
v0.1.0-rc.1
├── sealbuild-darwin-arm64
├── sealbuild-darwin-amd64
├── sealbuild-linux-amd64
├── sealbuild-windows-amd64.exe
└── checksums.txt
```

发布使用现有 Tag，不创建或移动 Tag。同名 Release 已存在时失败，不删除、不覆盖、不替换附件。Release 说明明确标记这是待实机验收的候选，不宣称四平台已正式支持。

正式 `v0.1.0` 不在本设计内自动生成。用户完成四平台实机验收后再单独决定是否把同一批已验收字节提升为正式发布；不能未经确认重新构建不同字节并沿用验收结论。

## 错误处理与禁止行为

所有构建和发布阶段保留明确错误上下文。缺少 Runtime、架构不符、依赖闭包不完整、SHA-256 不符、Dockerfile 构建失败、缓存未命中、OCI 平台错误、QEMU 残留、超出体积或 Release 已存在都必须立即失败。

本设计明确禁止：

- 自动重试下载、BuildKit Solve、QEMU 启动或 Release 上传。
- 自动选择镜像站、代理、备用 Registry、备用 URL 或旧 Artifact。
- 自动切换 KVM、HVF、WHPX、Docker、WSL、Podman 或远程 Builder。
- 从其他 Workflow Run 获取最近成功 Runtime。
- 缺少某个平台时发布不完整 Release。
- 同一 Tag 覆盖已有 Release 或替换附件。
- Actions 自动创建、移动或删除 Git Tag。

如后续确实需要上述任一行为，必须作为设计变更先获得项目所有者明确批准。

## 测试层次

### 单元测试

- Darwin Build Lock 同时接受且只接受明确的 ARM64 或 AMD64 平台与依赖集合。
- Darwin Homebrew 根目录、Mach-O 架构和依赖闭包严格匹配。
- Linux Build Lock schema、组件顺序、来源、SHA-256 和许可证校验。
- Linux ELF Interpreter、`DT_NEEDED`、RPATH/RUNPATH、架构、循环依赖、同名冲突和缺失依赖。
- 四个平台 runtimeassets build tags 和 Host Artifact 名称。
- Linux 内嵌动态加载器启动参数，不读取宿主 `LD_LIBRARY_PATH`。
- 版本注入和候选元数据。
- Workflow 关键门禁和禁止行为的静态测试。

### 平台测试

- 四个平台执行 `go test ./...`。
- 支持 Race Detector 的 Runner 执行 `go test -race ./...`。
- Host Runtime 在剥离第三方 PATH 后执行 QEMU `--version` 和 `-accel help`。
- Windows 保留 Job Object、文件锁和含空格路径测试。
- Darwin 两种架构分别验证 Mach-O、codesign、依赖重定位和含空格路径。
- Linux 验证 ELF 闭包、内嵌加载器、无系统 QEMU 和含空格路径。

### 端到端测试

- 每个平台都运行相同的标准 Dockerfile首次构建和缓存构建。
- 每个平台都验证两个 OCI Archive 是 `linux/amd64`。
- 每个平台都验证两次独立 VM 和无 QEMU 残留。
- 每个平台都验证单文件体积和 SHA-256。
- Tag 流程验证四平台聚合、统一 checksums 和不可覆盖 Pre-release。

## 完成标准

只有以下证据全部存在，本阶段才完成：

1. `main` 上统一四平台 Workflow 的同一次运行全部绿色。
2. Linux AMD64、Windows AMD64、Darwin ARM64 和 Darwin AMD64 四个产品 Job 都有首次构建、缓存构建、OCI 平台和 QEMU 清理证据。
3. 四个平台候选均小于 150 MiB，且统一 `checksums.txt` 可验证。
4. 手动创建的 `v0.1.0-rc.1` Tag 从自身源码重新运行同一流程并全部绿色。
5. GitHub `v0.1.0-rc.1` Pre-release 包含且只包含四个候选和 `checksums.txt`。
6. Release 附件 SHA-256 与 Tag Workflow 的聚合 Artifact 完全一致。
7. 文档明确标记四个平台为 RC 候选，等待用户真实设备最终验收。

在用户完成真实 Windows Home、Linux AMD64、Apple Silicon Mac 和 Intel Mac 测试前，不宣称四个平台正式支持，也不创建正式 `v0.1.0`。
