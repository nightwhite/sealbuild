# Runtime 安装与 VM 生命周期实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans` 逐任务实现此计划。步骤使用复选框跟踪进度。任何生产代码必须遵循 `karpathy-guidelines` 和 TDD。

**目标：** 在 Darwin ARM 上实现可供后续 `sealbuild build` 直接调用的 Runtime 安装、安装级 mTLS、显式代理材料、状态盘锁和 QEMU TCG 生命周期，并通过可注入 Probe 证明 Ready/取消/异常退出/清理语义。

**架构：** `internal/runtime` 从调用方提供的两份固定 tar.zst Asset 安全安装到内容摘要目录，不在本阶段提交或嵌入大型生成物；最终 `go:embed` 适配器留到单文件发布计划。`internal/cache` 只定义 Sealbuild 自有目录和状态盘，`internal/lockfile` 提供 Darwin 非阻塞文件锁，`internal/tlsmaterial` 与 `internal/proxy` 生成 QEMU fw_cfg 文件，`internal/vm` 负责参数、进程、Ready Probe 和关闭状态机。

**技术栈：** Go 1.26 标准库、`github.com/klauspost/compress/zstd` v1.18.6、macOS `flock(2)`、QEMU v11.0.2。此计划不增加第三方 Go 依赖，不实现 BuildKit gRPC Probe，只定义下一阶段必须实现的窄接口。

**版本控制约束：** 项目所有者未要求提交。本计划执行期间不创建 commit；每个任务结束时只运行局部测试、全量测试和 `git diff --check`。

---

## 文件结构

```text
internal/cache/layout.go
    计算并校验 Runtime、state、locks 和 logs 路径，不执行 clean。

internal/cache/layout_test.go
    UserCacheDir、Compatibility ID、路径逃逸和状态盘初始化测试。

internal/lockfile/lock.go
    锁冲突错误、公共 Lock 接口和 Close 语义。

internal/lockfile/lock_darwin.go
    使用 syscall.Flock(LOCK_EX|LOCK_NB) 的 Darwin 实现。

internal/lockfile/lock_darwin_test.go
    真实进程级锁竞争、释放和重复 Close 测试。

internal/runtime/asset.go
    内嵌资产适配器所需的 Asset 描述符与 Compatibility ID。

internal/runtime/extract.go
    压缩资产 SHA、tar.zst、Manifest、checksums 和 payload 严格验证。

internal/runtime/extract_test.go
    路径逃逸、symlink、重复项、权限、SHA、中断流和平台测试。

internal/runtime/install.go
    内容锁、临时目录、Host/Guest 安装、完成标记和原子发布。

internal/runtime/install_test.go
    首次安装、缓存复验、损坏目录拒绝和失败清理测试。

internal/tlsmaterial/material.go
    安装级 CA、Server、Client ECDSA P-256 证书生成与验证。

internal/tlsmaterial/material_test.go
    EKU、SAN、权限、CA 私钥删除、过期和密钥不匹配测试。

internal/proxy/config.go
    显式 Proxy URL 校验、Guest 回环改写、脱敏和 0600 临时文件。

internal/proxy/config_test.go
    http/https、userinfo/query/fragment、回环和无隐式环境读取测试。

scripts/runtime/inspect.go
    改为复用 `internal/proxy`，删除 Smoke 工具中的重复 URL 逻辑。

internal/vm/config.go
    QEMU 路径、drive、fw_cfg、hostfwd 和日志参数的纯构造逻辑。

internal/vm/config_test.go
    TCG-only、回环端口、qcow2、TLS、Proxy 文件和敏感信息测试。

internal/vm/process.go
    真实 `os/exec` 进程边界与窄接口。

internal/vm/process_darwin.go
    Darwin SIGTERM/SIGKILL 实现。

internal/vm/vm.go
    状态盘锁、临时端口、启动、Ready、取消、异常退出和关闭状态机。

internal/vm/vm_test.go
    Fake Process/Probe/Port 验证全部生命周期路径，不依赖真实 QEMU。
```

---

### 任务 1：定义 Cache Layout 与 Compatibility ID

**文件：**

- 创建：`internal/cache/layout.go`
- 创建：`internal/cache/layout_test.go`
- 创建：`internal/runtime/asset.go`
- 创建：`internal/runtime/asset_test.go`

- [ ] **步骤 1：编写 Asset 与路径红灯测试**

固定公开表面：

```go
type Asset struct {
	Name     string
	SHA256   string
	Size     int64
	Open     func() (io.ReadCloser, error)
}

type Bundle struct {
	Host  Asset
	Guest Asset
}

func (bundle Bundle) CompatibilityID() (string, error)
```

