# Sealbuild 四宿主 Actions 与 RC 发布实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 在 GitHub Actions 上构建并真实验收 Windows AMD64、Linux AMD64、Darwin ARM64 和 Darwin AMD64 四个自包含 Sealbuild 候选，全部只输出 `linux/amd64` OCI；四平台全绿后由 `v0.1.0-rc.1` Tag 发布统一 Pre-release。

**架构：** 一个 Workflow Run 只构建一次公共 Linux AMD64 Guest Runtime，四个原生 Runner 并行构建各自 QEMU Host Runtime，再在四个干净产品 Job 中嵌入并执行两次标准 Dockerfile 构建。Linux Host Runtime 通过内嵌 ELF 动态加载器和依赖闭包实现用户空间自包含；Darwin 两种架构共用 Mach-O 打包器但显式校验架构与 Homebrew 根；Windows 迁移当前已通过流程。聚合 Job 只接收四个平台同 Commit 的成功候选，Tag Job 重建并验收后创建不可覆盖的 Pre-release。

**技术栈：** Go 1.26.1、BuildKit v0.31.1、QEMU v11.0.2、Buildroot、Go `debug/elf`/`debug/macho`/`debug/pe`、GitHub Actions 官方 Runner、POSIX Shell、PowerShell。

---

## 文件结构

### 修改

- `AGENTS.md`：把四平台真实 Actions 产品构建改为 RC 发布硬门禁。
- `internal/runtimeassets/embedded.go`：允许四个受支持宿主编译公共嵌入逻辑。
- `internal/runtimeassets/bundle_stub.go`：只对四个 Runtime tagged 目标关闭 Stub。
- `scripts/runtime/packagehost/lock.go`：Darwin Build Lock 接受显式 ARM64 或 AMD64。
- `scripts/runtime/packagehost/main.go`：Darwin 打包参数显式包含目标架构和 Homebrew 根。
- `scripts/runtime/packagehost/macho.go`：校验 QEMU 与所有私有 dylib 的 Mach-O 架构。
- `scripts/runtime/packagewindowshost/lock.go`：锁定最终打包 DLL 对应的 MSYS2 运行时包。
- `scripts/runtime/packagewindowshost/main.go`：核对 DLL 来源包后才生成 Artifact。
- `internal/vm/vm.go`：启动前通过平台命令构造函数选择 QEMU 直接执行或 Linux 内嵌加载器执行。
- `internal/vm/config.go`：验证平台启动命令所需的 Host Runtime 文件。
- `internal/build/runner.go`：继续只传递安装后的 Host QEMU 路径，由 VM 包隔离 Linux 启动差异。
- `runtime/host/windows-amd64/build.lock.json`：加入最终 DLL 来源包的精确版本锁。
- `scripts/dev/prepare-runtime-assets_test.go`：覆盖四平台资产准备约束。
- `docs/runtime-spike-results.md`：记录四平台 Actions 和 RC 证据。
- `README.md`：只声明已由 Actions 证明的 RC 候选能力和最终实机验收状态。

### 创建

- `internal/runtimeassets/bundle_embedded_darwin_amd64.go`：Darwin AMD64 Host Artifact 名称。
- `internal/runtimeassets/bundle_embedded_linux_amd64.go`：Linux AMD64 Host Artifact 名称。
- `internal/runtimeassets/bundle_sources_test.go`：静态证明四个平台 build tags 完整且无其他平台。
- `internal/vm/command_darwin.go`：Darwin 直接执行内嵌 QEMU。
- `internal/vm/command_windows.go`：Windows 直接执行内嵌 QEMU。
- `internal/vm/command_linux.go`：Linux 使用内嵌加载器和内嵌 library path 启动 QEMU。
- `internal/vm/command_linux_test.go`：Linux 启动命令和宿主环境隔离测试。
- `scripts/runtime/build-qemu-darwin.sh`：显式目标架构的 Darwin TCG-only QEMU 构建。
- `runtime/host/darwin-amd64/build.lock.json`：Intel Mac Host Runtime 输入锁。
- `scripts/runtime/packagelinuxhost/lock.go`：Linux Host Build Lock schema。
- `scripts/runtime/packagelinuxhost/elf.go`：ELF Interpreter 与 `DT_NEEDED` 递归闭包。
- `scripts/runtime/packagelinuxhost/main.go`：Linux payload、固件、许可证、Manifest 和 tar.zst。
- `scripts/runtime/packagelinuxhost/*_test.go`：Linux Lock、闭包和打包测试。
- `scripts/runtime/build-qemu-linux-amd64.sh`：Linux AMD64 TCG-only QEMU 构建。
- `runtime/host/linux-amd64/build.lock.json`：Linux Host Runtime 输入与依赖版本锁。
- `scripts/dev/aggregatecandidate/main.go`：四平台精确文件聚合、大小和 SHA-256 校验。
- `scripts/dev/aggregatecandidate/main_test.go`：缺平台、额外文件、超限和 hash 测试。
- `scripts/dev/four_host_workflow_test.go`：统一 Workflow 的静态产品门禁测试。
- `.github/workflows/four-host-candidate.yml`：四平台构建、产品验收、聚合与 RC 发布。

