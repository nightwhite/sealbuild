# Sealbuild Darwin ARM 产品闭环设计

## 目标

在 Apple Silicon Mac 上交付一个自包含的 `sealbuild` CLI。用户只下载一个 Mach-O arm64 文件，即可在本机通过 QEMU 纯 TCG 构建、导出和推送严格为 `linux/amd64` 的 OCI 镜像。

本阶段必须完成真实产品路径，不再使用独立 `buildctl` 代替 Sealbuild 的宿主构建逻辑。

## 当前阶段范围

本阶段只实现和声明 `darwin/arm64` 宿主支持。

必须交付：

- `sealbuild build`：构建本地 Dockerfile Context。
- `sealbuild version`：输出 CLI 与所有内嵌 Runtime 组件版本。
- `sealbuild clean`：清理 Sealbuild 管理的 Runtime 和 BuildKit 状态。
- OCI Archive 导出。
- OCI Registry Push。
- OCI Archive 与 Registry Push 同时输出。
- 持久 BuildKit Cache。
- 显式代理配置。
- 单文件离线 Runtime 恢复。
- 普通用户权限运行。
- 最终发布文件不超过 150 MiB。

本阶段不实现或声明：

- Intel Mac、Linux 或 Windows 宿主支持。
- `linux/arm64` 或 Windows 镜像构建。
- Docker daemon Load。
- Docker API 兼容层。
- 守护进程、GUI、插件或远程 Builder。
- HVF、KVM、WHPX 或其他硬件加速路径。

## 成功标准

删除所有已解压 Runtime 和状态目录后，只保留最终 `sealbuild` 文件，以下命令必须在当前 Apple Silicon Mac 上成功：

```bash
./sealbuild build ./runtime/smoke \
  --output ./out/final-image.tar \
  --proxy http://127.0.0.1:7890
```

验收必须证明：

- 首次运行不联网下载 QEMU、Kernel、rootfs、BuildKit、runc 或其他 Runtime 组件。
- QEMU 版本固定为 v11.0.2，且只支持 TCG。
- Guest 中只有 AMD64 OCI worker。
- Dockerfile 的联网 `RUN`、`COPY`、多阶段构建和 `.dockerignore` 生效。
- OCI Archive 平台严格为 `linux/amd64`。
- 第二次相同构建命中持久缓存。
- 本地测试 Registry 能收到严格为 `linux/amd64` 的镜像。
- `--output` 与 `--push` 能在一次 Solve 中同时完成。
- `clean` 不删除 Sealbuild 管理目录之外的文件。
- Ctrl+C、Context 取消、构建失败、QEMU 异常退出和就绪超时后不残留进程或监听端口。
- 将 Homebrew 从 `PATH` 移除后，最终发布文件仍可完成构建。
- 发布文件大小不超过 150 MiB。

## 总体架构

最终发布文件由 Go CLI 和两份已压缩 Runtime 组成：

```text
sealbuild
├── Go host application
├── embedded host-runtime-darwin-arm64.tar.zst
└── embedded guest-runtime-linux-amd64.tar.zst
```

运行时数据流：

```text
Dockerfile Context
      |
      v
Sealbuild BuildKit Go Client
      |
      | mTLS over 127.0.0.1:<ephemeral-port>
      v
QEMU slirp hostfwd
      |
      v
linux/amd64 Guest BuildKit OCI worker
      |
      +--> OCI Archive
      |
      +--> OCI Registry
```

所有构建计算在用户本机完成。Registry 只接收最终镜像，不参与构建执行。

## 发布文件与 Runtime 资产

### Host Runtime

Host Runtime 包含：

```text
host-runtime/
├── bin/qemu-system-x86_64
├── lib/*.dylib
├── manifest.json
├── checksums.txt
└── licenses/
```

QEMU 构建约束：

