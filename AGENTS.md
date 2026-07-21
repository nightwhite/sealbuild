# AGENTS.md

## 项目目标

Sealbuild 是一个完全本地、免安装、自包含的镜像构建 CLI。用户只需要下载对应宿主平台的单个二进制，即可构建标准 `linux/amd64` OCI 镜像，不依赖 Docker Desktop、Docker Engine、WSL、Hyper-V、WHPX、KVM 或远程构建服务。

支持的宿主平台：

- `darwin/arm64`：Apple Silicon Mac。
- `darwin/amd64`：Intel Mac。
- `linux/amd64`：x86-64 Linux。
- `windows/amd64`：x86-64 Windows，包括不启用 WSL 的 Windows Home。

唯一镜像目标是 `linux/amd64`。不要增加 `linux/arm64`、`windows/amd64` 或其他镜像目标，除非项目所有者明确修改产品范围。

## Reference 项目

与 Sealbuild 设计和实现直接相关的上游项目统一 clone 到 `reference/`，索引维护在 `reference/index.md`。

- 每个参考项目使用独立子目录，例如 `reference/buildkit`、`reference/qemu`。
- 参考仓库只用于研究、验证和对照实现，不属于 Sealbuild 产品源码。
- 不要直接修改参考仓库代码，不要把参考仓库加入 Sealbuild 的 Go Module 或发布产物。
- 引用参考实现时必须记录上游仓库 URL、固定 Commit 和用途，禁止只跟踪不稳定分支。
- 写文档或代码时必须站在 Sealbuild 自身视角描述；引用外部实现时明确写成「参考 `reference/<name>`」。
- 未经项目所有者确认，不要擅自新增、替换或删除 Reference 项目。

## 强制工作原则

- 任何编程相关工作必须先使用 `karpathy-guidelines` skills。
- 规划、分析和执行计划必须使用 Superpowers 相关技能，禁止使用 helloagents 的 skills 生成计划包。
- 多阶段开发必须先完成设计和可验证的实现计划，再进入代码实现；验收标准必须能通过具体命令客观验证，禁止使用「基本可用」「差不多」等主观描述。
- 不要擅自增加 fallback、兜底切换、自动降级、静默重试或远程构建；确实需要时，先用通俗语言向用户确认。
- 每个技术决策都要能回答「为什么」：优先从跨平台一致性、标准 Dockerfile 兼容性、免安装体验、可复现发布和运行安全出发。
- 改动要小而准，不做与当前任务无关的重构，不提前实现没有明确需求的扩展点、插件系统或兼容模式。
- 遇到跨平台问题必须追查宿主差异、QEMU 行为或 BuildKit 协议的根因，禁止用平台特判掩盖错误。

## 产品边界

- 所有构建必须在用户本机完成，禁止依赖远程 BuildKit、云构建服务或远程执行节点。
- 所有宿主统一运行 `qemu-system-x86_64` 软件模拟，不要求用户启用任何可选虚拟化功能。
- 不接入 KVM、HVF、WHPX 等硬件加速路径，也不设计从硬件加速自动降级到 TCG 的逻辑。
- 产品全程只做 CLI，不增加桌面 GUI、浏览器 UI、托盘程序或常驻桌面服务。
- 发布产物必须自包含当前宿主需要的 QEMU 和公共 `linux/amd64` Guest Runtime。
- 首次运行可以解压内嵌组件到用户缓存目录，但不能联网下载 QEMU、Kernel、rootfs、BuildKit 或 `runc`。
- 单个平台发布文件的目标体积不超过 150 MB；预计超过前必须先说明原因并获得确认。
- 构建结果必须同时支持直接推送 OCI Registry 和导出本地 OCI 镜像包。
- 不支持把镜像加载到本地 Docker daemon，因为 Sealbuild 不要求宿主安装 Docker。

## 架构边界

- `cmd/sealbuild`：只负责程序入口、版本注入和退出码，不承载构建业务逻辑。
- `internal/cli`：负责命令、参数校验和用户可读输出，不直接管理 QEMU 进程或 BuildKit gRPC 细节。
- `internal/build`：负责 BuildKit session、context 传输、构建参数、进度、Registry 推送和 OCI 输出。
- `internal/vm`：负责 QEMU 生命周期、端口分配、启动探测、正常关闭和异常清理。
- `internal/runtime`：负责内嵌资产清单、校验、原子解压和 Runtime 版本目录。
- `internal/cache`：负责 BuildKit 持久磁盘、缓存目录、锁和清理，不解析 Dockerfile。
- `internal/registry`：只封装 Registry 凭据读取和 BuildKit session 认证，不保存或打印明文凭据。
- `internal/version`：统一输出 Sealbuild、QEMU、Linux、rootfs、BuildKit 和 `runc` 版本。
- `runtime`：保存 Guest Runtime 构建定义、版本锁和校验信息，不提交未经批准的大型生成二进制。
- `scripts`：只放可复现的 Runtime 构建和发布辅助脚本，不承载 Sealbuild 运行时业务逻辑。
- `.github/workflows`：负责测试、Runtime 构建、四平台打包、校验和 GitHub Release 发布。