Compatibility ID 固定为：

```text
sha256("sealbuild-runtime-v1\n" + hostSHA + "\n" + guestSHA + "\n")
```

测试拒绝空名称、非 64 位小写 SHA、`Size <= 0` 和 nil `Open`。

Cache API 固定为：

```go
type Layout struct {
	Root string
}

func DefaultLayout() (Layout, error)
func (layout Layout) Validate() error
func (layout Layout) RuntimeDir(compatibilityID string) (string, error)
func (layout Layout) StateDir(compatibilityID string) (string, error)
func (layout Layout) RuntimeLockPath(compatibilityID string) (string, error)
func (layout Layout) BuildLockPath() string
func (layout Layout) LogDir() string
```

`DefaultLayout` 必须等于 `filepath.Join(os.UserCacheDir(), "sealbuild")`。ID 只接受 64 位小写 SHA，任何 `/`、`..` 或反斜杠立即失败。

- [ ] **步骤 2：运行测试确认红灯**

```bash
go test ./internal/runtime ./internal/cache -run 'TestAsset|TestBundle|TestLayout' -count=1
```

预期：FAIL，Asset、Bundle 和 Layout 尚未定义。

- [ ] **步骤 3：实现最小模型与纯路径函数**

只使用标准库。路径函数只计算路径，不创建目录，不读取环境代理，不检测旧目录。

- [ ] **步骤 4：验证绿灯**

```bash
gofmt -w internal/runtime/asset.go internal/runtime/asset_test.go internal/cache/layout.go internal/cache/layout_test.go
go test ./internal/runtime ./internal/cache -run 'TestAsset|TestBundle|TestLayout' -count=1
```

- [ ] **步骤 5：运行差异检查**

```bash
git diff --check
git diff -- internal/runtime/asset.go internal/cache/layout.go
```

---

### 任务 2：实现 Darwin 非阻塞文件锁

**文件：**

- 创建：`internal/lockfile/lock.go`
- 创建：`internal/lockfile/lock_darwin.go`
- 创建：`internal/lockfile/lock_darwin_test.go`

- [ ] **步骤 1：编写真实锁竞争红灯测试**

固定 API：

```go
var ErrContended = errors.New("sealbuild lock is already held")

type Lock struct { /* Darwin 文件句柄 */ }

func TryAcquire(path string) (*Lock, error)
func (lock *Lock) Close() error
```

测试必须覆盖：

```text
第一次 TryAcquire 成功
同一进程第二次打开同一路径返回 ErrContended
Close 后可以重新获取
重复 Close 返回 nil
父目录不存在时返回带路径上下文的错误，不自动创建目录
锁文件权限为 0600
```

- [ ] **步骤 2：运行测试确认红灯**

```bash
go test ./internal/lockfile -count=1
```

预期：FAIL，包不存在。

- [ ] **步骤 3：实现 Darwin Flock**

`lock_darwin.go` 使用：

```go
syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
```

只把 `EWOULDBLOCK` 映射为 `ErrContended`。其他错误保留 `%w`。Close 先 `LOCK_UN` 再关闭文件，使用 `errors.Join` 返回两个错误。

- [ ] **步骤 4：验证绿灯与 Race**

```bash
gofmt -w internal/lockfile
go test ./internal/lockfile -count=1
go test -race ./internal/lockfile -count=1
```

---

### 任务 3：严格读取并解压 Runtime Artifact

**文件：**

- 创建：`internal/runtime/extract.go`
- 创建：`internal/runtime/extract_test.go`

- [ ] **步骤 1：编写合法 Host/Guest 解压红灯测试**

固定 API：

```go
type ExtractResult struct {
	Manifest ArtifactManifest
	SHA256   string
	Size     int64
}

func ExtractAsset(ctx context.Context, asset Asset, kind ArtifactKind, destination string) (ExtractResult, error)
```

测试用 `scripts/runtime/artifact.Build` 生成真实 tar.zst fixture，要求：

```text
压缩文件 SHA 与 Size 等于 Asset 描述符
Host 只接受 darwin/arm64
Guest 只接受 linux/amd64
payload、manifest.json、checksums.txt 全部落盘
输出 mode 精确为 0600/0644/0755
返回 Manifest 与归档一致
```

- [ ] **步骤 2：编写安全错误矩阵**

手工 tar.zst fixture 必须覆盖：

