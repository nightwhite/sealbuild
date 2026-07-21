# Windows AMD64 单文件产品设计

## 目标

为 Windows 10 Home 22H2 x64、Windows 11 Home x64、Windows Server 2022 x64 和 Windows Server 2025 x64 生成单文件 `sealbuild-windows-amd64.exe`。用户以普通权限直接运行该文件，在本机读取标准 Dockerfile Context，并固定构建 `linux/amd64` OCI 镜像。

用户不安装 Docker Desktop、Docker Engine、WSL、Hyper-V、WHPX、QEMU、MSYS2、MinGW、驱动、服务或其他第三方组件。产品不使用远程 builder，不联网下载 Runtime，也不自动切换到宿主已有工具。

## 范围

本阶段包含：

- Windows AMD64 Host Runtime 的可复现构建、依赖收集、许可证收集、Manifest 和压缩打包。
- Windows 原生 `sealbuild-windows-amd64.exe` 的单文件嵌入和运行。
- Windows 文件锁、QEMU 进程生命周期、正常关机通道、路径编码和缓存目录。
- GitHub Actions Windows Runner 上的单元测试、Runtime 构建、产品打包和真实 Dockerfile 双构建验收。
- Windows 发布产物、SHA-256 和小于 150 MiB 的体积门禁。

本阶段不包含：

- Registry Push、Registry 登录或凭据管理。
- Windows ARM64 宿主或任何非 `linux/amd64` 镜像目标。
- WSL、Hyper-V、WHPX 或其他硬件虚拟化路径。
- Windows 7、Windows 8、32 位 Windows 或停止维护的早期 Windows 10。
- GUI、安装器、系统服务、驱动、PATH 修改、注册表修改和管理员权限操作。
- 从 Git URL 构建 Context；本阶段继续使用本地目录 Context。

## 产品产物

唯一用户产物为：

```text
sealbuild-windows-amd64.exe
```

该 PE 文件使用 Go `embed` 内嵌：

```text
Windows Host Runtime
├── bin/qemu-system-x86_64.exe
├── bin/*.dll
├── share/qemu/*
├── licenses/*
├── manifest.json
└── checksums.txt

Linux AMD64 Guest Runtime
├── bzImage
├── rootfs.ext4
├── buildkit-state.qcow2
├── licenses/*
├── manifest.json
└── checksums.txt
```

MSYS2、Clang、LLD 和 MinGW-w64 只存在于 GitHub Actions 的构建环境，不进入用户系统，也不作为产品运行依赖。

## Windows Host Runtime

### 固定版本与构建环境

- QEMU 固定为 v11.0.2，源码 Revision 固定为 `e545d8bb9d63e9dd61542b88463183314cff9482`。
- GitHub Actions 使用 `windows-2025` Runner。
- QEMU 使用 MSYS2 CLANG64 环境构建。Clang 和 LLD 生成使用 MinGW-w64 Windows ABI 的原生 x86-64 PE 文件。
- 构建依赖和 QEMU 配置必须显式固定；不使用 `latest` 包含义生成发布产物。

### QEMU 功能范围

QEMU 仅构建 Sealbuild 需要的能力：

- `x86_64-softmmu`
- TCG 多线程软件模拟
- q35 PC machine
- virtio block、virtio net 和 virtio serial
- user-mode networking 与 host forwarding
- qcow2
- 串口文件日志
- 必要 PC BIOS 和 option ROM

明确禁用 GUI、音频、显示、guest agent、文档、user-mode emulation、WHPX 和其他非产品功能。`qemu-system-x86_64.exe -accel help` 的发布验收结果必须只包含 `tcg`。

### PE 依赖闭包

Windows Host Runtime 打包器使用 Go 标准库 `debug/pe` 递归读取 QEMU EXE 和 DLL 的 Import Table。

- Windows 系统 DLL 由固定 allowlist 识别，不复制进 Runtime。
- 非系统 DLL 必须从固定 MSYS2 CLANG64 构建环境解析并复制到 `bin/`。
- 每个复制的 DLL 继续递归检查 Import Table。
- DLL 缺失、同名冲突、架构不是 AMD64、路径逃逸或依赖闭包不完整时立即失败。
- Manifest 和 checksums 覆盖所有 EXE、DLL、固件和许可证。
- 打包后在不包含 MSYS2 路径的环境中执行 QEMU 版本与 accelerator 检查。

产品不在运行时搜索系统 PATH、MSYS2、Chocolatey、Scoop、Docker Desktop 或其他 QEMU 安装位置。

## Runtime 安装与缓存

`os.UserCacheDir()` 在 Windows 上解析到当前用户的本地缓存目录，Sealbuild 在其下使用：

```text
%LOCALAPPDATA%\sealbuild\
├── runtime\<compatibility-id>\
├── state\<compatibility-id>\buildkit-state.qcow2
├── locks\
└── logs\
```

