# Sealbuild 本地 OCI 构建设计

## 目标

在 Apple Silicon Mac 上完成真实的 `sealbuild build`：Sealbuild 不调用 Docker Engine、Docker Desktop API 或远程构建服务，自行启动一次性 QEMU TCG Guest，通过 BuildKit v0.31.1 构建固定平台 `linux/amd64`，并原子输出经过平台校验的 OCI Archive。

Docker 仅作为开发期 Linux 编译环境，用于在本机生成新版 Guest Runtime。最终 `sealbuild` CLI 的构建路径不依赖 Docker。

## 本轮范围

实现：

- 使用本机 Docker 的 `linux/amd64` 容器可复现构建新版 Guest Runtime。
- BuildKit mTLS Ready Probe，并验证唯一 OCI worker 支持基线 `linux/amd64`，且不暴露 ARM worker。
- 本地 Dockerfile Context、Dockerfile 路径和标准 `--build-arg` 传输。
- 显式 `--proxy`，同时用于 Guest BuildKit 网络请求和 Dockerfile 标准代理 build args。
- 固定 `linux/amd64` BuildKit Solve。
- 原子导出本地 OCI Archive。
- OCI Archive 平台严格校验。
- 纯文本构建进度、Context 取消、错误分层和 VM 清理。
- 同一持久 BuildKit 状态盘的顺序缓存复用。
- Apple Silicon Mac 上首次构建和缓存构建的真实验收。

不实现：

- Registry 推送和 Registry 凭据。
- Docker daemon 导入或 `docker load`。
- 多平台输出、ARM 镜像或 Windows 镜像。
- 多个并发 VM 共享状态盘。
- 常驻 VM、远程 builder、自动重试、镜像源切换或任何 fallback。
- 四宿主最终发布和单文件 Release 打包；本轮只为最终嵌入保留明确资产接口并完成 Darwin ARM 本地开发产物。

## Runtime 生成

新增开发脚本在固定 `linux/amd64` 容器中执行 Linux Guest 构建。容器只挂载 Sealbuild 源码、固定 Buildroot checkout、固定 QEMU checkout、下载缓存和输出目录。

容器内执行以下固定流程：

1. 安装构建依赖。
2. checkout Buildroot Commit `cb857ba4c87a93e5265a9e4a3f32071abf39e14a`。
3. 下载 QEMU v11.0.2 官方 release archive并校验 SHA-256 `3745f6ea88e2e87fe0dc838b2b1d4e0a770bf48e01a1d5a186842a1fff76ccf5`。
4. 构建 QEMU v11.0.2 `qemu-img`。
5. 在每次独立的大小写敏感 Docker volume 中执行现有 `scripts/runtime/build-guest.sh`，避免 macOS 文件系统折叠 Linux UAPI 中仅大小写不同的路径。
6. 生成 qcow2 状态盘模板、Guest Artifact、Manifest、checksums 和许可证。
7. 构建成功后只把 Guest archive 与 `artifact/` 写回宿主 `out/`，删除临时 Docker volume，不写入 Git。

不使用宿主 Docker 构建用户镜像，不读取 Docker daemon 中的镜像缓存，也不把 Docker socket 传入 Sealbuild。

## Runtime 资产接入

本地开发构建从明确指定的 Host Artifact 和 Guest Artifact 生成 Go 可嵌入资产。生成物位于被 Git 忽略的目录；缺少任一资产、SHA 不匹配或 Manifest 不合法时，CLI 构建立即失败。

Runtime 安装继续使用现有 `runtime.Bundle`、Compatibility ID、严格解压、安装级 mTLS、原子目录发布和持久 qcow2 状态盘。不会在线下载 Runtime，也不会回退到旧 raw-ext4 Guest。

## BuildKit 客户端

新增 `internal/build`，直接使用 BuildKit v0.31.1 官方 Go Client：

- `Probe` 使用安装级 CA、Client Certificate 和 Client Key 连接 `tcp://127.0.0.1:<port>`。
- TLS Server Name 固定为 `sealbuild-runtime`。
- Probe 调用 Worker API，要求存在且只存在一个 OCI worker。
- worker 必须包含基线 `linux/amd64`；出现 ARM、Windows 或其他 OS/Architecture 立即失败。
- Probe 错误交给现有 VM ticker 重试同一 VM；不会重启 QEMU 或重新分配端口。

Solve 固定使用 `dockerfile.v0` frontend：

- Context 与 Dockerfile 通过 BuildKit session 本地目录传输。
- frontend platform 固定为 `linux/amd64`。
- Dockerfile 默认是 Context 根目录的 `Dockerfile`；显式路径必须是 Context 内普通文件。
- build args 使用 `build-arg:<name>` frontend attr。
- 配置代理时，把 Guest 可见代理地址作为 `HTTP_PROXY`、`HTTPS_PROXY`、`http_proxy` 和 `https_proxy` build args；用户显式同名 build arg 冲突时返回参数错误，不覆盖。
- 输出 exporter 固定为 `oci`，不实现 image push exporter。