### 统一 Workflow 通过后删除

- `.github/workflows/windows-amd64.yml`：被统一 Workflow 取代。
- `.github/workflows/runtime-spike.yml`：Guest Spike 能力被统一 Workflow 的公共 Guest 和四平台产品验收覆盖。
- `scripts/dev/windows_workflow_test.go`：被统一 Workflow 静态门禁测试取代。

---

### 任务 1：扩展四平台 Runtime 资产入口

**文件：**
- 修改：`internal/runtimeassets/embedded.go`
- 修改：`internal/runtimeassets/bundle_stub.go`
- 创建：`internal/runtimeassets/bundle_embedded_darwin_amd64.go`
- 创建：`internal/runtimeassets/bundle_embedded_linux_amd64.go`
- 创建：`internal/runtimeassets/bundle_sources_test.go`

- [ ] **步骤 1：编写失败的四平台源约束测试**

测试读取四个 `bundle_embedded_*` 文件和两个公共 build tag，要求平台集合精确等于：

```go
var expectedBundles = map[string]string{
	"bundle_embedded_darwin_arm64.go":  "sealbuild-host-runtime-darwin-arm64.tar.zst",
	"bundle_embedded_darwin_amd64.go":  "sealbuild-host-runtime-darwin-amd64.tar.zst",
	"bundle_embedded_linux_amd64.go":   "sealbuild-host-runtime-linux-amd64.tar.zst",
	"bundle_embedded_windows_amd64.go": "sealbuild-host-runtime-windows-amd64.tar.zst",
}
```

并要求 `embedded.go` 与 `bundle_stub.go` 同时包含四个平台表达式，不接受 ARM Linux、Windows ARM 或其他平台。

- [ ] **步骤 2：运行测试验证失败**

运行：

```bash
go test ./internal/runtimeassets -run TestEmbeddedBundleSourcesCoverExactlyFourHosts -count=1
```

预期：FAIL，报告缺少 Darwin AMD64 与 Linux AMD64 Bundle 文件。

- [ ] **步骤 3：添加两个 Bundle 并扩展公共 build tags**

Darwin AMD64：

```go
//go:build sealbuild_runtime && darwin && amd64

package runtimeassets

import runtimepkg "github.com/labring/sealbuild/internal/runtime"

func Bundle() (runtimepkg.Bundle, error) {
	return embeddedBundle("sealbuild-host-runtime-darwin-amd64.tar.zst"), nil
}
```

Linux AMD64 使用同一结构并返回 `sealbuild-host-runtime-linux-amd64.tar.zst`。公共 tag 精确覆盖四个平台。

- [ ] **步骤 4：运行资产测试和跨平台编译检查**

运行：

```bash
go test ./internal/runtimeassets -count=1
for target in darwin/arm64 darwin/amd64 linux/amd64 windows/amd64; do
  GOOS=${target%/*} GOARCH=${target#*/} CGO_ENABLED=0 go test -c ./internal/runtimeassets -o /tmp/runtimeassets-${target%/*}-${target#*/}.test
done
```

预期：测试通过，四个目标均可编译 Stub 路径。

- [ ] **步骤 5：提交**

```bash
git add internal/runtimeassets
git commit -m "feat(runtime): 支持四宿主内嵌资产选择"
```

### 任务 2：把 Darwin Host 打包器泛化为双架构