首次运行从当前 `.exe` 解压 Host 和 Guest Runtime 到临时目录，完成 Manifest、文件大小和 SHA-256 验证后原子发布到内容版本目录。运行路径不联网下载 Runtime，不读取系统已有 QEMU。

每次 `sealbuild build` 创建新的 QEMU VM；同一 compatibility ID 的 qcow2 状态盘可以在不同构建间顺序复用，但通过 Windows 原生文件锁禁止并发挂载。

## Windows 平台边界

### 文件系统发布与权限

当前 Darwin 实现使用目录 `Sync`、硬链接发布和 Unix 权限位校验。Windows 对这些操作的支持和安全语义不同，因此 Runtime 安装、TLS、状态盘和 OCI 输出通过窄平台函数隔离：

- Windows 在同一卷内使用不覆盖既有目标的原子重命名发布临时文件或目录。
- 目标已存在时返回明确冲突，不删除或覆盖目标。
- Windows 不把 POSIX `0600`/`0644` 数字位当作访问控制证据；私有文件必须位于当前用户专属的 `%LOCALAPPDATA%` Runtime 树，且文件必须是普通文件、不可写保护异常、内容与 SHA-256 正确。
- Darwin 和 Linux 保留现有权限位与目录 `Sync` 约束，不因 Windows 降低 Unix 安全检查。
- Windows 平台测试在包含空格的临时目录验证 Runtime、TLS、状态盘和 OCI 原子发布。

### 文件锁

新增 `internal/lockfile/lock_windows.go`，使用 Windows `LockFileEx` 非阻塞申请独占锁，使用 `UnlockFileEx` 释放。竞争继续映射到现有 `ErrContended`，不通过错误文本判断状态。

测试必须证明：

- 首次加锁成功。
- 同进程第二次加锁返回 `ErrContended`。
- 关闭后可以重新加锁。
- 重复关闭安全。
- 子进程不继承锁 Handle。

### QEMU 进程

新增 `internal/vm/process_windows.go`：

