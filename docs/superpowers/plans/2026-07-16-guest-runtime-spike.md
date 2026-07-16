# Sealbuild Guest Runtime 可行性验证实施计划

> **执行方式：** 使用 Superpowers `executing-plans` 按顺序执行。项目当前没有 Git 仓库，因此本计划不创建分支、worktree 或 commit；用户明确要求初始化 Git 后再补版本控制步骤。

**目标：** 生成一个固定版本、可复现的 `linux/amd64` Guest Runtime，并证明它能通过 QEMU 纯 TCG 启动 BuildKit、执行联网和多阶段 Dockerfile、导出平台严格为 `linux/amd64` 的 OCI Archive。

**架构：** Buildroot 2026.05.1 生成 x86-64 Kernel 和最小 ext4 rootfs。独立 `BR2_EXTERNAL` Tree 从固定 GitHub Release 资产安装 BuildKit v0.31.1、runc v1.5.1 和 CNI plugins v1.9.1。Guest 以 rootful 模式启动唯一 OCI worker，显式使用 `native` snapshotter 和 `bridge` 网络。QEMU 只使用 TCG 与 user-mode networking，将 Guest 的 mTLS BuildKit 端口转发到宿主 `127.0.0.1`。

**技术栈：** Buildroot 2026.05.1、Linux 6.18.7、BuildKit v0.31.1、runc v1.5.1、CNI plugins v1.9.1、QEMU v11.0.2、Go 1.26。

**禁止项：** 不使用 Buildroot 内置的旧版 BuildKit/runc；不启用 host network、auto snapshotter、硬件加速、远程 builder、自动 fallback 或静默重试。

---

### 任务 1：定义并验证 Runtime Lock Manifest

**文件：**
- 创建：`runtime/manifest.lock.json`
- 创建：`internal/runtime/lock.go`
- 创建：`internal/runtime/lock_test.go`

- [ ] 先编写表驱动测试，要求 Manifest schema 固定为 `1`、Guest 平台固定为 `linux/amd64`、组件版本与 SHA-256 非空且 SHA-256 为 64 位小写十六进制。
- [ ] 运行 `go test ./internal/runtime`，确认测试因缺少解析与校验实现失败。
- [ ] 实现最小 `LoadLock(io.Reader)` 和 `Validate()`；只验证本里程碑需要的不变量，不加入默认值或兼容旧 schema 的 fallback。
- [ ] 写入 Buildroot、Linux、BuildKit、runc、CNI plugins、QEMU 的固定版本、来源、Commit 或发布资产 SHA-256。
- [ ] 运行 `go test ./internal/runtime`，预期通过。

### 任务 2：建立独立 Buildroot External Tree

**文件：**
- 创建：`runtime/buildroot/external.desc`
- 创建：`runtime/buildroot/Config.in`
- 创建：`runtime/buildroot/external.mk`
- 创建：`runtime/buildroot/package/sealbuild-buildkit/Config.in`
- 创建：`runtime/buildroot/package/sealbuild-buildkit/sealbuild-buildkit.mk`
- 创建：`runtime/buildroot/package/sealbuild-buildkit/sealbuild-buildkit.hash`
- 创建：`runtime/buildroot/package/sealbuild-runc/Config.in`
- 创建：`runtime/buildroot/package/sealbuild-runc/sealbuild-runc.mk`
- 创建：`runtime/buildroot/package/sealbuild-runc/sealbuild-runc.hash`
- 创建：`runtime/buildroot/package/sealbuild-cni-plugins/Config.in`
- 创建：`runtime/buildroot/package/sealbuild-cni-plugins/sealbuild-cni-plugins.mk`
- 创建：`runtime/buildroot/package/sealbuild-cni-plugins/sealbuild-cni-plugins.hash`

- [ ] 三个包都使用精确版本和 GitHub Release URL，不引用 `latest`。
- [ ] BuildKit 包只安装 `buildkitd`；runc 包安装为 `/usr/bin/runc`；CNI 包只安装 `bridge`、`loopback`、`host-local`、`firewall`。
- [ ] 每个下载资产都由 Buildroot hash 文件校验；校验失败必须终止，不允许改用其他 URL。
- [ ] 在 Linux 执行 `make BR2_EXTERNAL=$PWD/runtime/buildroot sealbuild_x86_64_defconfig`，预期配置成功且不会修改 `reference/buildroot`。

### 任务 3：定义最小 x86-64 Guest

**文件：**
- 创建：`runtime/buildroot/configs/sealbuild_x86_64_defconfig`
- 创建：`runtime/buildroot/board/sealbuild/x86_64/linux.config`
- 创建：`runtime/buildroot/board/sealbuild/x86_64/rootfs-overlay/etc/inittab`
- 创建：`runtime/buildroot/board/sealbuild/x86_64/rootfs-overlay/etc/buildkit/buildkitd.toml`
- 创建：`runtime/buildroot/board/sealbuild/x86_64/post-build.sh`

