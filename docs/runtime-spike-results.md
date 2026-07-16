# Guest Runtime Spike Results

## Status

里程碑 1 已完成 Runtime 定义、严格版本锁、Buildroot External Tree、Guest 启动配置、mTLS、QEMU TCG Smoke Test 和 Linux GitHub Actions 的基础实现。

当前结论不是「Guest Runtime 已通过」。项目尚未初始化 Git，也没有 GitHub Remote，因此 Linux Workflow 还没有实际运行；当前 Mac ARM 也没有安装 `qemu-system-x86_64`，无法在本机执行端到端 Smoke Test。

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

## Required Linux CI evidence

以下项目尚未验证，必须由 `.github/workflows/runtime-spike.yml` 真实运行产生：

- Buildroot 完整构建成功。
- QEMU v11.0.2 Linux binary 构建成功。
- Guest rootfs 启动且输出 `SEALBUILD_RUNTIME_READY`。
- BuildKit 只报告一个 `linux/amd64` worker。
- Smoke Dockerfile 的联网 `RUN`、`COPY` 和多阶段构建成功。
- 首次构建和缓存构建均导出 OCI Archive。
- 两个 OCI Archive 的平台检查严格通过 `linux/amd64`。
- Guest Runtime 压缩体积不超过 85 MiB。

## Required Mac ARM evidence

Linux CI 通过后，必须在 Mac ARM 使用同一 Guest Artifact 和 QEMU v11.0.2 纯 TCG 记录：

| Measurement | Result |
| --- | --- |
| Cold boot | 未验证 |
| First build | 未验证 |
| Cached build | 未验证 |
| Compressed Guest Runtime | 未验证 |

上述四项获得真实值并经项目所有者确认前，不进入里程碑 2，也不宣称 Sealbuild 已具备用户可用的镜像构建能力。