- 使用 `CREATE_NO_WINDOW` 启动 QEMU，不弹出额外控制台窗口。
- 创建 Windows Job Object，并把 QEMU 分配进去。
- Job Object 设置 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`，Sealbuild 异常退出时由 Windows 回收 QEMU 进程树。
- 正常构建先走 Guest 关机协议；只有就绪失败、取消、关机超时或 QEMU 异常时才终止 Job。
- `Wait` 负责回收进程和关闭 Job Handle，成功、失败和重复清理都不能泄漏 Handle。

不使用信号模拟 Unix 生命周期，不调用 `taskkill.exe`，不依赖 PowerShell 清理产品进程。

### 正常关机通道

macOS 当前使用 Unix Socket 连接 QEMU virtio-serial chardev。Windows 没有同等的 Unix Socket 路径语义，因此 Windows 使用第二个随机回环 TCP 端口：

```text
-chardev socket,id=shutdown,host=127.0.0.1,port=<random>,server=on,wait=off
-device virtserialport,chardev=shutdown,name=org.sealbuild.shutdown
```

Host 连接 `127.0.0.1:<shutdown-port>` 发送现有 shutdown 请求并等待 Guest acknowledgement。端口只绑定回环地址，启动前使用现有端口分配边界申请，QEMU 启动后由进程独占。

Guest 收到请求后继续执行：停止 BuildKit、同步状态盘、卸载状态盘、返回 acknowledgement、关机。关机失败不得视为成功。

### 路径与参数

- 所有宿主路径继续使用 `filepath`。
- QEMU compound options 中的文件路径必须按 QEMU 规则转义逗号。
- 盘符、反斜杠和空格不通过 Shell 解释；Go 直接使用 `exec.Cmd` 参数数组启动 QEMU。
- Runtime、状态盘、TLS、代理文件、串口日志和 OCI 输出必须在包含空格的 Windows 用户目录测试。
- 不把代理 URL、TLS 私钥或 Registry 凭据写入命令行日志。

## VM 配置

Windows 首版与已验收 Darwin ARM 保持一致：

```text
accelerator: tcg,thread=multi
machine: q35
CPU: max
vCPU: 4
memory: 4096 MiB
Guest rootfs: read-only raw ext4
BuildKit state: persistent qcow2
network: QEMU user-mode networking
```

不探测 WHPX，不提供自动加速，不在 TCG 启动失败时切换其他执行方式。

## Go Runtime 资产选择

Runtime 嵌入按 Go build tags 分离：

- `bundle_embedded_darwin_arm64.go`
- `bundle_embedded_windows_amd64.go`
- `bundle_stub.go`

Windows tagged bundle 的 Host Manifest 必须是 `windows/amd64`，Guest Manifest 必须是 `linux/amd64`。平台不匹配时 CLI 在启动 QEMU 前失败。

Windows CLI 使用：

```text
GOOS=windows GOARCH=amd64 CGO_ENABLED=0
```

Go CLI 本身不依赖 MinGW DLL；只有内嵌 QEMU Host Runtime 带自己的完整 DLL 闭包。

## GitHub Actions

### Windows Runtime Job

`build-windows-runtime` 在 `windows-2025` 上执行：

1. Checkout Sealbuild 与固定 QEMU Revision。
2. 安装固定 MSYS2 CLANG64 工具链和构建依赖。
3. 使用最小配置构建 `qemu-system-x86_64.exe`。
4. 运行版本、PE AMD64 和 TCG-only 检查。
5. 递归收集 DLL、固件和许可证。
6. 生成 Windows Host Runtime tar.zst、Manifest 和 checksums。
7. 上传不可变的中间 Artifact，供产品 Job 使用。

### Windows Product Job

`test-windows-product` 在新的 `windows-2025` Job 中执行，不能继承 Runtime Job 的 MSYS2 进程环境：

1. 下载 Windows Host Runtime 和固定 Linux AMD64 Guest Runtime。
2. 验证两个 Runtime Artifact。
3. 生成 Windows build-tag 对应的 embed 资产。
4. 使用 `CGO_ENABLED=0` 构建 `sealbuild-windows-amd64.exe`。
5. 清理 PATH 中的 MSYS2 项，在包含空格的工作目录运行产品。
6. 第一次构建本地多阶段 Dockerfile，输出 OCI Archive。
7. 验证 OCI 唯一平台严格为 `linux/amd64`。
8. 确认第一次构建后没有 QEMU 进程和锁残留。
9. 使用同一缓存盘启动新的 VM 完成第二次构建。
10. 验证 Dockerfile 执行步骤命中 `CACHED`，再次验证 OCI。
11. 确认第二次构建后没有 QEMU 进程、监听端口和锁残留。
12. 验证单文件 `.exe` 小于 150 MiB 并生成 SHA-256。
13. 上传 `.exe`、checksums、OCI 验证结果、串口日志和缓存证据。

### 候选产物与正式 Release 门禁

Windows 阶段的 Actions 工作流上传候选 Artifact：

```text
sealbuild-windows-amd64.exe
checksums.txt
```

该 Artifact 用于 CI/CD 验收和 Windows Home 实机测试，不单独创建不完整 GitHub Release。

正式 Git Tag Release 继续遵守仓库四平台门禁：只有 Darwin ARM64、Darwin AMD64、Linux AMD64、Windows AMD64 和统一 checksums 全部存在且各自验收成功后才允许创建。缺少任一平台产物、SHA-256、Manifest、许可证、端到端结果或体积门禁时终止 Release。同一 Tag 不覆盖已有产物。

## 错误与诊断

Windows 用户错误保持与 Darwin 相同的阶段语义：Runtime 安装、状态锁、QEMU 启动、BuildKit 就绪、Dockerfile Solve、OCI 输出和 Guest 关机错误分别保留原始原因。

Windows 专用错误必须包含可行动阶段：

- QEMU PE 或 DLL 不完整。
- Job Object 创建、配置或分配失败。
- `LockFileEx` 失败或锁竞争。
- Windows 路径无法编码为 QEMU 参数。
- shutdown 回环端口无法申请、连接或确认。

失败时保留串口日志路径，但不打印代理凭据、TLS 私钥或敏感环境变量。禁止通过下载另一套 QEMU、调用 Docker、启用 WHPX 或改用远程 builder 兜底。

## 验收标准

只有以下证据全部存在，才能宣称 Windows AMD64 可用：

- Windows 平台 Go 单元测试通过。
- Windows Host Runtime 从固定 QEMU Revision 构建并通过 SHA-256、PE、DLL、固件和许可证校验。
- `qemu-system-x86_64.exe -accel help` 只包含 TCG。
- 单文件 `sealbuild-windows-amd64.exe` 在无 MSYS2 PATH、无 Docker、无 WSL、无 Hyper-V、无 WHPX 的 Windows Runner 上运行。
- 两次构建都启动独立 QEMU VM，并输出严格 `linux/amd64` OCI Archive。
- 第二次构建复用缓存盘并存在明确 `CACHED` 证据。
- 每次构建后没有 QEMU 进程、锁、监听端口和临时文件残留。
- `.exe` 小于 150 MiB，SHA-256 已生成。
- Windows Home 真实设备使用普通用户权限完成至少一次相同构建；GitHub Actions 成功不能替代最终 Windows Home 实机验收。

在 Windows Home 实机验收完成前，只能宣称 Windows CI 产物可构建和可测试，不能宣称 Windows Home 已正式支持。