```text
压缩资产 SHA 不匹配
Asset Size 不匹配
绝对路径、..、反斜杠
symlink、hardlink、FIFO 和目录以外特殊类型
重复文件
Manifest 缺失或重复
checksums 缺失或重复
Manifest payload 清单与实际 tar 不一致
checksums 行缺失、重复、乱序或 SHA 不一致
文件大小、mode 或 SHA 不一致
Context 在复制中取消
目标目录非空
```

- [ ] **步骤 3：运行测试确认红灯**

```bash
go test ./internal/runtime -run TestExtractAsset -count=1
```

预期：FAIL，`ExtractAsset` 尚未定义。

- [ ] **步骤 4：实现两阶段流式验证**

实现顺序固定：

1. 把 `Asset.Open()` 流复制到 destination 同级临时压缩文件，同时计算 SHA 和 Size。
2. SHA/Size 通过后创建 zstd decoder 和 tar reader。
3. 只允许目录和普通文件；目录 mode 不信任归档，统一创建为 0755。
4. 普通 payload 逐文件写入 `O_EXCL` 文件并计算 SHA；路径通过 `filepath.Rel` 二次确认仍在 destination 内。
5. 严格解析 Manifest 与 checksums；用结构化 observed map 比对，不依赖提取顺序。
6. `fsync` 每个文件和目录；成功后删除临时压缩文件。

不创建 symlink，不调用外部 tar，不接受旧 schema。

- [ ] **步骤 5：验证绿灯与全量 Runtime 回归**

```bash
gofmt -w internal/runtime/extract.go internal/runtime/extract_test.go
go test ./internal/runtime -run TestExtractAsset -count=1
go test ./internal/runtime -count=1
```

---

### 任务 4：生成并验证安装级 mTLS

**文件：**

- 创建：`internal/tlsmaterial/material.go`
- 创建：`internal/tlsmaterial/material_test.go`

- [ ] **步骤 1：编写证书用途与权限红灯测试**

固定 API：

```go
type Paths struct {
	CA         string
	ServerCert string
	ServerKey  string
	ClientCert string
	ClientKey  string
}

func Generate(directory string, now time.Time) (Paths, error)
func Validate(paths Paths, now time.Time) error
```

固定证书约束：

```text
ECDSA P-256
CA: IsCA=true, KeyUsageCertSign
Server: DNS SAN 仅 sealbuild-runtime, EKU 仅 ServerAuth
Client: EKU 仅 ClientAuth
NotBefore = now - 5 分钟
NotAfter = now + 10 年（3650*24h）
ca.crt/server.crt/client.crt mode 0644
server.key/client.key mode 0600
最终目录不含 CA 私钥
```

错误测试覆盖过期、未生效、错误 SAN、错误 EKU、证书/私钥不匹配、错误权限和未知额外私钥文件。

- [ ] **步骤 2：运行测试确认红灯**

```bash
go test ./internal/tlsmaterial -count=1
```

预期：FAIL，包不存在。

- [ ] **步骤 3：实现原子证书目录生成**

使用 `crypto/x509`、`ecdsa.GenerateKey` 和 `pem`。先在同级临时目录生成，CA 私钥只保存在内存，所有文件 Sync 后原子 Rename。目标目录已存在时拒绝覆盖。

- [ ] **步骤 4：实现严格 Validate**

`Validate` 必须分别建立 CA Pool，验证 Server DNSName 与 Client EKU，并用公钥 DER 比较证书和私钥。不得自动重生成、延长或轮换。

- [ ] **步骤 5：验证绿灯**

```bash
gofmt -w internal/tlsmaterial
go test ./internal/tlsmaterial -count=1
go test -race ./internal/tlsmaterial -count=1
```

---

### 任务 5：实现原子 Runtime 安装与状态盘初始化

**文件：**

- 创建：`internal/runtime/install.go`
- 创建：`internal/runtime/install_test.go`
- 修改：`internal/cache/layout.go`
- 修改：`internal/cache/layout_test.go`

- [ ] **步骤 1：编写首次安装与缓存复验红灯测试**

固定 API：

```go
type Installation struct {
	CompatibilityID string
	Root             string
	Host             string
	Guest            string
	StateDisk        string
	TLS              tlsmaterial.Paths
}

type Installer struct {
	Layout cache.Layout
}

func (installer Installer) Install(ctx context.Context, bundle Bundle) (Installation, error)
```

安装目录固定：

```text
runtime/<compatibility-id>/host/
runtime/<compatibility-id>/guest/
runtime/<compatibility-id>/tls/
runtime/<compatibility-id>/installation.json
runtime/<compatibility-id>/complete
state/<compatibility-id>/buildkit-state.qcow2
locks/runtime-<compatibility-id>.lock
```