宿主程序使用 Go。VM 内只允许保留启动所需的最小 BusyBox Shell 脚本，不在宿主侧使用跨平台 Shell 脚本驱动核心流程。

## Guest Runtime 规范

- 所有宿主使用同一份 `linux/amd64` Guest Runtime，Mac ARM 也通过 QEMU TCG 运行 x86-64 Guest。
- Guest Runtime 只包含 Linux Kernel、最小 rootfs、`buildkitd`、`runc`、BusyBox、CA 证书和必要网络组件。
- BuildKit 使用 OCI worker，不引入完整 Docker Engine 或 containerd daemon。
- Guest 根文件系统应保持只读；BuildKit 状态和缓存写入独立的持久虚拟磁盘。
- 构建上下文通过 BuildKit session 从宿主传输，禁止依赖 9p、SMB、NFS 或宿主目录共享驱动。
- QEMU 暴露的 BuildKit 端口只能绑定宿主回环地址，不能监听局域网或公网地址。
- VM 启动必须有明确的就绪探测和超时；超时后返回可诊断错误，不静默切换其他执行方式。

## CLI 规范

首批命令只保留：

- `sealbuild build`：构建 `linux/amd64` Dockerfile context。
- `sealbuild version`：输出 CLI 和内嵌 Runtime 的完整版本。
- `sealbuild clean`：安全清理 BuildKit 缓存和过期 Runtime。

`sealbuild build` 必须支持：

- 指定 Dockerfile 和构建上下文。
- 设置镜像名称和 Tag。
- 传递标准 Dockerfile build arguments。
- 使用 `--push` 直接推送 OCI Registry。
- 使用 `--output <path>` 导出本地 OCI 镜像包。
- 输出适合本地终端和 CI 日志阅读的纯文本进度。
- 使用非零退出码表达参数错误、Runtime 错误、构建失败、推送失败和输出失败。

不要擅自增加命令、别名、守护进程模式、插件机制或 Docker 兼容 API。

## Go 代码编写规范

### 基础规则

- Go 代码必须通过 `gofmt`，禁止手工维护与 `gofmt` 冲突的排版。
- 默认使用 `CGO_ENABLED=0` 构建宿主 CLI；确实需要 CGO 时，必须先说明跨平台发布成本并获得确认。
- 优先使用 Go 标准库、BuildKit 官方 Go Client 和必要的 containerd/OCI 包，禁止重复实现已有协议。
- 不使用单字母变量名；只允许在极短循环中使用 `i`、`j` 等惯用索引。
- 命名必须表达领域含义，避免 `Manager`、`Helper`、`Utils`、`Common` 等无法说明职责的泛化名称。
- 注释只解释约束、原因和不明显的行为，不复述代码；导出标识符必须有符合 Go 约定的文档注释。
- 禁止在正常运行路径中使用 `panic`；只有进程无法继续的初始化不变量被破坏时才允许 panic，并且必须有测试覆盖。
- 禁止使用 `unsafe`、反射驱动的核心流程或运行时修改全局状态，除非没有更简单的标准方案且已获得确认。

### 包与依赖边界

- 每个包只承担一个明确职责；包名必须描述能力，不描述技术分层中的空泛概念。
- `internal/cli` 不能直接启动 QEMU、读写虚拟磁盘或调用 BuildKit gRPC，只能调用明确的应用接口。
- `internal/build` 不解析宿主平台差异，不直接管理 QEMU 进程。
- `internal/vm` 不解析 Dockerfile、不处理 Registry 凭据、不决定镜像输出格式。
- `internal/runtime` 不负责 VM 生命周期和 BuildKit 构建状态。
- 禁止循环依赖、全局 Service Locator、隐式单例和可变包级业务状态。
- 接口定义在使用方，只有存在真实替换实现或测试隔离需求时才创建接口；不要为单一实现提前抽象。
- 新增第三方依赖前必须说明标准库或现有依赖为什么不能满足，并检查许可证、维护状态和二进制体积影响。

### 函数与数据模型

