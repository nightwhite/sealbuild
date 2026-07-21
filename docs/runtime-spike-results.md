# Guest Runtime Spike Results

## Status

里程碑 1 已完成 Runtime 定义、严格版本锁、Buildroot External Tree、Guest 启动配置、mTLS、QEMU TCG Smoke Test 和 Linux GitHub Actions。2026-07-20 又完成新版 fw_cfg/qcow2 Guest、Darwin ARM 单文件 CLI、BuildKit Go Client、本地 OCI 导出、跨独立 VM 缓存复用和正常关机协议的真实验收。

当前可以宣称 Darwin ARM 本地构建 `linux/amd64` OCI Archive 可用。统一四宿主候选也已在 GitHub-hosted Linux AMD64、Windows Server 2025 x64、Apple Silicon Mac 和 Intel Mac Runner 完成真实端到端验收。GitHub-hosted Runner 不能替代用户最终实机验收，因此当前只宣称四宿主 CI 候选可构建和可测试，不宣称 Windows Home、Linux 或两种 Mac 正式支持。

## Four-host Actions candidate verified on 2026-07-22

成功运行：[Four host candidate #29850164965](https://github.com/nightwhite/sealbuild/actions/runs/29850164965)，PR Head Commit `3fdfea54c6974eb3bbd47b03473c7cadaccd4219`。`pull_request` 事件实际 Checkout 并注入四个候选的是 GitHub 合并测试 Commit `cf0a7c7351c761df52a02dba52b4e28ae1b9ffab`；四个平台候选和统一元数据均明确记录同一个 Commit。

- `quality`、唯一 QEMU 源准备、公共 Guest、四个 Host Runtime、四个产品双构建和统一聚合全部成功；只有非 Tag 事件专用的 `publish-rc` 按设计跳过。
- QEMU v11.0.2 源码只从固定官方 URL 下载一次并校验 SHA-256，随后由当前 Workflow Run 的 Artifact 分发给 Guest 和四个 Host Job；没有备用 URL、自动重试或跨 Run 产物复用。
- Darwin 依赖许可证文本来自与 Build Lock 完全匹配的已校验源码归档，固定在 `runtime/host/darwin-licenses`；Build Lock 对 13 个实际打包许可证文件逐个锁定 SHA-256，打包器校验最终复制内容。
- Linux、Windows、Darwin ARM64 和 Darwin AMD64 Host Runtime 均在对应原生 Runner 构建并执行；QEMU v11.0.2 的 accelerator 严格只有 `tcg`。
- 四个产品 Job 都不安装 Docker、WSL、MSYS2、系统 QEMU 或其他产品依赖；候选只使用自身内嵌 Host Runtime 和同一份 Linux AMD64 Guest Runtime。
- 每个平台连续运行两次标准 Dockerfile 构建，包含固定摘要基础镜像、联网 `RUN wget`、`COPY` 和多阶段 `FROM scratch`；第二次使用新的 QEMU VM 和同一持久缓存盘。
- Linux、Darwin ARM64、Darwin AMD64 的缓存日志各有 9 条 `CACHED`，Windows 有 5 条 `CACHED`。
- 每个平台的两个 OCI Archive 都由项目检查器输出 `OCI platform: linux/amd64`；每个平台都验证恰好生成 2 个 VM serial log，并在每次构建后确认无 QEMU 进程残留。
- 产品 Job 耗时：Linux AMD64 1 分 56 秒、Darwin ARM64 2 分 16 秒、Windows AMD64 3 分 8 秒、Darwin AMD64 4 分 37 秒。
- 公共 Guest Runtime 冷构建 41 分 6 秒；Host Runtime 构建耗时为 Linux 3 分 43 秒、Darwin ARM64 4 分 26 秒、Darwin AMD64 6 分 59 秒、Windows 13 分 33 秒。
- 统一聚合 Job 重新校验四个平台独立 SHA-256，并拒绝缺失、额外、空文件或达到 150 MiB 的候选。
- `sealbuild-darwin-arm64`：56,352,466 字节，SHA-256 `330f522a9883e5270bb7203800fbbc5273f8ff3e9a6bc79385fdf1c8f170fb46`，Mach-O arm64。
- `sealbuild-darwin-amd64`：57,755,136 字节，SHA-256 `43c8cb3b0eabdbe6fe74ca63471c4488408ebd7268eae66be586d87e7d806c23`，Mach-O x86_64。
- `sealbuild-linux-amd64`：60,297,378 字节，SHA-256 `893b7f94d3bf165ecdec0f9fcf5d67e50a766ddb47f9daf822683208bf2a1de1`，静态 ELF x86-64。
- `sealbuild-windows-amd64.exe`：73,206,784 字节，SHA-256 `73df708d294c803bd39d2c8057c3f34ab8ac446966915db5acbab5b6fa8f3985`，PE32+ x86-64。
- 下载后的 `checksums.txt` 对四个候选全部返回 `OK`；独立重新计算的大小和 SHA-256 与 `candidate.json` 完全一致，四个文件均低于 150 MiB。
- 尚缺证据：同一 RC 产物在 Windows 10/11 Home 普通用户、常见 Linux AMD64 发行版、Apple Silicon Mac 和 Intel Mac 实机各完成至少一次构建。

## Windows AMD64 Actions candidate verified on 2026-07-21

成功运行：[Windows AMD64 candidate #29813868783](https://github.com/nightwhite/sealbuild/actions/runs/29813868783)，Commit `1f1558c4548a935cc036a5f77de9758aacb42a25`。

- Windows 文件锁使用 `LockFileEx` 非阻塞独占锁，竞争映射到现有 `ErrContended`；本机完成 Windows 测试二进制交叉编译，真实行为由 Windows Actions 运行。
- Windows QEMU 使用第二个 `127.0.0.1` 随机 TCP 端口承载 Guest shutdown acknowledgement，不使用 Unix Socket、WSL、WHPX、Hyper-V 或远程 builder。
- QEMU 进程以 `CREATE_NO_WINDOW` 启动并立即加入 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` Job Object；正常路径仍优先等待 Guest 停止 BuildKit、同步并卸载状态盘。
- Windows Host 打包器直接解析 PE Import Directory 并验证 AMD64 PE，递归收集包括 ordinal-only import 在内的 DLL 闭包；系统 DLL 使用固定 allowlist，私有 DLL 缺失、同名冲突或非 AMD64 时失败。打包后的 QEMU 在清空 MSYS2 PATH 后再次执行版本和 TCG-only 校验。
- Runtime Installer 现在同时校验 Artifact kind 和当前宿主平台，Windows EXE 不会误用 Darwin Host Runtime。
- `.github/workflows/windows-amd64.yml` 包含 `build-guest-runtime`、`build-windows-runtime` 和 `test-windows-product` 三个隔离 Job；Actions 固定到 Commit SHA，QEMU 固定 v11.0.2 Revision。
- 产品 Job 不安装 MSYS2、Docker、WSL 或 QEMU。它下载两个 Runtime 后构建单文件 EXE，在包含空格的目录清空第三方 PATH，连续执行两次 Dockerfile 构建，要求第二次出现 `CACHED`，并验证两个 OCI 都是 `linux/amd64`、无 QEMU 残留且 EXE 小于 150 MiB。
- Windows Host Runtime：20,934,031 字节，SHA-256 为 `6e21dd14d4cf17e773d6e31be1f5c9c9c26813b26020ade7d27d94048c622032`；包内 QEMU 为 v11.0.2，accelerator 严格只有 `tcg`。
- Linux AMD64 Guest Runtime：31,305,931 字节，SHA-256 为 `9e06ab7669c2467503c1da1e7cba9118faf022cb88deb3c77823d9dfca5d4969`。
- 单文件 `sealbuild-windows-amd64.exe`：82,245,120 字节，约 78.4 MiB，SHA-256 为 `22ea2e33f9784746d53b55d4fb0e278c25f6d6bff0ee836b922f0c01dc92324a`，低于 150 MiB 门禁。
- 标准 Dockerfile 首次构建完成固定 digest Alpine 拉取、联网 `RUN wget`、`COPY`、多阶段 `FROM scratch` 和 OCI 导出；第二次使用新的 QEMU VM 和同一持久缓存盘，4 个关键步骤显示 `CACHED`。
- 两次 OCI 检查均为 `linux/amd64`，manifest digest 均为 `sha256:ca9b89b2c4848c74c2e575cdd9b9f69ebf49e47776d50ab106c94fa4b21d6daa`，config digest 均为 `sha256:5ca72b2aa43e7bf8599307e69458935f7bfef0e541f64415d56f81908c6e0cbb`。
- 产品 Job 的双构建、缓存检查、OCI 检查和 QEMU 进程残留检查全部通过；产品 Job 总耗时 2 分 59 秒，其中两次构建与清理步骤耗时 1 分 2 秒。
- 尚缺证据：Windows 10/11 Home x64 普通用户实机至少完成一次相同构建。GitHub Actions 的 Windows Server 2025 结果不能替代该门禁。

成功运行：[Runtime spike #29495019169](https://github.com/nightwhite/sealbuild/actions/runs/29495019169)，Commit `98f89cb83a9cac627f3c69b58fa9a654f155c527`。

## Locally verified on 2026-07-16

- 宿主：macOS arm64。
- Go：1.26.1。
- `go vet ./...`：通过。
- `go test ./...`：通过。
- Runtime Lock：schema、`linux/amd64` 和 SHA-256 约束通过。
- OCI Archive 检查器：`linux/amd64`、ARM、缺平台和多 Manifest 测试通过。
- Spike mTLS：Server 与 Client Certificate 均通过 CA 验证，生成目录不保留 CA 私钥。
- Buildroot External Tree：`sealbuild_x86_64_defconfig` 可被固定 Buildroot checkout 发现并展开。
- Buildroot Kconfig：Sealbuild BuildKit、runc、CNI、musl、CA、iptables、cgroup2 和 ext4 均被选中。
- Buildroot Kconfig：内置旧版 `BR2_PACKAGE_RUNC` 和 `BR2_PACKAGE_MOBY_BUILDKIT` 均未启用。
- GitHub Actions：`actionlint v1.7.7` 通过。

## Linux CI evidence

运行环境：GitHub-hosted `ubuntu-24.04` AMD64 Runner；QEMU 明确使用 TCG，不使用 `/dev/kvm`。

- QEMU v11.0.2 构建：3 分 11 秒。
- Guest Runtime 冷构建：37 分 35 秒。
- QEMU TCG Smoke：20 秒。
- Guest 冷启动至 BuildKit Ready：7 秒。
- 首次 Dockerfile 构建：12 秒。
- 缓存 Dockerfile 构建：1 秒。
- Guest Runtime 压缩文件：36,070,725 字节，约 34.4 MiB。
- BuildKit：v0.31.1，Revision `673b7e0196de0cac83308274b88aaed97a91af74`。
- Worker：唯一 OCI worker，`native` snapshotter，CNI 网络，平台仅包含 `linux/amd64` 及其 `v2`、`v3` 微架构变体。
- Smoke Dockerfile：联网 `RUN`、`COPY` 和多阶段构建通过。
- 首次构建和缓存构建均导出 9,216 字节 OCI Archive。
- 两个 OCI Archive 均由项目检查器确认平台严格为 `linux/amd64`。

## Known diagnostics

以下启动日志没有导致本次构建失败，但需要在 Runtime 收敛阶段处理：

- Buildroot 的 cgroup2 启动脚本重复挂载已经由 `/etc/fstab` 挂载的 cgroup2，输出 `Resource busy`。
- `seedrng` 尝试在只读 rootfs 写入 `/var/lib/seedrng`，输出只读文件系统错误。
- BuildKit 在 `proxyNetwork` 未启用时仍预填充 proxy network namespace，最小 Kernel 返回 `protocol not supported`；普通 bridge 网络的两次 Smoke 构建均成功。
- Guest 不包含 `git`，BuildKit 明确警告 Git source frontend 不可用；本阶段要求的本地 Dockerfile Context 不受影响。

## Darwin ARM local OCI build verified on 2026-07-20

- 宿主：Apple Silicon arm64，产品构建路径未调用 Docker；Docker 只用于开发期生成 Guest Runtime。
- 目标：固定 `linux/amd64`，两份 OCI Archive 均由产品校验器通过，大小均为 9,216 字节。
- Host Runtime：4,787,133 字节，包含 QEMU v11.0.2、递归 dylib 和启动所需的固定 PC firmware 文件。
- Guest Runtime：31,304,785 字节，包含 Linux 6.18.7、BuildKit v0.31.1、runc v1.5.1、CNI v1.9.1 和 32 GiB qcow2 空缓存模板。
- 单文件 CLI：65,646,530 字节，约 62.6 MiB，SHA-256 为 `fb76f0a9d91e30b8c462c6546b9c88cc62a2e116fa8f655bd813408666e5e25f`，低于 150 MiB 单平台上限。
- 显式代理：`127.0.0.1:7890` 在 Guest 中固定转换为 `10.0.2.2:7890`；不读取环境代理，不重试 Solve。Docker Hub 固定优先使用 `https://docker.1ms.run`，并通过 `NO_PROXY` 绕过本地代理直连该镜像站。
- 测试 Context：固定摘要 Alpine 3.22、联网 `RUN`、`COPY` 和多阶段构建。
- 第一次构建：37.52 秒，包含基础层拉取、联网 `RUN` 和 OCI 导出。
- 第二次构建：12.66 秒，使用同一持久缓存盘，但启动不同的 QEMU VM；4 个 Dockerfile 执行步骤均显示 `CACHED`。
- 生命周期：两次构建分别生成 `vm-2482201276.serial.log` 与 `vm-4211247737.serial.log`；每次结束后均无 QEMU 进程残留。
- 缓存一致性：第二次启动日志没有 ext4 journal recovery，也没有 `SEALBUILD_RUNTIME_FAILED`。Guest 通过 virtio-serial 收到 shutdown，停止 BuildKit、同步、卸载状态盘后关机。

## Real Node and Rust Dockerfile verified on 2026-07-21

- Context：真实 `aws-account` 项目，Dockerfile 包含 Node 22 前端、Rust 1.91 后端、Debian runtime、pnpm、Cargo、apt、nginx、`COPY --from` 和 ENTRYPOINT。
- 网络：Docker Hub 基础镜像通过 `docker.1ms.run`；pnpm 与 Cargo 使用显式 `127.0.0.1:7890` 代理；`--no-proxy deb.debian.org,.debian.org` 让 apt 直连 Debian。
- 资源根因：固定 2 vCPU 时重型 Rust 编译导致 BuildKit session healthcheck 连续超时；4 vCPU 消除该问题。2 GiB 内存时 `aws-sdk-bedrock` 的 rustc 被 `SIGKILL`；4 GiB 后该 crate 约 4 分钟完成。
- 当前 VM 配置：QEMU TCG-only、4 vCPU、4 GiB 内存；未启用 HVF、KVM 或其他宿主硬件虚拟化。
- 首次成功构建：28 分 15.78 秒，其中 `cargo build --release` 为 26 分 32 秒；导出 45 MiB OCI Archive。
- 缓存构建：使用同一持久缓存盘和新的 QEMU VM，总耗时 11.84 秒；18 个 Dockerfile 步骤显示 `CACHED`，包括完整 Rust release 构建。
- 两次导出的 OCI manifest digest 均为 `sha256:2cf014e44be1a73ddd0cd3f7deb978b571950389c313843c4f56a6bff08655a4`，config digest 均为 `sha256:7a73237ded73fee5d699d4bb0275cfb17aa3803a2ce416c2e0fe9dcc06208ebb`；产品检查器确认平台严格为 `linux/amd64`。
- 两次成功构建结束后均无 QEMU 进程或状态锁持有者残留。

## Mac ARM raw-ext4 spike evidence

运行环境：Apple Silicon arm64、macOS 26.5.1、10 个 CPU 核心、25 GiB 内存。

- QEMU：从固定的 v11.0.2 源码构建，宿主产物为 Mach-O arm64，`-accel help` 仅列出 `tcg`。
- BuildKit Client：从固定的 v0.31.1 源码构建为原生 `darwin/arm64`，用于本次 Smoke Test。
- Guest Runtime：直接使用 Linux CI 产出的同一份 `sealbuild-guest-runtime.tar.zst`，SHA-256 校验全部通过。
- 状态盘：逻辑大小 4 GiB，macOS 解压后实际占用约 2.3 MiB，普通 `cp` 保持稀疏布局和文件 SHA-256。
- 网络：本机直连 Docker Hub 超时，本次测试显式使用本机 `127.0.0.1:7890` 代理；Guest 通过 QEMU slirp 网关 `10.0.2.2:7890` 使用同一代理。宿主与 Guest 均未配置代理时，Guest 访问 Registry 超时；只配置 Guest 时，宿主侧获取鉴权 Token 超时。成功测试同时显式配置两侧代理，不存在自动切换或静默降级。
- Worker：唯一 OCI worker，平台包含基线 `linux/amd64`，未出现 ARM 平台。
- Smoke Dockerfile：联网 `RUN`、`COPY` 和多阶段构建通过。
- 首次构建与缓存构建均导出 9,216 字节 OCI Archive，项目检查器确认平台严格为 `linux/amd64`。

| Measurement | Result |
| --- | --- |
| Cold boot | 4 秒 |
| First build | 36 秒 |
| Cached build | 1 秒 |
| Compressed Guest Runtime | 36,070,725 字节，约 34.4 MiB |

## Darwin ARM packaging observations

- 2026-07-17 在 macOS 26.5.1 arm64 上从固定 Commit `e545d8bb9d63e9dd61542b88463183314cff9482` 完成全新 QEMU v11.0.2 冷构建，1689 个 Ninja 目标全部完成，`-accel help` 严格只包含 `tcg`。
- 冷构建固定使用 Python 3.14.6，并从该 Python 安装包内置、SHA-256 固定的 setuptools 79.0.1 wheel 创建隔离 bootstrap venv；整个 QEMU configure 保持 `--disable-download`，不访问 PyPI。
- Host Artifact 内包含 QEMU、9 个递归依赖 dylib 和 7 个组件的许可证，共 23 个 payload 文件。所有非系统 Mach-O 依赖均重写为 `@loader_path`，所有 Mach-O 均通过 ad-hoc codesign strict verify。
- 新 Host Artifact 压缩大小约 4.6 MiB，解包大小约 28 MiB，SHA-256 为 `690d32325fa6e00c754cde6b08446c466d471362bd193f85e77380f0ee29b22f`。
- 在 `env -i HOME=/tmp PATH=/usr/bin:/bin` 环境中，解包后的 QEMU 可输出 v11.0.2 和仅 `tcg` 的 accelerator 列表；`otool -L` 闭包不存在 `/opt/homebrew`、`/usr/local` 或用户目录路径。
- 本次原生 `buildctl` 为 32 MiB，zstd 压缩后约 13.8 MiB。它只用于 Spike，正式 Sealbuild 将直接使用 BuildKit Go Client，不会再携带独立 `buildctl`。
- 当前 Host Artifact 体积远低于单平台 150 MiB 上限；最终单文件大小仍需加上新版 Guest Runtime 和 Go CLI 后重新测量。

## Guest packaging migration verified on 2026-07-17

- Kernel 配置启用 `CONFIG_FW_CFG_SYSFS` 与 `CONFIG_FW_CFG_SYSFS_CMDLINE`；rootfs 构建不再接收或复制 TLS 私钥。
- Guest init 在状态盘挂载后，从 QEMU fw_cfg 读取 CA、Server Certificate、Server Key 和可选显式代理；BuildKit TLS 路径全部位于状态盘 Runtime 目录。
- 构建脚本固定 qemu-img v11.0.2，把 32 GiB raw ext4 模板转换为 `compat=1.1,lazy_refcounts=on` qcow2，并通过 JSON 检查格式与虚拟容量。
- Guest Lock 已移除 Host QEMU，并增加 BuildKit、runc、CNI 三个固定源码归档。真实 HTTPS 下载与 SHA 校验通过，collector 从 PAX tar.gz 和测试用 legal-info 树中生成 322 个许可证文件，约 3.1 MiB。
- Guest Artifact Builder 的确定性、无 TLS payload、qcow2 文件名、symlink 拒绝、Manifest/checksums 发布和 85 MiB 压缩上限均有本地测试覆盖。
- Smoke 与 Linux workflow 已迁移到 qcow2 和 fw_cfg；`actionlint v1.7.7` 通过。
- 该阶段遗留的新版 Guest 真实验收已于 2026-07-20 完成，结果见上方 Darwin ARM local OCI build 章节。

Darwin ARM 已完成自包含单文件本地构建验证；四宿主已完成统一 GitHub Actions CI 候选验收，但仍等待 Windows Home、Linux AMD64、Apple Silicon Mac 和 Intel Mac 实机门禁。Registry Push 仍不在当前交付范围内。