**文件：**
- 修改：`scripts/runtime/packagehost/lock.go`
- 修改：`scripts/runtime/packagehost/lock_test.go`
- 修改：`scripts/runtime/packagehost/main.go`
- 修改：`scripts/runtime/packagehost/main_test.go`
- 修改：`scripts/runtime/packagehost/macho.go`
- 修改：`scripts/runtime/packagehost/macho_test.go`
- 修改：`scripts/runtime/packagehost/manifest.go`

- [ ] **步骤 1：编写失败的 Build Lock 双架构测试**

新增表驱动测试：

```go
tests := []struct {
	name     string
	platform runtimepkg.Platform
	wantErr  string
}{
	{"arm64", runtimepkg.Platform{OS: "darwin", Architecture: "arm64"}, ""},
	{"amd64", runtimepkg.Platform{OS: "darwin", Architecture: "amd64"}, ""},
	{"linux rejected", runtimepkg.Platform{OS: "linux", Architecture: "amd64"}, "hostPlatform must be darwin/arm64 or darwin/amd64"},
}
```

新增打包参数测试，要求 `--host-architecture` 和 `--homebrew-root` 必填且组合只能是 `arm64` + `/opt/homebrew` 或 `amd64` + `/usr/local`。

- [ ] **步骤 2：运行测试验证失败**

运行：

```bash
go test ./scripts/runtime/packagehost -run 'TestBuildLock|TestRunRequiresExplicitDarwinTarget' -count=1
```

预期：FAIL，现有代码只接受 `darwin/arm64` 且 Homebrew 根写死。

- [ ] **步骤 3：实现显式 Darwin 平台配置**

配置结构加入：

```go
type hostPackageConfig struct {
	HostArchitecture string
	HomebrewRoot      string
	// existing fields remain
}
```

`BuildLock.Validate` 只接受两个 Darwin 平台。打包器要求 Lock 架构、显式参数、QEMU Mach-O 架构和 Homebrew 根四者一致。

- [ ] **步骤 4：编写并验证 Mach-O 架构失败测试**

为 Runner 注入 `file` 或 `lipo -archs` 输出，覆盖：arm64 成功、x86_64 成功、Universal 拒绝、QEMU 与 dylib 架构不一致拒绝。

运行：

```bash
go test ./scripts/runtime/packagehost -run 'TestValidateMachOArchitecture|TestPackageHost' -count=1
```

预期：PASS。

- [ ] **步骤 5：运行完整 Darwin 打包器测试**

```bash
go test ./scripts/runtime/packagehost -count=1
```

预期：PASS。

- [ ] **步骤 6：提交**

```bash
git add scripts/runtime/packagehost
git commit -m "feat(runtime): 泛化 Darwin 双架构 Host 打包"
```

### 任务 3：建立 Darwin ARM64 与 AMD64 固定构建定义

**文件：**
- 创建：`scripts/runtime/build-qemu-darwin.sh`
- 修改：`internal/runtime/buildroot_test.go`
- 删除：`scripts/runtime/build-qemu-darwin-arm64.sh`
- 创建：`runtime/host/darwin-amd64/build.lock.json`
- 修改：`runtime/host/darwin-arm64/build.lock.json`

- [ ] **步骤 1：先修改静态测试要求显式架构脚本**

测试要求新脚本接收：

```text
HOST_ARCHITECTURE HOMEBREW_ROOT PYTHON SETUPTOOLS_WHEEL QEMU_SOURCE OUTPUT_DIR
```

并包含固定 QEMU Revision、固定 setuptools SHA-256、`--enable-tcg`、`--enable-slirp`、`--disable-hvf`、`--disable-download`、目标 Mach-O 架构检查和 TCG-only 精确输出。

- [ ] **步骤 2：运行测试验证失败**

```bash
go test ./internal/runtime -run TestDarwinQEMUBuildScriptUsesExplicitArchitecture -count=1
```

预期：FAIL，通用脚本尚不存在。

- [ ] **步骤 3：实现单一显式架构构建脚本**

脚本必须：

```sh
test "$(uname -s)" = Darwin
test "$(uname -m)" = "${host_architecture}"
test "$(brew --prefix)" = "${homebrew_root}"
test "$(git -C "${qemu_source}" rev-parse HEAD)" = e545d8bb9d63e9dd61542b88463183314cff9482
```