- 函数只完成一个可描述的动作；当函数同时负责校验、状态变更、I/O 和输出格式化时，应按职责拆分。
- 参数表达不清或布尔参数超过一个时，使用命名配置结构体，禁止出现难以理解的 `Start(true, false)`。
- 配置结构体与运行时状态结构体分离，禁止在同一对象中混合用户输入、默认值和可变生命周期状态。
- 枚举状态使用具名类型和常量，禁止用无语义字符串在包之间传递 VM 或构建状态。
- 时间、大小、路径和平台等关键值使用明确类型或标准类型，禁止在核心流程中传递单位不明的整数。
- 不返回内部可变集合供调用者修改；切片、Map 或字节缓冲区存在所有权风险时必须复制或明确转移所有权。

### 错误处理

- 所有错误必须保留操作阶段和原始原因，使用 `%w` 包装；禁止只返回 `failed`、`unknown error` 等无上下文信息。
- 需要调用方分支处理的错误使用具名错误类型或 `errors.Is` / `errors.As`，禁止解析错误字符串。
- 参数错误、Runtime 错误、VM 错误、BuildKit 错误、构建错误、Registry 错误和输出错误必须可区分。
- 清理失败不能覆盖主错误；需要同时报告时使用 `errors.Join` 或等价的显式组合。
- 禁止忽略错误。确实可以忽略时，代码必须用简短注释说明为什么不影响正确性。
- 用户错误信息保持简洁可行动，详细诊断通过结构化上下文保留，但不能泄露凭据。

### Context、并发与生命周期

- 所有可能阻塞、访问网络、启动进程或执行构建的公共操作必须接收 `context.Context`，并把它作为第一个参数。
- 不在结构体中长期保存请求级 `context.Context`；Context 必须沿调用链显式传递。
- 每个启动的 Goroutine 都必须有明确退出条件、取消来源和等待方，禁止无法回收的后台 Goroutine。
- 并发任务优先使用 `errgroup` 或清晰的所有权模型；不要用裸 Goroutine 隐藏错误。
- Channel 由创建并负责发送的一方关闭；接收方不得关闭未知所有权的 Channel。
- 共享状态必须通过互斥锁、原子操作或单所有者事件循环保护；禁止依赖执行时序碰巧正确。
- QEMU 进程、BuildKit session、临时目录、端口占用和文件锁必须在成功、失败、取消和信号退出路径中释放。
- 禁止用固定 `sleep` 判断 VM 就绪；必须使用有超时和 Context 取消能力的显式健康检查。

### 文件系统与跨平台

- 路径必须使用 `filepath` 和系统目录 API，禁止拼接硬编码路径分隔符。
- 文件写入涉及 Runtime、Lock Manifest、状态磁盘或 OCI 输出时必须采用临时文件加原子替换，避免半成品被使用。
- 临时文件和目录使用 Go 标准库创建，不自行生成可预测名称。
- Unix 与 Windows 的进程组、信号、文件锁、重命名和权限语义不同，必须使用平台文件和 Build Tags 隔离实现。
- 宿主差异通过窄接口和 `*_windows.go`、`*_unix.go` 等平台文件隔离，禁止在业务流程中散落 `runtime.GOOS` 判断。
- 不要求宿主具备管理员或 root 权限；新增特权操作视为产品范围变化，必须先确认。
- 并发执行多个 Sealbuild 进程时，不能破坏 Runtime 目录、端口分配或 BuildKit 状态磁盘。

### 日志与 CLI 输出

- 正常构建进度写入标准输出，警告和错误写入标准错误；机器可读输出不能混入日志文本。
- 日志必须包含阶段信息，但不能输出 Registry 密码、Token、私钥、完整认证头或敏感环境变量。
- 不在底层包直接调用 `os.Exit`；只有 `cmd/sealbuild` 根据返回错误决定退出码。
- 不在库代码中直接打印用户文案；通过进度事件或返回值交给 `internal/cli` 格式化。
- 相同错误在四个平台必须保持一致语义，禁止某个平台静默忽略或改写为成功。

### 测试代码规范

- 单元测试优先使用表驱动测试，测试名必须描述输入条件和预期行为。
- 测试使用 `t.TempDir()` 隔离文件系统，禁止写入用户真实缓存目录或依赖开发机残留状态。
- 单元测试不得依赖公网、真实 Registry 或本机已安装的 QEMU；外部依赖必须通过窄接口隔离。
- 不使用固定 `time.Sleep` 等待异步结果；使用 Channel、Context、状态探测或最终一致性断言。
- 涉及并发和锁的代码必须包含取消、超时、重复调用和竞争测试，并在支持的平台运行 `go test -race ./...`。
- 修复 Bug 前先写能稳定复现问题的测试，再编写最小修复。
- 测试不得为了通过而降低产品约束、跳过错误或增加只在测试环境生效的业务分支。