- 上游版本固定为 v11.0.2，Commit 固定为 `e545d8bb9d63e9dd61542b88463183314cff9482`。
- 只构建 `x86_64-softmmu`。
- 显式启用 TCG 和 slirp。
- 显式关闭 HVF、Cocoa、GTK、SDL、Docs、Guest Agent、Tools 和 User Mode。
- 不维护私有设备 Kconfig。当前压缩体积已经足够小，设备级裁剪不能抵消维护和兼容风险。
- 构建完成后 strip QEMU 和可分发 dylib。
- 递归收集所有非系统 dylib。
- 使用 `install_name_tool` 把 dylib 依赖改为 `@loader_path` 相对路径。
- 重写完成后对 QEMU 和 dylib 执行 ad-hoc codesign，并使用 `codesign --verify` 验证。
- 在清空 Homebrew 环境变量后执行 `--version` 和 `-accel help` 验证。
- `-accel help` 必须只输出 `tcg`。

系统 Framework 和 `/usr/lib` 依赖不复制；Homebrew、MacPorts 和用户目录下的依赖必须全部复制到 Host Runtime。

### Guest Runtime

Guest Runtime 包含：

```text
guest-runtime/
├── bzImage
├── rootfs.ext4
├── buildkit-state.qcow2
├── manifest.lock.json
├── checksums.txt
└── licenses/
```

Guest 继续使用已验证的 Buildroot、Linux、BuildKit、runc 和 CNI 固定版本。

状态盘从 4 GiB raw sparse ext4 模板改为 32 GiB 虚拟容量的 qcow2：

- Guest 内部仍是 ext4。
- 发布文件只保存 qcow2 的实际已分配数据，不嵌入 32 GiB 零空间。
- 首次安装直接复制小型 qcow2 模板，不依赖 APFS clonefile 或 GNU sparse copy。
- 每个 Runtime 版本使用独立状态盘，禁止在不兼容 Runtime 之间静默复用。
- 同一状态盘只允许一个活跃 QEMU，第二个进程立即返回明确的锁冲突错误。

rootfs 保持只读。BuildKit 状态、CNI 状态、生成的配置和缓存全部写入状态盘。

## Runtime Manifest

Host Runtime 与 Guest Runtime 均使用 schema 固定的 JSON Manifest。Manifest 至少记录：

- Schema 版本。
- Runtime 内容版本。
- 宿主或 Guest 平台。
- 每个文件的相对路径、SHA-256、逻辑大小和可执行属性。
- QEMU、Linux、Buildroot、BuildKit、runc 和 CNI 版本。
- 上游 Commit 或发布资产来源。
- Runtime 总未压缩大小和压缩大小。

Manifest 的文件清单只覆盖 Runtime payload，不包含 Manifest 自身和 `checksums.txt`，避免循环哈希。`checksums.txt` 覆盖全部 payload 和 Manifest，但不覆盖自身。内嵌资产的整体 SHA-256 由 Go 编译期描述符记录。

Manifest 不提供旧 schema fallback。schema、平台、文件清单或 SHA-256 不匹配时立即拒绝运行。

## 内嵌与原子解压

Go 使用 `go:embed` 嵌入两份 `.tar.zst`。构建正式发布文件前，生成资产必须已经通过 Manifest 和 SHA-256 校验。

Runtime 根目录使用 `os.UserCacheDir()`：

```text
~/Library/Caches/sealbuild/
├── runtime/<content-digest>/
├── state/<runtime-compatibility-id>/buildkit-state.qcow2
├── locks/
└── logs/
```

首次解压流程：

1. 获取 Runtime 内容锁。
2. 如果最终目录存在，重新验证完成标记和 Manifest。
3. 创建同级临时目录。
4. 流式解压 Host 和 Guest Runtime，拒绝绝对路径、`..`、逃逸 symlink 和重复文件。
5. 逐文件验证类型、大小、权限和 SHA-256。
6. 生成安装级 mTLS 材料。
7. 写入完整安装 Manifest 和完成标记。
8. `fsync` 文件与目录。
9. 原子重命名为内容摘要目录。

任何步骤失败都删除本次临时目录并返回原始错误。禁止使用旧 Runtime、未验证目录或在线下载作为 fallback。

## 安装级 mTLS

Spike 中固定的共享证书不能作为产品方案。Sealbuild 在每个 Runtime 安装目录生成独立 CA、Server Certificate 和 Client Certificate：