## 本地输出

`--output` 必须是本地 OCI Archive 文件路径：

1. 校验父目录存在且输出目标不存在。
2. 在同一父目录创建权限为 `0600` 的临时文件。
3. BuildKit OCI exporter 只写临时文件。
4. Solve 成功后关闭并 Sync 文件。
5. 解析 tar 中的 `index.json`、OCI Manifest 和 Config，要求唯一镜像平台严格为 `linux/amd64`。
6. 校验成功后原子 rename 到目标路径并 Sync 父目录。
7. 失败、取消或校验失败时删除临时文件，绝不留下半成品。

本轮不覆盖已有输出文件，避免静默删除用户数据。

## CLI

首个可用命令：

```text
sealbuild build [--dockerfile PATH] [--build-arg NAME=VALUE]... [--proxy URL] --output PATH CONTEXT
```

约束：

- Context 必须是存在的本地目录。
- `--output` 必填。
- 镜像目标不开放平台参数，始终为 `linux/amd64`。
- `--proxy` 只接受现有代理包支持的显式 HTTP/HTTPS URL。
- 公共 Registry 拉取使用 Sealbuild 自己的匿名 BuildKit auth session；宿主获取 Registry Token 时使用 Proxy 的原始宿主地址，Guest 拉取 Manifest、Layer 和执行 Dockerfile 网络请求时使用转换后的 Guest 地址。
- 匿名 auth session 不读取 Docker config、系统 keychain 或 Registry 凭据；需要私有镜像认证时明确报错，凭据支持留给 Registry 阶段。
- 不读取 Docker 配置、Docker socket、系统代理或 Registry 凭据。
- 正常进度写 stdout，错误写 stderr；不打印证书、私钥或代理 URL 内容。

## 生命周期与缓存

每次 `sealbuild build` 都启动一个全新的 QEMU 进程和 Guest：

1. 安装或复验 Runtime。
2. 获取持久状态盘独占锁。
3. 创建本次串口日志和可选代理临时文件。
4. 分配一次回环端口并启动一次 QEMU。
5. 等待 BuildKit Ready。
6. 执行一次 Solve 并输出 OCI Archive。
7. 无论成功、失败或取消，都通过本机 Unix socket 与 QEMU virtio-serial 请求 Guest 停止 BuildKit、同步并卸载状态盘，再正常关闭本次 VM；协议失败会返回错误并执行进程清理。
8. 保留 qcow2 状态盘供下一次独立 VM 顺序复用。

第二个并发构建遇到同一状态盘锁时立即失败，不等待、不创建第二个状态盘，也不共享正在运行的 VM。

## 错误处理

参数、Runtime 安装、VM 启动、BuildKit Ready、Solve、OCI 输出和清理错误保留独立阶段上下文。主错误与关闭、文件删除、锁释放错误使用 `errors.Join` 同时返回。

不添加以下行为：

- 自动重试 Solve。
- 自动更换端口。
- 自动重启 QEMU。
- 自动切换 accelerator。
- 自动读取环境代理。
- 自动改用 Docker Engine。
- 自动保留或改用旧 Guest Runtime。

## 测试与验收

单元测试：

- Build 参数、Context 边界、Dockerfile 路径和 build args。
- BuildKit worker 平台语义。
- mTLS Client 配置。
- OCI exporter 原子文件语义。
- OCI Archive 缺文件、损坏摘要、多 Manifest、ARM 平台和非 AMD64 Config 拒绝。
- CLI 参数、退出码和错误输出。
- VM 成功、取消、超时、早退、串口失败和并发缓存锁。

本地真实验收：

1. 在 Docker `linux/amd64` 容器中生成新版 Guest Runtime。
2. 使用自包含 Darwin ARM QEMU Host Runtime。
3. 构建包含 `FROM`、`COPY`、联网 `RUN` 和多阶段构建的固定测试 Context。
4. 显式使用 `http://127.0.0.1:7890`，Guest 中转换为 `10.0.2.2:7890`。
5. 第一次 `sealbuild build` 成功输出 OCI Archive。
6. 校验 OCI Archive 唯一平台严格为 `linux/amd64`。
7. 第二次启动全新 VM 构建同一 Context，成功复用持久 BuildKit 缓存。
8. 两次构建后均确认 QEMU 进程退出。
9. 记录 Runtime 大小、CLI 大小、冷启动、首次构建和缓存构建耗时。

只有上述真实验收全部通过，才能宣称 Darwin ARM 本地 OCI 构建可用。其他宿主在各自真实端到端验收前仍不得宣称支持。