测试覆盖首次安装、第二次复验后复用、状态盘模板只在不存在时普通复制、损坏完成标记、损坏 payload、锁冲突和失败后无最终目录。

- [ ] **步骤 2：运行测试确认红灯**

```bash
go test ./internal/runtime -run TestInstaller -count=1
```

预期：FAIL，Installer 尚未定义。

- [ ] **步骤 3：实现内容锁与临时安装**

流程严格为：创建 cache 子目录、获取 Runtime Lock、验证现有安装或创建同级临时目录、分别 Extract Host/Guest、Generate TLS、写 installation.json/complete、Sync、Rename。现有目录验证失败直接返回错误，不删除、不回退旧 Runtime。

- [ ] **步骤 4：实现状态盘初始化**

Runtime 目录发布成功后获取独立 state lock，把 `guest/buildkit-state.qcow2` 普通复制到 state 同级临时文件，mode 0600，Sync 后原子发布。已有状态盘只验证是普通文件且 size > 0，不从模板覆盖。

- [ ] **步骤 5：验证绿灯**

```bash
gofmt -w internal/runtime/install.go internal/runtime/install_test.go internal/cache
go test ./internal/runtime ./internal/cache -run 'TestInstaller|TestState' -count=1
go test ./... -count=1
```

---

### 任务 6：统一显式代理材料

**文件：**

- 创建：`internal/proxy/config.go`
- 创建：`internal/proxy/config_test.go`
- 修改：`scripts/runtime/inspect.go`
- 修改：`scripts/runtime/inspect-proxy_test.go`

- [ ] **步骤 1：编写 Proxy 模型红灯测试**

固定 API：

```go
type Config struct {
	Raw   string
	Guest string
}

func Parse(raw string) (Config, error)
func (config Config) Redacted() string
func (config Config) WriteGuestFile(directory string) (path string, cleanup func() error, err error)
```

测试矩阵：http/https、IPv4/localhost/IPv6 回环改写、远程地址不变、userinfo/query/fragment/空 host/空值/前后空白拒绝。`WriteGuestFile` 必须 mode 0600，cleanup 删除文件并允许重复调用。

测试显式清空 `HTTP_PROXY` 等环境后 Parse 仍只使用参数；Parse 空值不读取任何环境变量。

- [ ] **步骤 2：运行测试确认红灯**

```bash
go test ./internal/proxy -count=1
```

预期：FAIL，包不存在。

- [ ] **步骤 3：移动现有 Proxy 逻辑**

把 `scripts/runtime/inspect.go` 的 URL 解析改为调用 `proxy.Parse`，输出 Guest 字符串。删除重复的 net/url 逻辑，保留 Smoke CLI 行为和不泄露错误。

- [ ] **步骤 4：验证绿灯与 Smoke 回归**

```bash
gofmt -w internal/proxy scripts/runtime/inspect.go scripts/runtime/inspect-proxy_test.go
go test ./internal/proxy ./scripts/runtime -run 'TestParse|TestTransformProxy' -count=1
sh -n scripts/runtime/smoke-guest.sh
```

---

### 任务 7：构造固定 QEMU 命令

**文件：**

- 创建：`internal/vm/config.go`
- 创建：`internal/vm/config_test.go`

- [ ] **步骤 1：编写参数红灯测试**

固定模型：

```go
type Config struct {
	QEMUPath    string
	KernelPath  string
	RootFSPath  string
	StatePath   string
	SerialPath  string
	TLS         tlsmaterial.Paths
	ProxyFile   string
	HostPort    uint16
}

func (config Config) Validate() error
func (config Config) Args() ([]string, error)
```

精确参数必须包含：

```text
-accel tcg,thread=multi
-machine q35
-cpu max
-smp 2
-m 2048
-nodefaults
-no-reboot
-nographic
-append root=/dev/vda ro console=ttyS0,115200 panic=1
rootfs format=raw,if=virtio,readonly=on
state format=qcow2,if=virtio
hostfwd=tcp:127.0.0.1:<port>-:1234
3 个 TLS fw_cfg file 参数
可选 1 个 Proxy fw_cfg file 参数
```

拒绝端口 0、非绝对路径、非普通文件、缺 TLS 和 ProxyFile mode 非 0600。参数不得包含 Proxy URL、HVF、KVM、WHPX 或 `0.0.0.0`。

- [ ] **步骤 2：运行测试确认红灯**

```bash
go test ./internal/vm -run TestConfig -count=1
```

预期：FAIL，包不存在。

- [ ] **步骤 3：实现纯参数构造**

