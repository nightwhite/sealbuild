# Windows AMD64 单文件产品实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法跟踪。当前工作树包含已批准但未提交的 Darwin ARM 开发成果；未经项目所有者明确要求，本计划不创建 Commit、不切换分支、不回滚现有变更。

**目标：** 生成自包含 QEMU 与 Linux Guest Runtime 的 `sealbuild-windows-amd64.exe`，并通过 GitHub Actions Windows Runner 完成两次本地 Dockerfile 构建、缓存、OCI 平台和资源清理验收。

**架构：** 复用现有 BuildKit、Runtime、缓存和 OCI 逻辑，以 build tags 隔离 Windows 文件锁、文件系统发布、QEMU Job Object 和 TCP shutdown。CI 在 MSYS2 CLANG64 中从固定 QEMU 源码构建最小 Host Runtime，再在全新 Windows Job 中嵌入公共 Guest Runtime并执行产品验收。

**技术栈：** Go 1.26.1、`golang.org/x/sys/windows`、QEMU v11.0.2、MSYS2 CLANG64、MinGW-w64 ABI、GitHub Actions `windows-2025`、BuildKit v0.31.1、PowerShell 7。

---

## 文件结构

- `internal/platformfs/`：隔离 Unix 与 Windows 的权限校验、目录同步和不覆盖原子发布。
- `internal/lockfile/lock_windows.go`：使用 `LockFileEx` 的 Windows 非阻塞文件锁。
- `internal/vm/process_windows.go`：Windows Job Object 和无窗口 QEMU 进程。
- `internal/vm/shutdown_*.go`：Darwin Unix Socket 与 Windows loopback TCP shutdown 客户端。
- `internal/vm/config_*.go`：平台专用 shutdown chardev 参数和 QEMU compound path 编码。
- `scripts/runtime/packagewindowshost/`：PE Import Table 闭包、系统 DLL allowlist、固件和许可证打包。
- `scripts/runtime/build-qemu-windows-amd64.ps1`：固定 MSYS2 CLANG64 QEMU 构建入口。
- `runtime/host/windows-amd64/build.lock.json`：Windows QEMU 和依赖版本、来源、SHA-256、许可证锁。
- `internal/runtimeassets/bundle_embedded_*.go`：按宿主 build tags 嵌入对应 Host Runtime和公共 Guest Runtime。
- `scripts/dev/prepare-runtime-assets.go`：跨平台验证并原子准备 embed 资产。
- `.github/workflows/windows-amd64.yml`：Guest、Windows Host、Windows 产品三 Job 验收与候选产物上传。

## 任务 1：跨平台 Manifest 与文件系统语义

**文件：**
- 修改：`internal/runtime/artifact_manifest.go`
- 修改：`internal/runtime/artifact_manifest_test.go`
- 创建：`internal/platformfs/platform.go`
- 创建：`internal/platformfs/platform_unix.go`
- 创建：`internal/platformfs/platform_windows.go`
- 创建：`internal/platformfs/platform_test.go`
- 修改：`internal/runtime/install.go`
- 修改：`internal/runtime/install_test.go`
- 修改：`internal/runtime/extract.go`
- 修改：`internal/tlsmaterial/material.go`
- 修改：`internal/build/output.go`
- 修改：`scripts/runtime/artifact/archive.go`

- [ ] **步骤 1：编写失败的 Host Manifest 平台测试**

增加 `darwin/arm64` 与 `windows/amd64` Host Manifest 成功用例，并保留其他 Host 平台失败用例。预期 API：

```go
func (platform Platform) IsSupportedHost() bool {
    return platform == (Platform{OS: "darwin", Architecture: "arm64"}) ||
        platform == (Platform{OS: "windows", Architecture: "amd64"})
}
```

运行：`go test ./internal/runtime -run TestArtifactManifest -count=1`

预期：Windows Host Manifest 因当前只接受 Darwin 而失败。

- [ ] **步骤 2：实现最小 Host 平台校验并验证绿灯**