- CA 私钥只用于生成证书，生成结束后删除。
- Server Certificate 只允许 `sealbuild-runtime`。
- Client Certificate 只允许客户端认证。
- 私钥文件权限为当前用户可读。
- 证书过期或损坏时明确报错；本阶段不自动轮换。

Guest rootfs 不保存产品级私钥。QEMU 使用 fw_cfg 把 Server Certificate、Server Key 和 CA Certificate 传入 Guest：

```text
opt/sealbuild/tls/ca.crt
opt/sealbuild/tls/server.crt
opt/sealbuild/tls/server.key
```

Guest init 从 `/sys/firmware/qemu_fw_cfg/by_name/` 读取文件，复制到状态盘上的受限目录，再启动 `buildkitd`。Kernel 必须显式启用 QEMU fw_cfg sysfs 支持。

## 显式代理

`sealbuild build` 支持：

```text
--proxy http://127.0.0.1:7890
```

不配置 `--proxy` 时只执行直连。Sealbuild 不读取并猜测系统代理，不自动切换代理，不在失败后重试其他地址。

本阶段只接受 `http` 或 `https` Proxy URL，并拒绝 userinfo、query 和 fragment。需要用户名、密码、PAC 或 SOCKS 的代理属于后续独立设计，不能把凭据放入 CLI 参数。

配置代理时：

- 如果代理主机是 `127.0.0.1`、`localhost` 或 `::1`，传给 Guest 的地址只把主机替换为 QEMU slirp 网关 `10.0.2.2`，端口保持不变。
- 非回环代理地址原样传给 Guest。
- 代理值写入权限为 `0600` 的临时文件，通过 QEMU fw_cfg 传入 Guest，不出现在 QEMU 命令行和日志中。
- Guest init 把代理设置为 `buildkitd` 环境；Registry 元数据、Token、Layer 和 Dockerfile `RUN` 网络请求均通过 Guest 代理。
- BuildKit session 认证只从宿主读取凭据，不在宿主发起 Registry HTTP 请求。
- Dockerfile 标准 proxy build args 使用转换后的 Guest 代理地址。
- 错误和进度输出必须清除代理 URL 中的用户名、密码和查询参数。

不支持 PAC、自动代理发现或多个代理地址。

## VM 生命周期

每次 `build` 启动独立 QEMU 进程并复用持久状态盘。

启动流程：

1. 获取状态盘独占锁。
2. 校验 Runtime、状态盘和 mTLS 材料。
3. 在 `127.0.0.1:0` 获取一个临时可用端口，关闭探测 Listener 后立即启动 QEMU。
4. QEMU 使用 `-accel tcg,thread=multi`、`q35`、2 个 vCPU、2 GiB 内存、只读 rootfs 和可写 qcow2 状态盘。
5. `hostfwd` 只绑定 `127.0.0.1`。
6. 串口写入本次构建日志文件。
7. 同时监控 QEMU 进程、串口失败标记和 mTLS BuildKit Info 探测。
8. 只有 BuildKit Info 成功并确认唯一 AMD64 worker后，VM 才进入 Ready。

端口在探测 Listener 关闭到 QEMU 绑定之间存在有限竞争窗口。发生绑定失败时，本次启动明确失败，不重新选择端口。

关闭流程：

- 正常构建结束后向 QEMU 发送 `SIGTERM`。
- 在固定关闭期限内等待进程退出。
- 到期后执行明确记录的 `SIGKILL` 清理，这属于进程生命周期终止，不切换构建实现。
- 等待进程回收后再释放状态盘锁和临时文件。
- 清理错误与主错误使用 `errors.Join` 同时报告。

Go 单元测试通过窄进程接口验证生命周期，不依赖真实 QEMU；端到端测试使用内嵌 Runtime。

## BuildKit Go Client

宿主直接使用 BuildKit v0.31.1 官方 Go Client：

- mTLS 连接 QEMU 映射端口。
- filesync session 传输 Context 和 Dockerfile。
- Dockerfile frontend 固定 `platform=linux/amd64`。
- 用户不能传入其他平台。
- 使用 BuildKit 官方进度 UI 输出纯文本进度。
- Registry 凭据通过 BuildKit session auth provider 传递。
- 不执行宿主 `buildctl`，不实现 BuildKit gRPC 私有协议。