ARM64 和 AMD64 不互相探测、不交叉编译、不使用 Rosetta。

- [ ] **步骤 4：添加 Intel Lock 并校验两个 Lock**

Intel Lock 使用与 ARM Lock 相同的固定组件来源、SHA-256 和许可证集合，但 `hostPlatform` 为 `darwin/amd64`。两个 Lock 的组件顺序保持 `qemu`、`glib`、`pixman`、`libslirp`、`zstd`、`gettext`、`pcre2`。

运行：

```bash
go test ./scripts/runtime/packagehost -run TestLoadBuildLock -count=1
sh -n scripts/runtime/build-qemu-darwin.sh
```

预期：PASS。

- [ ] **步骤 5：提交**

```bash
git add scripts/runtime/build-qemu-darwin.sh scripts/runtime/build-qemu-darwin-arm64.sh runtime/host/darwin-* internal/runtime/buildroot_test.go
git commit -m "feat(runtime): 固定 Darwin ARM 与 Intel QEMU 构建"
```

### 任务 4：让 Linux 使用内嵌动态加载器启动 QEMU

**文件：**
- 修改：`internal/vm/config.go`
- 修改：`internal/vm/vm.go`
- 创建：`internal/vm/command_darwin.go`
- 创建：`internal/vm/command_windows.go`
- 创建：`internal/vm/command_linux.go`
- 创建：`internal/vm/command_linux_test.go`
- 修改：`internal/vm/config_test.go`

- [ ] **步骤 1：编写 Linux 启动命令失败测试**

Linux 测试创建：

```text
host/bin/qemu-system-x86_64
host/lib/ld-linux-x86-64.so.2
host/lib/libc.so.6
```

并要求命令为：

```text
host/lib/ld-linux-x86-64.so.2
--library-path
host/lib
host/bin/qemu-system-x86_64
<QEMU args>
```

测试同时要求缺少 loader、缺少 lib 目录、非普通文件都明确失败。

- [ ] **步骤 2：在 Linux 目标运行测试确认失败**

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test ./internal/vm -run TestLinuxQEMUCommand -count=1
```

预期：FAIL，`linuxQEMUCommand` 尚不存在。

- [ ] **步骤 3：实现平台命令构造边界**

`vm.Start` 在生成 QEMU 参数后调用：

```go
launchPath, launchArgs, err := qemuCommand(config, args)
if err != nil {
	return nil, errors.Join(fmt.Errorf("construct QEMU launch command: %w", err), cleanup())
}
process, err := options.Launcher.Start(launchPath, launchArgs)
```

Darwin 和 Windows 原样返回 QEMU；Linux 从 QEMU 所在 Host 根目录解析内嵌 loader 和 `lib`。不读取宿主 `LD_LIBRARY_PATH`，不搜索 PATH。

- [ ] **步骤 4：运行 VM 单元测试与跨平台编译**

```bash
go test ./internal/vm -count=1
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/vm -o /tmp/vm-linux-amd64.test
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/vm -o /tmp/vm-windows-amd64.test
```

预期：PASS。

- [ ] **步骤 5：提交**

```bash
git add internal/vm
git commit -m "feat(vm): 使用内嵌 Linux ELF 加载器启动 QEMU"
```

### 任务 5：实现 Linux ELF Host Runtime 打包器

**文件：**
- 创建：`scripts/runtime/packagelinuxhost/lock.go`
- 创建：`scripts/runtime/packagelinuxhost/lock_test.go`
- 创建：`scripts/runtime/packagelinuxhost/elf.go`
- 创建：`scripts/runtime/packagelinuxhost/elf_test.go`
- 创建：`scripts/runtime/packagelinuxhost/main.go`
- 创建：`scripts/runtime/packagelinuxhost/main_test.go`

- [ ] **步骤 1：编写 Linux Build Lock 失败测试**

Lock schema：

```go
type LinuxBuildLock struct {
	SchemaVersion int                    `json:"schemaVersion"`
	HostPlatform  runtimepkg.Platform    `json:"hostPlatform"`
	Components    []runtimepkg.Component `json:"components"`
	FirmwareFiles []string               `json:"firmwareFiles"`
}
```

测试拒绝非 `linux/amd64`、空组件、重复或逃逸固件路径、未知 JSON 字段和尾随 JSON。

- [ ] **步骤 2：运行 Lock 测试验证失败**

```bash
go test ./scripts/runtime/packagelinuxhost -run TestLoadLinuxBuildLock -count=1
```

预期：FAIL，包尚不存在。

- [ ] **步骤 3：实现最小 Lock 解析并通过测试**

复用 `runtime.ArtifactManifest.Validate` 校验组件；固件路径使用 clean slash relative path 规则。

- [ ] **步骤 4：编写 ELF 闭包失败测试**

对可注入的 Inspector 测试以下图：

```text
qemu -> ld-linux, libglib.so, libslirp.so
libglib.so -> libc.so
libslirp.so -> libc.so
```

期望稳定输出 loader、QEMU 和按 basename 排序的库。另测循环依赖、缺失库、同名冲突、非 AMD64、相对 Interpreter、搜索目录逃逸。

- [ ] **步骤 5：运行 ELF 测试确认失败**

```bash
go test ./scripts/runtime/packagelinuxhost -run 'TestResolveELFClosure|TestInspectELF' -count=1
```

预期：FAIL，ELF Inspector 尚未实现。

- [ ] **步骤 6：使用 `debug/elf` 实现闭包**

核心模型：

```go
type ELFImage struct {
	Path        string
	Interpreter string
	Needed      []string
}

