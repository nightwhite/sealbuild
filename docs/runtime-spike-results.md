# Guest Runtime Spike Results

## Status

里程碑 1 已完成 Runtime 定义、严格版本锁、Buildroot External Tree、Guest 启动配置、mTLS、QEMU TCG Smoke Test 和 Linux GitHub Actions 的基础实现。

Linux Runtime Spike 已在 GitHub Actions 端到端通过。当前 Mac ARM 没有可用的 `qemu-system-x86_64`，因此 Mac ARM 纯 TCG 性能门仍未验证，里程碑 1 尚未最终关闭。

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

## Required Mac ARM evidence

Linux CI 通过后，必须在 Mac ARM 使用同一 Guest Artifact 和 QEMU v11.0.2 纯 TCG 记录：

| Measurement | Result |
| --- | --- |
| Cold boot | 未验证 |
| First build | 未验证 |
| Cached build | 未验证 |
| Compressed Guest Runtime | 未验证 |

上述四项获得真实值并经项目所有者确认前，不进入里程碑 2，也不宣称 Sealbuild 已具备用户可用的镜像构建能力。