构建输入：

- Context，默认当前目录。
- Dockerfile，默认 Context 下的 `Dockerfile`。
- 零个或多个 `--build-arg KEY=VALUE`。
- 可选 `--tag`。
- 可选 `--output`。
- 可选 `--push`。
- 可选 `--proxy`。
- 可选 `--registry-insecure`。

参数约束：

- `--output` 和 `--push` 至少提供一个。
- `--push` 必须提供合法完整镜像引用。
- `--registry-insecure` 只允许与 `--push` 同时使用，并明确把目标 Registry 配置为 HTTP 或跳过 TLS 校验；Sealbuild 不根据连接错误自动启用该参数。
- `--output` 已存在时拒绝覆盖，本阶段不增加隐式覆盖。
- Dockerfile 必须位于 Context 内，禁止通过路径逃逸读取任意文件。
- Build Args 保持用户顺序，重复 Key 明确报错。
- 不接受 `--platform` 参数。

输出：

- OCI Archive 使用 BuildKit OCI exporter。
- Registry Push 使用 image exporter 和 `push=true`。
- 同时指定时在一次 Solve 中配置两个 exporter，任一 exporter 失败则整个命令失败。
- OCI Archive 先写临时文件，Solve 成功且平台检查通过后原子移动到用户路径。

## Registry 凭据与 Push

Registry 凭据读取顺序只有一个确定来源：

1. 如果设置 `DOCKER_CONFIG`，读取该目录的 `config.json`。
2. 否则读取 `~/.docker/config.json`。
3. 文件不存在表示没有凭据，不尝试其他凭据存储位置。

支持 Docker config 中的标准 `auths` 和 Docker credential helper。helper 执行失败直接返回错误，不回退到其他凭据。

Sealbuild 不直接使用 BuildKit 默认的 Docker Auth Provider。该 Provider 的 Token HTTP Client 没有可注入的 Proxy Transport，只能读取进程全局代理环境；运行时修改全局环境违反本项目并发和状态约束。

Sealbuild 实现一个只负责 `Credentials` 的窄 session attachable：

- 使用 BuildKit 官方 `session/auth` protobuf 和 gRPC 注册接口，不定义私有协议。
- `Credentials` 从上述唯一 Docker config 来源读取用户名、密码或 Identity Token。
- `FetchToken`、`GetTokenAuthority` 和 `VerifyTokenAuthority` 返回 `Unimplemented`。
- BuildKit daemon 因此使用官方既有路径在 Guest 内获取 Registry Token，并通过 Guest 的显式代理访问 Token Service。
- Sealbuild 不复制或改写 OAuth、Bearer Token、Scope 和 Challenge 处理逻辑。

凭据只存在宿主内存和 BuildKit session 中，不写入 Guest、Runtime、日志或错误文本。

本地端到端测试启动临时 HTTP OCI Registry，并通过最终 CLI 的 `--registry-insecure` 显式设置 BuildKit exporter `registry.insecure=true`。普通 `--push` 失败时不会自动启用。

## 持久缓存与并发

BuildKit 状态全部保存在 qcow2 状态盘。相同 Runtime Compatibility ID 的连续构建复用同一状态盘。

并发策略：

- 本阶段每个用户只允许一个活跃构建。
- 全局构建锁被占用时立即返回冲突错误。
- 不排队、不轮询、不自动创建第二块状态盘。

缓存命中通过 BuildKit Solve 进度中的 `CACHED` 和显著缩短的重复构建时间共同验证。

## `sealbuild clean`

默认清理：

- 当前 Runtime Compatibility ID 的 BuildKit 状态盘。
- 已完成但不再被当前 CLI 引用的 Runtime 内容目录。
- 已结束构建留下的串口日志和临时目录。

不会清理：

- 当前 CLI 正在使用的 Runtime。
- 活跃构建的状态盘。
- Runtime 根目录之外的任何文件。
- 用户指定的 OCI Archive。