运行：`go test ./internal/runtime -run TestArtifactManifest -count=1`

预期：PASS，Guest 仍只允许 `linux/amd64`。

- [ ] **步骤 3：编写平台文件系统行为测试**

定义窄 API：

```go
func PublishFileNoReplace(temporaryPath, finalPath string) error
func PublishDirectoryNoReplace(temporaryPath, finalPath string) error
func SyncDirectory(path string) error
func ValidatePrivateFile(info os.FileInfo) error
func ValidatePublicFile(info os.FileInfo) error
```

测试目标已存在、缺失父目录、普通文件、目录和重复调用。Unix 测试继续断言 `0600`/`0644`；Windows 测试由 CI 断言不依赖 POSIX mode 数字。

运行：`go test ./internal/platformfs -count=1`

预期：FAIL，包尚不存在。

- [ ] **步骤 4：实现平台文件系统并替换直接调用**

Unix 保留硬链接和目录 Sync。Windows 使用同卷 `os.Rename` 前先以不覆盖语义预留目标；冲突映射为 `os.ErrExist`。将 Runtime、TLS、OCI 输出和 Artifact 发布的直接 `os.Link`/目录 Sync/权限校验改为 `platformfs`。

运行：

```bash
go test ./internal/platformfs ./internal/runtime ./internal/tlsmaterial ./internal/build ./scripts/runtime/artifact -count=1
```

预期：PASS，Darwin 现有安全语义不变。

## 任务 2：Windows 文件锁

**文件：**
- 创建：`internal/lockfile/lock_windows.go`
- 创建：`internal/lockfile/lock_windows_test.go`
- 修改：`internal/lockfile/lock.go`

- [ ] **步骤 1：编写 Windows 锁测试源码并交叉编译红灯**

测试首次锁成功、第二次 `errors.Is(err, ErrContended)`、关闭后重获、重复关闭和缺失父目录。实现签名保持：

```go
func TryAcquire(path string) (*Lock, error)
func release(file *os.File) error
```

运行：`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/lockfile -o out/lockfile-windows.test.exe`

预期：FAIL，Windows 缺少 `TryAcquire` 与 `release`。

- [ ] **步骤 2：使用 LockFileEx 实现并交叉编译绿灯**

使用 `LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY` 锁定第一个字节；`ERROR_LOCK_VIOLATION` 映射 `ErrContended`；Handle 保持不可继承。