## Runtime 与供应链

- 精确锁定 Go、QEMU、Linux Kernel、Alpine 或最终 rootfs、BuildKit 和 `runc` 版本，禁止使用 `latest`。
- 使用机器可读的 Runtime Lock Manifest 记录版本、下载来源、SHA-256 和适用平台。
- 外部源码或预编译组件在嵌入前必须校验 SHA-256；校验失败立即终止打包。
- 首次解压必须先写临时目录、完成校验后再原子切换，禁止使用不完整 Runtime 启动构建。
- Runtime 目录必须按内容版本隔离，旧版本只能由明确的清理流程删除。
- `sealbuild version` 和 GitHub Release 元数据必须包含所有内嵌组件版本。
- 不提交大型 Runtime 生成物到 Git，除非项目所有者明确批准仓库二进制管理策略。

## Registry 与安全要求

- 支持标准 Docker Registry 凭据格式，但不能要求用户安装 Docker Desktop 或 Docker Engine。
- 不在日志、错误、构建进度或诊断信息中打印 Registry 密码、Token、私钥和完整 Authorization Header。
- 凭据尽量只保留在内存中，并通过 BuildKit session 转发；不要写入 Guest Runtime 镜像。
- QEMU 端口、Runtime 状态、凭据和缓存文件应使用宿主系统允许的最小权限。
- 导出 OCI 文件前必须校验目标路径；覆盖已有文件必须遵循明确的 CLI 参数语义，不能静默删除其他文件。

## GitHub Actions 与版本发布

GitHub Actions 根据 Git Tag 自动测试、构建并发布：

- `sealbuild-darwin-arm64`
- `sealbuild-darwin-amd64`
- `sealbuild-linux-amd64`
- `sealbuild-windows-amd64.exe`
- `checksums.txt`

发布流程必须：

- 先运行 Go 单元测试和静态检查，再构建 Runtime 和发布文件。
- 从 Git Tag 注入版本号，同时写入 Commit SHA；禁止根据日期或分支名猜测版本。
- 为每个平台嵌入正确的宿主 QEMU 和相同的 `linux/amd64` Guest Runtime。
- 为所有发布文件生成 SHA-256，并在缺少任一平台产物或校验项时终止发布。
- 发布文件必须不可变；同一 Tag 不允许悄悄替换不同内容。
- 四个平台发布候选必须分别在对应 GitHub Actions Runner 内执行两次真实 Dockerfile 构建；第二次必须命中持久缓存，两个 OCI 都必须严格为 `linux/amd64`，构建结束不得残留 QEMU 进程。
- Git Tag Release 必须重新构建并验收当前 Tag 的四个平台产物，禁止复用其他 Workflow Run 的旧候选。
- 缺少任一平台候选、真实构建证据、缓存证据、OCI 平台校验、QEMU 清理证据或 SHA-256 时禁止发布。

## 测试与验收

- 单元测试覆盖 CLI 参数、版本、资产校验、原子解压、缓存锁和生命周期状态机。
- 平台测试覆盖缓存目录、路径、进程终止、文件锁和权限行为。
- 集成测试 Dockerfile 必须包含 `FROM`、`COPY`、联网 `RUN` 和多阶段构建。
- Registry 推送与 OCI 文件导出必须分别验证，不能用其中一种成功代替另一种。
- 每个产物都必须检查 OCI Manifest，确认平台严格为 `linux/amd64`。
- 重复构建必须验证 BuildKit 缓存命中，`sealbuild clean` 必须验证不会删除用户项目文件。
- 只拿到发布二进制的干净宿主必须能完成首次启动和构建，不能依赖预装 Runtime。
- 未在真实宿主完成端到端构建前，不得宣称对应平台已支持。

## 验证命令

Go 基础验证：

```bash
cd /Users/night/Documents/code/sealos/sealbuild
gofmt -l ./cmd ./internal
go vet ./...
go test ./...
go build ./cmd/sealbuild
./out/tools/actionlint .github/workflows/four-host-candidate.yml
```

发布前还必须执行项目后续定义的 Runtime 校验和四平台打包命令；这些命令落地后同步更新本文件，禁止在文档中提前编造不存在的脚本。

只改文档时不需要构建完整 Runtime，但必须确认 Markdown、路径、命令和项目边界准确。