如果构建锁被占用，`clean` 立即失败，不终止构建。

所有删除目标必须先经过路径所有权校验：绝对路径必须位于 Sealbuild Cache Root 下，且不能等于 Cache Root、用户 Home 或文件系统根目录。

## `sealbuild version`

输出至少包含：

- Sealbuild Git Tag 或开发版本。
- Sealbuild Commit。
- 构建时间。
- Host Runtime 内容摘要。
- Guest Runtime 内容摘要。
- QEMU、Linux、Buildroot、BuildKit、runc 和 CNI 版本。
- 支持的宿主 `darwin/arm64`。
- 固定镜像目标 `linux/amd64`。

版本输出只读取编译期数据，不解压 Runtime、不启动 QEMU、不访问网络。

## 错误与退出码

退出码：

- `0`：成功。
- `2`：CLI 参数错误。
- `10`：Runtime 校验或解压错误。
- `11`：锁冲突。
- `12`：QEMU 启动、退出或就绪错误。
- `13`：BuildKit 构建错误。
- `14`：Registry 认证或 Push 错误。
- `15`：OCI 输出错误。
- `16`：清理错误。

底层错误保留阶段和 `%w` 原因，CLI 只输出可行动信息。串口日志路径可以输出，但不能打印私钥、Registry Token 或带凭据代理 URL。

## 依赖策略

生产依赖只增加完成产品路径所必需的官方或成熟包：

- BuildKit v0.31.1 Go Client 和 session 包。
- Docker CLI config 包，用于标准 Registry 凭据格式。
- containerd/OCI 平台和镜像规范包。
- `klauspost/compress/zstd`，用于内嵌 Runtime 解压。
- Go 跨进程文件锁包；选型前验证 Darwin 行为和许可证。

本地 Registry 测试可以使用只进入测试二进制的标准 OCI Registry 实现。新增依赖必须锁定版本并记录许可证。

## 测试策略

所有行为遵循 TDD，先验证失败，再写最少实现。

### 单元测试

- Manifest schema、平台、路径和 SHA-256。
- tar.zst 路径逃逸、重复文件、错误权限和中断解压。
- 原子安装和残留临时目录。
- mTLS 证书用途、权限和损坏处理。
- 代理 URL 校验、回环地址转换和敏感信息清除。
- QEMU 参数必须只有 TCG、回环 hostfwd 和固定 Guest 平台。
- VM 状态机、取消、超时、异常退出和清理错误组合。
- CLI 参数、退出码和输出通道。
- Dockerfile 路径边界和 Build Args。
- Registry config、credential helper 错误和敏感信息保护。
- `clean` 路径所有权、锁冲突和活跃 Runtime 保留。

### 集成测试

- 解压真实 Host Runtime 后在无 Homebrew `PATH` 环境执行 QEMU。
- 使用真实 Guest Runtime完成 mTLS Ready。
- 通过 Go Client 列出唯一 AMD64 worker。
- 构建 Smoke Dockerfile 并导出 OCI Archive。
- 重复构建验证缓存。
- 启动本地 Registry，验证 Push 和拉取后的 Manifest 平台。
- 同一次 Solve 同时导出 OCI Archive 和 Push。
- 删除 Runtime 目录后只用最终 CLI 离线恢复并再次构建。
- Ctrl+C 和强制结束 QEMU 后检查进程、端口、锁和临时目录。

### 完成前验证

```bash
gofmt -l ./cmd ./internal ./scripts
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/sealbuild
```

最终还必须运行项目实现后的 Darwin ARM 打包命令和单文件端到端验收命令。设计文档不提前编造尚不存在的脚本名称。

## 实现分期

为了让每个阶段都有可运行产物，Darwin ARM 产品闭环拆为 4 个实现计划：

1. Host/Guest Runtime 打包与 Manifest。
2. Runtime 安装、mTLS、锁和 VM 生命周期。
3. BuildKit Go Client、OCI 输出和 Registry Push。
4. Cache Clean、单文件发布和最终端到端验收。

每个计划完成后运行其局部测试和全量 Go 测试。前一阶段验收失败时停止，不进入后一阶段，也不增加替代实现。