- [ ] Defconfig 固定 x86-64、Linux 6.18.7、musl、BusyBox、DHCP、CA certificates、ext4 rootfs 和三个 Sealbuild 外部包。
- [ ] Kernel 配置显式包含 namespaces、cgroup2、veth、bridge、netfilter、overlay/ext4、virtio block/net 和 devtmpfs；删除图形、声音、USB 等无关设备。
- [ ] BuildKit 配置只启用 rootful OCI worker，平台固定 `linux/amd64`，snapshotter 固定 `native`，network 固定 `bridge`，containerd worker 固定关闭。
- [ ] Rootfs overlay 不包含构建缓存；缓存必须挂载在独立 virtio 状态盘。
- [ ] Linux 执行 `make ... olddefconfig` 后检查 `.config`，预期不存在 Buildroot 内置 `BR2_PACKAGE_RUNC` 和 `BR2_PACKAGE_MOBY_BUILDKIT`。

### 任务 4：实现 Guest Init 与 mTLS

**文件：**
- 创建：`runtime/buildroot/board/sealbuild/x86_64/rootfs-overlay/etc/init.d/S40sealbuild-runtime`
- 创建：`runtime/tls/README.md`
- 创建：`scripts/runtime/generate-spike-certs.sh`

- [ ] Init 脚本验证 `/proc`、`/sys`、`/dev`、cgroup2、网络和状态盘，任一必要条件失败即退出并向串口打印明确阶段。
- [ ] 状态盘挂载到 `/var/lib/buildkit`，rootfs 保持无构建状态。
- [ ] `buildkitd` 使用 `--addr tcp://0.0.0.0:1234`、server cert、server key 和 CA，且要求 client certificate。
- [ ] 证书脚本只用于生成 Spike 固定输入，产物不提交 Git；脚本不在失败时改用明文 TCP。
- [ ] BuildKit worker 就绪后串口输出唯一标记 `SEALBUILD_RUNTIME_READY`；启动失败输出 `SEALBUILD_RUNTIME_FAILED` 并退出。

### 任务 5：构建 Runtime 与生成 Artifact

**文件：**
- 创建：`scripts/runtime/build-guest.sh`
- 创建：`scripts/runtime/package-guest.sh`

- [ ] `build-guest.sh` 仅接受固定的 Buildroot checkout 和独立输出目录，检查宿主为 Linux 后执行 defconfig 与构建。
- [ ] 构建前验证 `reference/buildroot` Commit 为 `cb857ba4c87a93e5265a9e4a3f32071abf39e14a`；不匹配直接失败。
- [ ] `package-guest.sh` 复制 Kernel、rootfs、Manifest 和 mTLS 客户端材料到临时目录，生成 SHA-256 后原子移动为 Artifact 目录。
- [ ] 记录未压缩和压缩体积；压缩 Guest Runtime 超过 85 MB 时输出体积明细并使验收门失败，不自动删功能或改为联网下载。

### 任务 6：添加 QEMU TCG Smoke Test

**文件：**
- 创建：`runtime/smoke/Dockerfile`
- 创建：`runtime/smoke/input.txt`
- 创建：`scripts/runtime/smoke-guest.sh`
- 创建：`scripts/runtime/inspect-oci-platform.go`
- 创建：`scripts/runtime/inspect-oci-platform_test.go`

- [ ] Dockerfile 包含联网 `RUN`、`COPY` 和多阶段构建，最终 stage 不依赖宿主架构。
- [ ] 先为 OCI Index/Manifest 平台检查编写失败测试，再实现只接受 `linux/amd64` 的检查器。
- [ ] Smoke 脚本使用 `qemu-system-x86_64 -accel tcg -nographic`、virtio 状态盘和调用方指定的空闲回环端口；端口冲突立即失败，禁止重试其他端口或追加其他 accelerator。
- [ ] 脚本等待串口明确就绪标记并使用 mTLS 连接，不以固定 `sleep` 判定成功。
- [ ] 通过 BuildKit client 列出 worker、构建两次并导出 OCI Archive，记录冷启动、首次构建、缓存构建和输出大小。
- [ ] 平台检查器确认 OCI 结果严格为 `linux/amd64`。

### 任务 7：配置 Linux GitHub Actions

**文件：**
- 创建：`.github/workflows/runtime-spike.yml`

- [ ] Workflow 固定 Ubuntu runner，先执行 Go 测试，再构建 Guest Runtime。
- [ ] QEMU Smoke Test 强制 TCG，不使用 `/dev/kvm`。
- [ ] 上传 Runtime Artifact、checksums、OCI Archive、串口日志和性能记录。
- [ ] 任一 SHA、BuildKit worker、Dockerfile、OCI 平台或 85 MB 体积门失败时 Workflow 整体失败。
- [ ] Workflow 只构建 Sealbuild 自身 Runtime，不接受用户镜像构建请求，不构成远程 builder。

### 任务 8：完成里程碑验收

**文件：**
- 创建：`docs/runtime-spike-results.md`
- 修改：`README.md`

- [ ] 本地运行 `gofmt -l ./cmd ./internal ./scripts/runtime`、`go vet ./...`、`go test ./...`。
- [ ] Linux CI 成功生成并验证 Guest Artifact。
- [ ] 在 Mac ARM 使用 QEMU v11.0.2 纯 TCG 执行同一 Smoke Test，记录冷启动、首次构建、缓存构建和压缩体积。
- [ ] 将真实测量值写入结果文档；任何未执行项明确标为未验证，不得宣称对应平台已支持。
- [ ] 把性能与体积结果提交项目所有者确认，通过后才进入里程碑 2。
