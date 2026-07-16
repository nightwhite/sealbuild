# Reference 项目索引

`reference/` 用于存放 Sealbuild 研究和实现直接依赖的上游参考仓库。参考仓库保持独立 Git 历史，只用于阅读、验证和对照，不作为 Sealbuild 产品源码提交。

新增参考项目时，在本文件记录：

- 项目名称和本地目录。
- 上游仓库 URL。
- 固定 Commit SHA。
- 引入目的和重点研究范围。
- 对 Sealbuild 设计或实现产生的明确结论。

## 当前项目

### BuildKit

- 本地目录：`reference/buildkit`
- 上游仓库：`https://github.com/moby/buildkit.git`
- 固定版本：`v0.31.1`
- Commit：`673b7e0196de0cac83308274b88aaed97a91af74`
- 引入目的：Sealbuild 的核心镜像构建引擎和 Go Client 来源。
- 重点范围：BuildKit session、Dockerfile frontend、OCI worker、context 传输、Registry 认证、构建进度、OCI 输出和缓存生命周期。

### Buildx

- 本地目录：`reference/buildx`
- 上游仓库：`https://github.com/docker/buildx.git`
- 固定版本：`v0.35.0`
- Commit：`a319e5b15052cf6557ceb666eb8ff6e32380b782`
- 引入目的：参考成熟的 BuildKit CLI 编排、参数语义和用户交互，不作为 Sealbuild 运行时依赖。
- 重点范围：build 命令、进度输出、Registry auth、构建参数、OCI exporter 和错误呈现。

### QEMU

- 本地目录：`reference/qemu`
- 上游仓库：`https://gitlab.com/qemu-project/qemu.git`
- 固定版本：`v11.0.2`
- Commit：`e545d8bb9d63e9dd61542b88463183314cff9482`
- 引入目的：为四种宿主构建精简的 `qemu-system-x86_64` 软件模拟器。
- 重点范围：TCG、headless 启动、用户态网络、回环端口转发、虚拟磁盘、进程退出和最小编译配置。

### runc

- 本地目录：`reference/runc`
- 上游仓库：`https://github.com/opencontainers/runc.git`
- 固定版本：`v1.5.1`
- Commit：`8f2685a471d3347a686ad3909783d8aafc6bb208`
- 引入目的：Guest Runtime 内 BuildKit OCI worker 使用的 OCI Runtime。
- 重点范围：静态构建、运行依赖、Kernel 能力、namespace、cgroup 和 BuildKit OCI worker 集成条件。

### Buildroot

- 本地目录：`reference/buildroot`
- 上游仓库：`https://gitlab.com/buildroot.org/buildroot.git`
- 固定版本：`2026.05.1`
- Commit：`cb857ba4c87a93e5265a9e4a3f32071abf39e14a`
- 引入目的：可复现地构建最小 `linux/amd64` Kernel、rootfs 和 initramfs。
- 重点范围：x86-64 QEMU defconfig、BusyBox、网络、CA 证书、OverlayFS、cgroup、BuildKit 和 `runc` 集成。

## 使用约束

- 上述仓库均为浅克隆，并固定在表述的 Tag 和 Commit。
- 不在 Reference 仓库中开发 Sealbuild 功能，不提交对上游源码的本地修改。
- 需要验证上游改动时，在独立实验分支或临时副本中进行，不污染固定参考基线。
- 升级版本前必须说明升级原因、兼容性影响和发布体积变化，并同步更新本索引。
- 新增或删除参考项目必须先获得项目所有者确认。