运行：

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/lockfile -o out/lockfile-windows.test.exe
go test ./internal/lockfile -count=1
```

预期：交叉编译成功，Darwin 测试 PASS；Windows 测试在 Actions 运行。

## 任务 3：跨平台 shutdown 与 QEMU 参数

**文件：**
- 修改：`internal/vm/config.go`
- 修改：`internal/vm/config_test.go`
- 创建：`internal/vm/config_darwin.go`
- 创建：`internal/vm/config_windows.go`
- 重命名：`internal/vm/shutdown.go` 为 `internal/vm/shutdown_darwin.go`
- 修改：`internal/vm/shutdown_test.go`
- 创建：`internal/vm/shutdown_windows.go`
- 创建：`internal/vm/shutdown_windows_test.go`
- 修改：`internal/vm/vm.go`
- 修改：`internal/vm/vm_test.go`
- 修改：`internal/build/runner.go`
- 修改：`internal/build/runner_test.go`

- [ ] **步骤 1：编写 QEMU compound path 编码测试**

测试 Windows 风格绝对路径、空格和逗号；预期逗号按 QEMU 参数规则加倍，参数仍通过 `exec.Cmd.Args` 传递，不进行 Shell quoting。

运行：`go test ./internal/vm -run 'TestConfigArgs|TestEscapeQEMU' -count=1`

预期：FAIL，当前路径没有编码且 shutdown 固定 Unix Socket。

- [ ] **步骤 2：拆分平台 shutdown chardev 参数**

公共 Config 增加：

```go
ShutdownAddress string
ShutdownPort    uint16
```

Darwin 生成 `socket,id=shutdown,path=...`；Windows 生成：

```text
socket,id=shutdown,host=127.0.0.1,port=<port>,server=on,wait=off
```

禁止 `0.0.0.0`、WHPX、HVF、KVM。

- [ ] **步骤 3：编写 Windows TCP shutdown 客户端测试**

用 `net.Listen("tcp4", "127.0.0.1:0")` 接收 `shutdown\n` 并返回 `SEALBUILD_RUNTIME_SHUTDOWN\n`，测试异常 acknowledgement 与超时。

运行：`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/vm -o out/vm-windows.test.exe`

预期：FAIL，Windows shutdown 实现缺失。

- [ ] **步骤 4：实现平台 shutdown 并分配第二个端口**

VM Start 在 Windows 为 BuildKit 和 shutdown 各申请一个回环端口，释放 reservation 后启动 QEMU。Instance 保存 shutdown address，由 `RequestGuestShutdown` 连接。

运行：

```bash
go test ./internal/vm ./internal/build -count=1
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/vm -o out/vm-windows.test.exe
```

预期：Darwin PASS，Windows 交叉编译成功。

## 任务 4：Windows QEMU Job Object 生命周期

**文件：**
- 创建：`internal/vm/process_windows.go`
- 创建：`internal/vm/process_windows_test.go`
- 修改：`internal/vm/process.go`

- [ ] **步骤 1：编写 Windows 进程生命周期测试**

测试 `ExecLauncher` 启动当前测试二进制的 helper process、无窗口标志、Terminate、Kill、Wait、重复回收和启动失败。测试进程必须通过 channel/pipe 协调，不使用固定 sleep。

运行：`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/vm -o out/vm-windows.test.exe`

预期：FAIL，Windows `Terminate` 与 `Kill` 缺失。

- [ ] **步骤 2：实现 CREATE_SUSPENDED + Job Object**

Windows Start 顺序：创建 suspended process、创建 Job、设置 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`、分配 process、恢复主线程。任一步失败都关闭 Handle并终止进程。

`Terminate` 与 `Kill` 都终止 Job；`Wait` 只调用一次 `Cmd.Wait` 并关闭 Job Handle。错误继续带 `start/terminate/kill/wait QEMU process` 阶段。