不启动进程、不分配端口、不读取环境。Args 返回新切片，调用方修改不能影响 Config。

- [ ] **步骤 4：验证绿灯**

```bash
gofmt -w internal/vm/config.go internal/vm/config_test.go
go test ./internal/vm -run TestConfig -count=1
```

---

### 任务 8：实现 QEMU 生命周期状态机

**文件：**

- 创建：`internal/vm/process.go`
- 创建：`internal/vm/process_darwin.go`
- 创建：`internal/vm/vm.go`
- 创建：`internal/vm/vm_test.go`

- [ ] **步骤 1：编写接口与成功启动红灯测试**

固定接口：

```go
type Process interface {
	Wait() error
	Terminate() error
	Kill() error
}

type Launcher interface {
	Start(path string, args []string) (Process, error)
}

type Probe interface {
	Ready(ctx context.Context, address string, tls tlsmaterial.Paths) error
}

type PortAllocator interface {
	ReserveLoopback() (port uint16, release func() error, err error)
}

type Options struct {
	Launcher        Launcher
	Probe           Probe
	Ports           PortAllocator
	Locks           func(path string) (io.Closer, error)
	ReadyTimeout    time.Duration
	ProbeInterval   time.Duration
	ShutdownTimeout time.Duration
}

type Instance struct { /* 进程、锁、地址、清理所有权 */ }

func Start(ctx context.Context, config Config, stateLockPath string, options Options) (*Instance, error)
func (instance *Instance) Address() string
func (instance *Instance) Close() error
```

成功测试验证：先锁 state、保留并释放 loopback port、再启动一次 QEMU；Probe 成功后返回 `tcp://127.0.0.1:<port>`；Close 发送 Terminate、等待退出、最后释放锁。

- [ ] **步骤 2：编写生命周期错误矩阵**

Fake Process/Probe 必须覆盖：

```text
状态盘锁冲突时不分配端口、不启动进程
端口分配失败时释放锁
Launcher 失败时释放端口 reservation 和锁
端口 reservation 在 Start 前仅释放一次
Probe 未 Ready 时按 ticker 重试同一地址
Probe 超时后 Terminate；期限内不退出则 Kill
Context 取消走同一关闭流程
QEMU 在 Ready 前退出立即返回进程错误
串口监视器报告 SEALBUILD_RUNTIME_FAILED 时立即关闭
Close 重复调用只执行一次
Terminate/Kill/Wait/锁释放/临时文件清理错误使用 errors.Join
绝不重新分配第二个端口、重新启动 QEMU或切换 accelerator
```

- [ ] **步骤 3：运行测试确认红灯**

```bash
go test ./internal/vm -run 'TestStart|TestInstance' -count=1
```

预期：FAIL，生命周期实现不存在。

- [ ] **步骤 4：实现真实进程边界**

`process.go` 使用 `exec.Command` 和 `Start`，不使用 `CommandContext` 自动 SIGKILL。`process_darwin.go` 使用 `os.Interrupt` 不满足 QEMU 语义，因此明确发送 `syscall.SIGTERM`，超时后发送 `syscall.SIGKILL`。Wait 只允许一个 goroutine 调用并把结果广播给状态机。

- [ ] **步骤 5：实现状态机**

Start 创建单一 ownership cleanup 栈；任何返回路径都逆序释放。Ready 循环使用 ticker、process wait channel、serial failure channel、context 和 timeout select，不使用固定 sleep。Close 使用 `sync.Once`，先 Terminate，再等待 shutdown timer，超时 Kill，最后 Wait 和释放资源。

- [ ] **步骤 6：验证绿灯、Race 与全量回归**

```bash
gofmt -w internal/vm
go test ./internal/vm -count=1
go test -race ./internal/vm -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
git diff --check
```

---

## 本计划完成标准

必须同时满足：

```text
Runtime fixture 能首次原子安装并在第二次调用中完整复验
任何损坏安装不会被复用或自动替换
安装级 CA 私钥不落盘，Server/Client EKU 与权限正确
同一状态盘第二个锁立即冲突
QEMU 参数只有 TCG、loopback hostfwd、qcow2 和 fw_cfg 文件
生命周期单测覆盖 Ready、取消、早退、超时强杀与错误组合
go test ./...、go test -race ./...、go vet ./... 全部退出 0
```

本阶段没有真实 BuildKit Probe，因此不宣称 VM 真实 Ready 或镜像构建可用。下一计划必须用 BuildKit v0.31.1 Go Client 实现 `Probe` 和 Solve，并使用 Linux 生成的新 Guest Artifact 执行真实 mTLS Ready。