type ELFClosure struct {
	Executable string
	Loader     string
	Libraries  []string
}
```

`inspectELF` 要求 `elf.EM_X86_64`，读取 `.interp` 和 `ImportedLibraries()`；解析只使用显式 `--library-dir` 索引。任何同 basename 不同内容都失败。

- [ ] **步骤 7：编写打包失败测试**

测试要求 Artifact 精确包含：

```text
bin/qemu-system-x86_64
lib/ld-linux-x86-64.so.2
lib/*.so*
share/qemu/<locked firmware>
licenses/**
manifest.json
checksums.txt
```

并验证可执行 mode、Manifest 平台和临时目录清理。

- [ ] **步骤 8：实现打包并运行全包测试**

```bash
go test ./scripts/runtime/packagelinuxhost -count=1
```

预期：PASS。

- [ ] **步骤 9：提交**

```bash
git add scripts/runtime/packagelinuxhost
git commit -m "feat(runtime): 打包自包含 Linux QEMU ELF 闭包"
```

### 任务 6：建立 Linux QEMU 构建锁并补强 Windows 运行时锁

**文件：**
- 创建：`scripts/runtime/build-qemu-linux-amd64.sh`
- 创建：`runtime/host/linux-amd64/build.lock.json`
- 修改：`scripts/runtime/packagewindowshost/lock.go`
- 修改：`scripts/runtime/packagewindowshost/lock_test.go`
- 修改：`scripts/runtime/packagewindowshost/main.go`
- 修改：`scripts/runtime/packagewindowshost/main_test.go`
- 修改：`runtime/host/windows-amd64/build.lock.json`
- 修改：`internal/runtime/buildroot_test.go`

- [ ] **步骤 1：编写 Linux 构建脚本静态失败测试**

要求固定 Revision、`uname -m = x86_64`、`--enable-tcg`、`--enable-slirp`、`--disable-kvm`、`--disable-xen`、`--disable-gtk`、`--disable-sdl`、`--disable-docs`、`--disable-user`、`--disable-download`、AMD64 ELF 和 TCG-only 检查。

- [ ] **步骤 2：运行静态测试确认失败**

```bash
go test ./internal/runtime -run TestLinuxQEMUBuildScriptUsesPinnedTCGOnlyConfiguration -count=1
```

预期：FAIL，Linux 构建脚本不存在。

- [ ] **步骤 3：实现 Linux 构建脚本和初始 Lock**

Lock 至少记录 QEMU、glib、pixman、libslirp、zstd、glibc/loader 的精确版本、来源和 SHA-256，并列出四个固定 firmware 文件。首轮固定 Runner 输出实际包版本后，在同一开发分支更新 Lock；下一轮必须先校验 Lock 再打包。

- [ ] **步骤 4：编写 Windows DLL 包锁失败测试**

`WindowsBuildLock` 增加：

```go
type LockedRuntimePackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
```

测试拒绝空值、重复包、未排序包和实际 `pacman -Q` 清单不一致。打包器要求每个非系统 DLL 都能映射到一个已锁定包。

- [ ] **步骤 5：实现 Windows 运行时包校验**

工作流把 `pacman -Qo <dll>` 的包名和版本写成确定性 JSON，打包器通过新参数读取并与 Lock 比对。工具链包不进入 Manifest；最终 DLL 来源包进入锁和证据。

- [ ] **步骤 6：运行 Host 构建定义测试**

```bash
sh -n scripts/runtime/build-qemu-linux-amd64.sh scripts/runtime/build-qemu-darwin.sh scripts/runtime/build-qemu-windows-amd64.sh
go test ./internal/runtime ./scripts/runtime/packagewindowshost ./scripts/runtime/packagelinuxhost -count=1
```

预期：PASS。

- [ ] **步骤 7：提交**

```bash
git add scripts/runtime/build-qemu-linux-amd64.sh runtime/host/linux-amd64 scripts/runtime/packagewindowshost runtime/host/windows-amd64 internal/runtime/buildroot_test.go
git commit -m "feat(runtime): 固定 Linux 与 Windows Host 依赖"
```

### 任务 7：实现四平台候选聚合与版本元数据

**文件：**
- 创建：`scripts/dev/aggregatecandidate/main.go`
- 创建：`scripts/dev/aggregatecandidate/main_test.go`
- 修改：`internal/version/version_test.go`

- [ ] **步骤 1：编写聚合失败测试**

输入目录必须精确包含四个文件。测试覆盖：

```go
var candidateNames = []string{
	"sealbuild-darwin-amd64",
	"sealbuild-darwin-arm64",
	"sealbuild-linux-amd64",
	"sealbuild-windows-amd64.exe",
}
```

缺文件、额外文件、目录、空文件、`>= 150*1024*1024`、平台 checksum 不一致都失败。成功时写按文件名排序的 `checksums.txt` 和包含 Version、Commit、BuiltAt 的 `candidate.json`。

- [ ] **步骤 2：运行聚合测试确认失败**

```bash
go test ./scripts/dev/aggregatecandidate -count=1
```

预期：FAIL，聚合器不存在。

- [ ] **步骤 3：实现流式 SHA-256 和原子元数据输出**

CLI：

```text
aggregatecandidate --input DIR --output DIR --version VERSION --commit SHA --built-at RFC3339
```

输出目录必须不存在；使用同级临时目录完成全部校验后原子 rename。Tag、SHA 和时间格式不合法立即失败。

- [ ] **步骤 4：验证版本注入行为**

为 `internal/version` 增加表驱动测试，确保默认值和 `Info.String()` 稳定。Workflow 使用：

```text
-trimpath -buildvcs=false
-X github.com/labring/sealbuild/internal/version.Version=<tag-or-dev>
-X github.com/labring/sealbuild/internal/version.Commit=<GITHUB_SHA>
-X github.com/labring/sealbuild/internal/version.BuiltAt=<commit-UTC-RFC3339>
```

- [ ] **步骤 5：运行测试并提交**

```bash
go test ./scripts/dev/aggregatecandidate ./internal/version -count=1
git add scripts/dev/aggregatecandidate internal/version/version_test.go
git commit -m "feat(release): 聚合四宿主候选与校验和"
```

### 任务 8：实现统一四平台 GitHub Actions Workflow

**文件：**
- 创建：`scripts/dev/four_host_workflow_test.go`
- 创建：`.github/workflows/four-host-candidate.yml`
- 修改：`AGENTS.md`

- [ ] **步骤 1：编写 Workflow 失败测试**

静态测试要求以下 Jobs 全部存在：

```text
quality
build-guest-runtime
build-host-linux-amd64
build-host-windows-amd64
build-host-darwin-arm64
build-host-darwin-amd64
test-linux-amd64
test-windows-amd64
test-darwin-arm64
test-darwin-amd64
aggregate
publish-rc
```

要求固定 Runner 标签、固定第三方 Action SHA、四个候选文件、两次 build、`CACHED`、两次 `verify-oci`、QEMU 残留检查、150 MiB、严格 RC Tag 正则、`--prerelease`、`--verify-tag` 和同名 Release 拒绝。产品 Jobs 禁止 `docker`、`wsl`、`kvm`、`hvf`、`whpx`、系统 QEMU 和远程 Builder 命令。

- [ ] **步骤 2：运行 Workflow 测试确认失败**

```bash
go test ./scripts/dev -run TestFourHostWorkflow -count=1
```

预期：FAIL，统一 Workflow 尚不存在。

- [ ] **步骤 3：实现质量与公共 Guest Jobs**

`quality` 执行：

```text
gofmt check
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
sh -n scripts/runtime/*.sh scripts/dev/*.sh
git diff --check
actionlint
```

Guest Job 只使用固定官方 URL、SHA-256 和 Buildroot Commit，不添加重试或备用源。

- [ ] **步骤 4：实现四个 Host Runtime Jobs**

每个 Job 在原生固定 Runner 构建、验证并上传自己的 tar.zst。Darwin Jobs 显式传入架构/Homebrew 根；Linux Job运行 ELF packager；Windows Job 迁移当前已通过 PE/DLL 流程并补运行时包锁。

- [ ] **步骤 5：实现四个产品双构建 Jobs**

每个产品 Job：下载同一 Guest 和本 Host、验证 Runtime、准备 embed、注入版本、清理第三方 PATH、在含空格目录构建两次、验证 `CACHED`、两个 OCI、QEMU 清理和大小，再上传候选及诊断证据。

- [ ] **步骤 6：实现聚合与 RC 发布 Jobs**

`aggregate` 只下载当前 Run 四个产品 Artifact，调用 `aggregatecandidate`。`publish-rc` 只在严格 RC Tag 上运行，先拒绝已存在 Release，再执行：

```bash
gh release create "$GITHUB_REF_NAME" \
  --verify-tag \
  --prerelease \
  --title "$GITHUB_REF_NAME" \
  --notes-file "$RUNNER_TEMP/release-notes.md" \
  "$RUNNER_TEMP/release"/*
```

- [ ] **步骤 7：更新项目政策并运行本地 Workflow 检查**

`AGENTS.md` 改为四平台真实产品构建是 RC 发布硬门禁，保留最终实机验收限制。

运行：

```bash
go test ./scripts/dev -run TestFourHostWorkflow -count=1
./out/tools/actionlint .github/workflows/four-host-candidate.yml
git diff --check
```

预期：PASS。

- [ ] **步骤 8：提交并推送开发分支**

```bash
git add .github/workflows/four-host-candidate.yml scripts/dev/four_host_workflow_test.go AGENTS.md
git commit -m "ci: 验收并聚合四宿主候选"
git push -u origin feat/four-host-actions
```

### 任务 9：在 Actions 中逐平台闭环并迁移旧工作流

**文件：**
- 修改：首轮 Actions 明确暴露问题对应的最小源码、Lock 或 Workflow 文件
- 删除：`.github/workflows/windows-amd64.yml`
- 删除：`.github/workflows/runtime-spike.yml`
- 删除：`scripts/dev/windows_workflow_test.go`
- 修改：`README.md`
- 修改：`docs/runtime-spike-results.md`

- [ ] **步骤 1：从开发分支手动执行统一 Workflow**

```bash
gh workflow run four-host-candidate.yml --ref feat/four-host-actions
gh run watch --exit-status <RUN_ID>
```

预期：首轮用于获取四个固定 Runner 的真实依赖和平台错误；不把失败解释为通过。

- [ ] **步骤 2：按根因修复每个平台**

每次失败：下载该 Run 日志和诊断 Artifact，定位到源码、依赖锁、打包闭包或 Runner 命令。先补能稳定复现的测试，再做最小修复并提交。禁止添加 retry、备用 URL、镜像站、平台降级或跳过门禁。

- [ ] **步骤 3：完成依赖 Lock 的第二次严格验证**

Linux、Darwin AMD64 和补强后的 Windows Lock 在首轮记录明确值后，下一次 Workflow 必须在打包前验证实际依赖与 Lock 完全一致。只有至少一轮“先验证 Lock，再生成候选”成功，才满足可复现门禁。

- [ ] **步骤 4：取得开发分支同一次四平台全绿 Run**

验收四个产品 Jobs 均有：首次日志、缓存日志、两个 OCI verifier 输出、QEMU 清理证据、候选大小和 SHA-256。

- [ ] **步骤 5：删除旧事实来源并更新文档**

统一 Workflow 全绿后删除旧两个 Workflow 和旧 Windows 静态测试。README 和结果文档记录 Run URL、四个候选大小/hash、缓存证据和“等待真实设备验收”。

- [ ] **步骤 6：运行完整本地验证并提交**

```bash
test -z "$(gofmt -l $(rg --files cmd internal scripts -g '*.go'))"
sh -n scripts/runtime/*.sh scripts/dev/*.sh
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
./out/tools/actionlint .github/workflows/four-host-candidate.yml
git diff --check
git add .github/workflows README.md docs/runtime-spike-results.md scripts/dev
git commit -m "docs: 记录四宿主候选验收"
```

- [ ] **步骤 7：把已验收分支合入并推送 `main`**

在主工作区快进合并 `feat/four-host-actions`，不做 squash，不改写已由 Actions 验证的提交：

```bash
git merge --ff-only feat/four-host-actions
git push origin main
```

- [ ] **步骤 8：验证 `main` 同一次四平台全绿**

```bash
gh run list --workflow four-host-candidate.yml --branch main --limit 1
gh run watch --exit-status <MAIN_RUN_ID>
```

预期：所有质量、Guest、四 Host、四产品和 aggregate Jobs 为 success；`publish-rc` 因非 Tag 正确跳过。

### 任务 10：创建并验收 `v0.1.0-rc.1` Pre-release

**文件：**
- 不修改源码；Tag 必须指向已全绿的 `main` Commit。

- [ ] **步骤 1：确认发布前状态**

```bash
git status --short --branch
test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)"
test -z "$(git tag --list v0.1.0-rc.1)"
test -z "$(gh release list --json tagName --jq '.[] | select(.tagName == "v0.1.0-rc.1") | .tagName')"
```

预期：工作区干净、HEAD 等于远端 main、Tag 和 Release 均不存在。

- [ ] **步骤 2：创建并推送不可移动的 RC Tag**

```bash
git tag -a v0.1.0-rc.1 -m "sealbuild v0.1.0-rc.1"
git push origin refs/tags/v0.1.0-rc.1
```

- [ ] **步骤 3：等待 Tag Workflow 完整通过**

```bash
gh run list --workflow four-host-candidate.yml --branch v0.1.0-rc.1 --limit 1
gh run watch --exit-status <TAG_RUN_ID>
```

预期：全部 Jobs success，包括 `publish-rc`。

- [ ] **步骤 4：审计 Release 内容和 SHA-256**

```bash
gh release view v0.1.0-rc.1 --json isPrerelease,tagName,targetCommitish,assets,url
gh release download v0.1.0-rc.1 --dir /tmp/sealbuild-v0.1.0-rc.1
cd /tmp/sealbuild-v0.1.0-rc.1
shasum -a 256 -c checksums.txt
```

预期：Pre-release 为 true，只含四个候选和 `checksums.txt`，全部 hash 通过。

- [ ] **步骤 5：形成最终实机验收交接**

报告 Tag、Commit、主分支 Run、Tag Run、Release URL、四个大小和 SHA-256，并给出四个平台统一测试命令。明确状态为“RC Actions 验收完成，等待真实 Windows Home、Linux、Apple Silicon Mac 和 Intel Mac 验收”，不宣称正式支持。

---

## 计划自检

- 规格中的四个平台、公共 Guest、双构建、全新 VM、缓存、OCI、QEMU 清理、150 MiB、统一校验、Tag 重建、Pre-release 和最终实机边界分别由任务 1 至 10 覆盖。
- 每个代码行为任务都先写失败测试并明确红灯命令，再写最少实现并运行绿灯命令。
- 配置文件由静态 Go 测试和 `actionlint` 约束；真实行为由四个原生 Runner 的产品 Job 证明。
- 不包含未完成占位符、自动重试、镜像切换、旧 Artifact fallback、远程 Builder 或硬件加速降级。
- 首轮 Runner 依赖发现不作为通过证据；依赖值进入 Lock 后必须再运行一轮严格验证。
- 正式 `v0.1.0` 不属于本计划，RC 后唯一剩余工作是用户多平台实机验收。