运行：

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/vm -o out/vm-windows.test.exe
go test ./internal/vm -count=1
```

预期：交叉编译成功，Darwin 生命周期测试 PASS。

## 任务 5：Windows PE Runtime 打包器

**文件：**
- 创建：`scripts/runtime/packagewindowshost/main.go`
- 创建：`scripts/runtime/packagewindowshost/main_test.go`
- 创建：`scripts/runtime/packagewindowshost/pe.go`
- 创建：`scripts/runtime/packagewindowshost/pe_test.go`
- 创建：`scripts/runtime/packagewindowshost/systemdll.go`
- 创建：`scripts/runtime/packagewindowshost/systemdll_test.go`
- 创建：`scripts/runtime/packagewindowshost/fixture_test.go`
- 创建：`runtime/host/windows-amd64/build.lock.json`

- [ ] **步骤 1：编写 PE Import 闭包失败测试**

使用 Go 在测试中生成最小 AMD64 PE fixtures，覆盖：系统 DLL、递归私有 DLL、缺失 DLL、同名冲突、x86 DLL、路径逃逸和大小写不敏感解析。

核心 API：

```go
type PEFile struct { Path string; Imports []string }
func ResolvePEClosure(rootExecutable string, searchDirectories []string) ([]string, error)
```

运行：`go test ./scripts/runtime/packagewindowshost -count=1`

预期：FAIL，包不存在。

- [ ] **步骤 2：使用 debug/pe 实现闭包**

系统 DLL 只允许固定 Windows 10/11 API set 与核心 DLL 名称；未知 DLL 不兜底。所有非系统依赖复制到平坦 `bin/`，冲突立即失败。

运行：`go test ./scripts/runtime/packagewindowshost -count=1`

预期：PASS。

- [ ] **步骤 3：实现 Windows Host Artifact 构建**

CLI 参数：

```text
--qemu
--dll-dir
--qemu-data-dir
--lock
--license-root
--output
```

只复制 `qemu-system-x86_64.exe`、闭包 DLL、固定固件、锁定许可证。Manifest 平台固定 `windows/amd64`，调用现有 deterministic artifact builder。

运行：`go test ./scripts/runtime/packagewindowshost ./scripts/runtime/artifact -count=1`

预期：PASS。

## 任务 6：固定 Windows QEMU 构建入口

**文件：**
- 创建：`scripts/runtime/build-qemu-windows-amd64.ps1`
- 创建：`scripts/runtime/build-qemu-windows-amd64_test.go`
- 修改：`runtime/host/windows-amd64/build.lock.json`

- [ ] **步骤 1：编写脚本静态约束测试**

断言脚本固定 QEMU Revision、要求 MSYSTEM=CLANG64、`--target-list=x86_64-softmmu`、`--enable-tcg`、`--enable-slirp`、`--disable-download`，并明确禁用 WHPX、GUI、user 和 docs。禁止自动下载备用源码或切换工具链。

运行：`go test ./scripts/runtime -run TestWindowsQEMUBuildScript -count=1`

预期：FAIL，脚本不存在。

- [ ] **步骤 2：实现 PowerShell/MSYS2 构建脚本**

脚本接收已 checkout 的固定 QEMU source 和输出目录，不自行 clone，不自动重试。运行 configure、Ninja、PE 架构检查、版本检查和 TCG-only 检查。

运行：

```bash
go test ./scripts/runtime -run TestWindowsQEMUBuildScript -count=1
pwsh -NoProfile -Command '$null = [scriptblock]::Create((Get-Content -Raw scripts/runtime/build-qemu-windows-amd64.ps1))'
```

预期：静态测试与 PowerShell 语法通过。

## 任务 7：平台 Runtime 资产嵌入

**文件：**
- 重命名：`internal/runtimeassets/bundle_embedded.go` 为 `internal/runtimeassets/bundle_embedded_darwin_arm64.go`
- 创建：`internal/runtimeassets/bundle_embedded_windows_amd64.go`
- 修改：`internal/runtimeassets/bundle_stub.go`
- 修改：`internal/runtimeassets/bundle_test.go`
- 创建：`scripts/dev/prepare-runtime-assets/main.go`
- 创建：`scripts/dev/prepare-runtime-assets/main_test.go`
- 删除：`scripts/dev/prepare-runtime-assets.sh`
- 修改：`scripts/dev/prepare-runtime-assets_test.go`

- [ ] **步骤 1：编写 build-tag 与平台验证测试**

Windows bundle 只接受 Host `windows/amd64` + Guest `linux/amd64`；Darwin bundle保持原行为。Stub 在无 embed tag 时返回明确不可用错误。

运行：

```bash
go test ./internal/runtimeassets -count=1
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -tags sealbuild_runtime ./internal/runtimeassets -o out/runtimeassets-windows.test.exe
```

预期：FAIL，Windows bundle 文件不存在且当前文件名未限制平台。

- [ ] **步骤 2：实现平台 bundle 与跨平台资产准备工具**

Go 工具参数：

```text
--host <archive>
--guest <archive>
--output internal/runtimeassets/generated
```

工具调用现有 Runtime 验证，写临时目录后原子发布，不覆盖已存在 generated。该工具替代仅 Unix 可运行的 Shell 脚本。

运行：

```bash
go test ./internal/runtimeassets ./scripts/dev/prepare-runtime-assets -count=1
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags sealbuild_runtime -o out/sealbuild-windows-amd64.exe ./cmd/sealbuild
```

预期：Darwin 测试通过，Windows 产品交叉编译成功。

## 任务 8：Windows GitHub Actions 候选产物与端到端验收

**文件：**
- 创建：`.github/workflows/windows-amd64.yml`
- 创建：`scripts/dev/verify-windows-clean.ps1`
- 创建：`scripts/dev/verify-windows-clean_test.go`
- 修改：`README.md`
- 修改：`docs/runtime-spike-results.md`

- [ ] **步骤 1：编写工作流静态测试**

断言工作流存在三个 Job：`build-guest-runtime`、`build-windows-runtime`、`test-windows-product`。Windows Job 固定 `windows-2025`，产品 Job 不执行 MSYS2 setup；测试两次 build、两次 OCI verify、缓存日志、QEMU 清理、锁清理和 150 MiB 门禁。禁止 Docker、WSL、Hyper-V、WHPX 和远程 builder 命令。

运行：`go test ./scripts/dev -run TestWindowsWorkflow -count=1`

预期：FAIL，工作流不存在。

- [ ] **步骤 2：实现 Windows Runtime Job**

使用固定版本 `msys2/setup-msys2` Action，显式列出 CLANG64 包。Checkout 固定 QEMU Revision，调用 Windows 构建脚本和 PE 打包器，上传 Host Runtime、Manifest、checksums 与构建日志。

- [ ] **步骤 3：实现独立 Windows Product Job**

下载 Host/Guest Artifacts，运行 Go 资产准备工具，构建 `.exe`。在含空格目录执行两次：

```powershell
sealbuild-windows-amd64.exe build --output first.oci.tar runtime\testdata\local-build
sealbuild-windows-amd64.exe build --output cached.oci.tar runtime\testdata\local-build
```

使用 Go OCI verifier 验证平台；日志要求第二次出现 `CACHED`。PowerShell 清理检查使用 `Get-Process qemu-system-x86_64`、`Get-NetTCPConnection` 和第三次锁获取验证，不以强制清理掩盖残留。

- [ ] **步骤 4：实现候选 Artifact 上传与文档**

上传 `sealbuild-windows-amd64.exe`、`checksums.txt`、两个 OCI 验证输出、串口日志和缓存日志。工作流不创建单平台 GitHub Release。README 明确 Windows 状态分为“CI 候选通过”和“Windows Home 实机通过”。

运行：

```bash
go test ./scripts/dev -run 'TestWindowsWorkflow|TestWindowsCleanScript' -count=1
./out/tools/actionlint .github/workflows/windows-amd64.yml
```

预期：PASS。

## 任务 9：完整验证

**文件：**
- 修改：`docs/runtime-spike-results.md`

- [ ] **步骤 1：运行本机完整验证**

```bash
test -z "$(gofmt -l $(rg --files cmd internal scripts -g '*.go'))"
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags sealbuild_runtime -o out/sealbuild-windows-amd64.exe ./cmd/sealbuild
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/lockfile -o out/lockfile-windows.test.exe
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/vm -o out/vm-windows.test.exe
./out/tools/actionlint .github/workflows/runtime-spike.yml .github/workflows/windows-amd64.yml
git diff --check
```

预期：全部退出 0。Mac 只能证明 Windows 源码可交叉编译，不能宣称 Windows 运行通过。

- [ ] **步骤 2：GitHub Actions 真实验收**

推送后运行 `windows-amd64.yml`，要求三个 Job 全绿并下载候选 Artifact。记录 `.exe` 大小、SHA-256、QEMU 版本、TCG-only、两次 OCI 平台、缓存命中和资源清理证据。

预期：工作流退出 0；失败时保留日志并按根因修复，不增加 fallback。

- [ ] **步骤 3：Windows Home 最终验收**

在 Windows 10/11 Home 普通用户账户运行候选 `.exe` 两次构建相同 Context，确认没有 WSL、Hyper-V、WHPX、Docker 和管理员权限依赖。把实测证据写入 `docs/runtime-spike-results.md` 后，才允许把 Windows 状态改为正式支持。
