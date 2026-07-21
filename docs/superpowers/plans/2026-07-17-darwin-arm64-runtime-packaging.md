# Darwin ARM Runtime 打包实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans` 逐任务实现此计划。步骤使用复选框跟踪进度。任何生产代码都必须遵循 `karpathy-guidelines` 和 TDD。

**目标：** 生成可嵌入 Sealbuild 的 Darwin ARM Host Runtime 与无共享私钥的 `linux/amd64` Guest Runtime，并在 Apple Silicon Mac 上证明解压后的 QEMU 不依赖 Homebrew、只使用 TCG、能通过 fw_cfg 启动同一 BuildKit Guest。

**架构：** 通用 Artifact Builder 根据严格 Manifest 生成确定性 tar.zst。Darwin Host Packager 递归收集 QEMU 的非系统 dylib、重写 Mach-O 加载路径并 ad-hoc codesign。Guest Runtime 通过 QEMU fw_cfg 接收每次测试生成的 mTLS 与显式代理，持久盘改为 32 GiB 虚拟容量的 qcow2。

**技术栈：** Go 1.26、QEMU v11.0.2、Buildroot 2026.05.1、Linux 6.18.7、BuildKit v0.31.1、`github.com/klauspost/compress/zstd` v1.18.6、macOS `otool` / `install_name_tool` / `codesign`、GNU tar 测试夹具。

**版本控制约束：** 项目所有者未要求本轮提交。执行过程中不创建 commit；每个任务结束时只检查 `git diff` 和测试结果。获得明确提交指令后再按任务边界提交。

---

## 文件结构

新增或修改的文件职责如下：

```text
internal/runtime/artifact_manifest.go
    运行时使用的严格 Artifact Manifest 模型与校验。

internal/runtime/artifact_manifest_test.go
    Manifest schema、平台、路径、排序、重复项和 SHA-256 测试。

scripts/runtime/artifact/archive.go
    扫描 payload、生成 Manifest/checksums、写确定性 tar.zst。

scripts/runtime/artifact/archive_test.go
    确定性、权限、symlink 拒绝、校验值和解包内容测试。

scripts/runtime/packagehost/main.go
    Darwin Host Runtime 打包命令入口。

scripts/runtime/packagehost/macho.go
    otool 依赖图、系统库分类、dylib 复制和加载路径重写。

scripts/runtime/packagehost/macho_test.go
    依赖图、同名冲突、未解析依赖和重写命令测试。

scripts/runtime/packagehost/runner.go
    可注入外部命令执行边界。

scripts/runtime/packagehost/manifest.go
    Host Runtime 组件和许可证元数据组装。

scripts/runtime/build-qemu-darwin-arm64.sh
    固定 QEMU Commit 和配置参数的 Darwin ARM 构建入口。

scripts/runtime/fetch-host-licenses.sh
    按锁定 URL/SHA 获取 Host Runtime 第三方许可证。

runtime/host/darwin-arm64/build.lock.json
    QEMU 与所有随包 dylib 的固定版本、源码、SHA 和许可证表达式。

runtime/buildroot/board/sealbuild/x86_64/linux.config
    增加 fw_cfg sysfs Kernel 能力。

runtime/buildroot/board/sealbuild/x86_64/post-build.sh
    移除 Spike 证书写入，只保留只读 rootfs 挂载点。

runtime/buildroot/board/sealbuild/x86_64/rootfs-overlay/etc/init.d/S50sealbuild-runtime
    从 fw_cfg 读取 mTLS/代理并从状态盘启动 BuildKit。

runtime/buildroot/board/sealbuild/x86_64/rootfs-overlay/etc/buildkit/buildkitd.toml
    TLS 路径改为状态盘 Runtime 目录。

scripts/runtime/build-guest.sh
    生成 32 GiB ext4 raw 临时盘并转换为 qcow2。

scripts/runtime/package-guest.sh
    改为调用通用 Artifact Builder，不再打包证书或 raw sparse 盘。

scripts/runtime/collect-guest-licenses.sh
    收集 Buildroot legal-info 和预编译 BuildKit/runc/CNI 的完整许可证树。

scripts/runtime/smoke-guest.sh
    使用 qcow2、fw_cfg mTLS 和可选显式 Proxy 执行 Smoke。

internal/runtime/buildroot_test.go
    Guest fw_cfg、无内嵌私钥、qcow2 和固定 Kernel 配置回归测试。

.github/workflows/runtime-spike.yml
    使用固定 QEMU v11.0.2 的 qemu-img 构建新 Guest 并执行迁移后的 Smoke。

docs/runtime-spike-results.md
    记录新 Runtime 格式、体积和 Darwin 自包含 QEMU 验证结果。
```

---

### 任务 1：定义严格 Artifact Manifest

**文件：**

- 创建：`internal/runtime/artifact_manifest.go`
- 创建：`internal/runtime/artifact_manifest_test.go`

- [ ] **步骤 1：编写合法 Manifest 与错误矩阵测试**

测试 API 固定为：

```go
manifest := ArtifactManifest{
	SchemaVersion: 1,
	Kind:          ArtifactKindHost,
	Platform:      Platform{OS: "darwin", Architecture: "arm64"},
	Components: []Component{{
		Name:    "qemu",
		Version: "v11.0.2",
		Source:  "https://gitlab.com/qemu-project/qemu.git",
		SHA256:  validSHA256,
	}},
	Files: []ArtifactFile{{
		Path:   "bin/qemu-system-x86_64",
		SHA256: validSHA256,
		Size:   1024,
		Mode:   0o755,
	}},
}
```

表驱动错误用例必须覆盖：

```text
schemaVersion != 1
kind 不是 host/guest
host 平台不是 darwin/arm64
guest 平台不是 linux/amd64
空 components
空 files
绝对路径
包含 .. 的路径
包含反斜杠的路径
manifest.json 或 checksums.txt 出现在 payload
重复路径
未按字典序排序
非 64 位小写 SHA-256
size <= 0
mode 含文件类型位或不受支持权限
JSON 未知字段
JSON 尾随值
```

- [ ] **步骤 2：运行测试确认红灯**

运行：

```bash
go test ./internal/runtime -run 'TestArtifactManifest|TestLoadArtifactManifest' -count=1
```

预期：FAIL，`ArtifactManifest`、`ArtifactFile` 和 `LoadArtifactManifest` 尚未定义。

- [ ] **步骤 3：实现最小 Manifest 模型**

实现以下公开表面：

```go
type ArtifactKind string

const (
	ArtifactKindHost  ArtifactKind = "host"
	ArtifactKindGuest ArtifactKind = "guest"
)

type ArtifactManifest struct {
	SchemaVersion int            `json:"schemaVersion"`
	Kind          ArtifactKind   `json:"kind"`
	Platform      Platform       `json:"platform"`
	Components    []Component    `json:"components"`
	Files         []ArtifactFile `json:"files"`
}

type ArtifactFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
}

func LoadArtifactManifest(reader io.Reader) (ArtifactManifest, error)
func (manifest ArtifactManifest) Validate() error
```

路径校验使用 `path.Clean`，不使用宿主 `filepath.Clean`，保证 Manifest 始终使用 `/`。

- [ ] **步骤 4：验证绿灯与现有 Lock 回归**

```bash
gofmt -w internal/runtime/artifact_manifest.go internal/runtime/artifact_manifest_test.go
go test ./internal/runtime -count=1
```

预期：PASS，现有 `runtime/manifest.lock.json` 测试保持通过。

- [ ] **步骤 5：检查任务差异**

```bash
git diff --check
git diff -- internal/runtime/artifact_manifest.go internal/runtime/artifact_manifest_test.go
```

---

### 任务 2：实现确定性 Artifact Builder

**文件：**

- 创建：`scripts/runtime/artifact/archive.go`
- 创建：`scripts/runtime/artifact/archive_test.go`
- 修改：`go.mod`
- 创建或修改：`go.sum`

- [ ] **步骤 1：编写 payload 扫描测试**

使用 `t.TempDir()` 创建：

```text
payload/
├── bin/tool       mode 0755
└── lib/data       mode 0644
```

期望：

```go
files, err := ScanPayload(payloadDir)
```

返回按 `bin/tool`、`lib/data` 排序的 `[]runtime.ArtifactFile`，SHA-256、逻辑大小和权限正确。

拒绝测试：

- symlink。
- socket、FIFO 或设备文件。
- payload 中已有 `manifest.json` 或 `checksums.txt`。
- 空 payload。

- [ ] **步骤 2：运行扫描测试确认红灯**

```bash
go test ./scripts/runtime/artifact -run TestScanPayload -count=1
```

预期：FAIL，`ScanPayload` 尚未定义。

- [ ] **步骤 3：实现扫描与 SHA-256**

公开表面：

```go
func ScanPayload(root string) ([]runtime.ArtifactFile, error)
func HashFile(path string) (string, int64, error)
```

扫描使用 `os.ReadDir` 递归，遇到任何非普通文件立即失败，不跟随 symlink。

- [ ] **步骤 4：编写 Artifact 生成红灯测试**

测试调用：

```go
result, err := Build(BuildConfig{
	PayloadDir: payloadDir,
	OutputPath: archivePath,
	Manifest: runtime.ArtifactManifest{
		SchemaVersion: 1,
		Kind:          runtime.ArtifactKindHost,
		Platform:      runtime.Platform{OS: "darwin", Architecture: "arm64"},
		Components:    components,
	},
})
```

要求：

- `manifest.json` 的 `files` 来自真实 payload，不接受调用方伪造。
- `checksums.txt` 包含 payload 和 `manifest.json`，不包含自身。
- tar 路径、uid、gid、用户名、组名、mtime 固定。
- zstd 压缩级别固定。
- 连续构建两次字节完全一致。
- 输出已存在时失败，不覆盖。
- 构建失败不留下最终文件。

- [ ] **步骤 5：运行生成测试确认红灯**

```bash
go test ./scripts/runtime/artifact -run 'TestBuild|TestArchive' -count=1
```

预期：FAIL，`Build` 尚未定义。

- [ ] **步骤 6：引入固定 zstd 依赖并实现最小 Builder**

```bash
go get github.com/klauspost/compress@v1.18.6
```

实现：

```go
type BuildConfig struct {
	PayloadDir string
	OutputPath string
	Manifest  runtime.ArtifactManifest
}

type BuildResult struct {
	Manifest      runtime.ArtifactManifest
	ArchiveSHA256 string
	ArchiveSize   int64
}

func Build(config BuildConfig) (BuildResult, error)
```

tar header 固定：

```go
header.Uid = 0
header.Gid = 0
header.Uname = "root"
header.Gname = "root"
header.ModTime = time.Unix(0, 0).UTC()
header.AccessTime = time.Time{}
header.ChangeTime = time.Time{}
```

输出先写同目录临时文件，`Close`、`Sync`、校验后再 `Rename`。

- [ ] **步骤 7：运行包测试和全量测试**

```bash
gofmt -w scripts/runtime/artifact
go test ./scripts/runtime/artifact -count=1
go test ./... -count=1
```

预期：全部 PASS。

- [ ] **步骤 8：检查依赖与许可证**

```bash
go mod tidy
go list -m all
git diff --check
```

确认 `klauspost/compress` 精确锁定为 v1.18.6，没有非必要依赖。

---

### 任务 3：锁定 Darwin ARM Host Runtime 输入

**文件：**

- 创建：`runtime/host/darwin-arm64/build.lock.json`
- 创建：`scripts/runtime/packagehost/lock.go`
- 创建：`scripts/runtime/packagehost/lock_test.go`

- [ ] **步骤 1：编写 Host Build Lock 测试**

锁文件模型：

```go
type BuildLock struct {
	SchemaVersion int               `json:"schemaVersion"`
	HostPlatform  runtime.Platform  `json:"hostPlatform"`
	Components    []LockedComponent `json:"components"`
}

type LockedComponent struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Source       string   `json:"source"`
	Revision     string   `json:"revision,omitempty"`
	SHA256       string   `json:"sha256"`
	License      string   `json:"license"`
	LicenseFiles []string `json:"licenseFiles"`
}
```

要求组件名严格且按顺序为：

```text
qemu
glib
pixman
libslirp
zstd
gettext
pcre2
```

测试拒绝未知 schema、非 `darwin/arm64`、重复组件、空许可证、错误 SHA、空 LicenseFiles 和不符合固定组件顺序的输入。

- [ ] **步骤 2：运行测试确认红灯**

```bash
go test ./scripts/runtime/packagehost -run TestBuildLock -count=1
```

预期：FAIL，Package 和 Lock 模型尚不存在。

- [ ] **步骤 3：写入当前固定源码信息**

QEMU：

```text
version: v11.0.2
revision: e545d8bb9d63e9dd61542b88463183314cff9482
source: https://download.qemu.org/qemu-11.0.2.tar.xz
sha256: 3745f6ea88e2e87fe0dc838b2b1d4e0a770bf48e01a1d5a186842a1fff76ccf5
```

Homebrew 依赖固定为当前已验证版本与源码 SHA：

```text
glib 2.88.2      cf3f215a640c8a4257f14317586b8f1fdd25a10a93cb4bdda147c0f9ad88e74f
pixman 0.46.4    d09c44ebc3bd5bee7021c79f922fe8fb2fb57f7320f55e97ff9914d2346a591c
libslirp 4.9.3   ee698ca4ce05217ca7d520c7f0b1b1228fd7d32922dd32d1051c347152588417
zstd 1.5.7       37d7284556b20954e56e1ca85b80226768902e2edabd3b649e9e72c0c9012ee3
gettext 1.0      85d99b79c981a404874c02e0342176cf75c7698e2b51fe41031cf6526d974f1a
pcre2 10.47      47fe8c99461250d42f89e6e8fdaeba9da057855d06eb7fc08d9ca03fd08d7bc7
```

锁文件必须保存完整 Source URL、SPDX 表达式和源码包内 LicenseFiles 路径。

- [ ] **步骤 4：实现严格 LoadBuildLock**

```go
func LoadBuildLock(reader io.Reader) (BuildLock, error)
func (lock BuildLock) Validate() error
```

使用 `json.Decoder.DisallowUnknownFields()` 并拒绝尾随 JSON。

- [ ] **步骤 5：验证仓库锁文件**

```bash
gofmt -w scripts/runtime/packagehost/lock.go scripts/runtime/packagehost/lock_test.go
go test ./scripts/runtime/packagehost -run TestBuildLock -count=1
```

预期：PASS。

---

### 任务 4：构建并打包自包含 Darwin ARM QEMU

**文件：**

- 创建：`scripts/runtime/build-qemu-darwin-arm64.sh`
- 创建：`scripts/runtime/fetch-host-licenses.sh`
- 创建：`scripts/runtime/packagehost/main.go`
- 创建：`scripts/runtime/packagehost/macho.go`
- 创建：`scripts/runtime/packagehost/macho_test.go`
- 创建：`scripts/runtime/packagehost/runner.go`
- 创建：`scripts/runtime/packagehost/manifest.go`
- 修改：`internal/runtime/buildroot_test.go`

- [ ] **步骤 1：为 QEMU 构建参数编写红灯测试**

在 `internal/runtime/buildroot_test.go` 增加脚本约束测试，要求存在以下精确参数：

```text
--target-list=x86_64-softmmu
--enable-tcg
--enable-slirp
--disable-hvf
--disable-cocoa
--disable-gtk
--disable-sdl
--disable-docs
--disable-guest-agent
--disable-tools
--disable-user
--disable-bsd-user
--disable-linux-user
--disable-download
```

并要求脚本校验固定 QEMU Commit 和宿主 `Darwin/arm64`。

- [ ] **步骤 2：运行测试确认红灯**

```bash
go test ./internal/runtime -run TestDarwinARMQEMUBuildScript -count=1
```

预期：FAIL，脚本不存在。

- [ ] **步骤 3：实现最小 QEMU 构建脚本**

命令接口固定为：

```bash
./scripts/runtime/build-qemu-darwin-arm64.sh QEMU_SOURCE OUTPUT_DIR
```

脚本必须：

- 拒绝非 Darwin 或非 arm64。
- 使用 `git rev-parse HEAD` 校验固定 Commit。
- 拒绝已存在的 Output Dir。
- 明确选择可用 Python 路径参数，不联网安装 Python 包。
- 使用 `--disable-download`，缺少固定 subproject 时直接失败。
- 调用 `configure` 后检查 `config-host.mak` 和配置摘要。
- 只执行 `ninja qemu-system-x86_64`。
- 验证版本和 `-accel help` 严格只有 `tcg`。

- [ ] **步骤 4：编写 Mach-O 依赖图红灯测试**

使用 Fake Runner 返回固定 `otool -L` 输出：

```text
/tmp/qemu:
    /opt/homebrew/opt/glib/lib/libglib-2.0.0.dylib (...)
    /usr/lib/libSystem.B.dylib (...)

/opt/homebrew/opt/glib/lib/libglib-2.0.0.dylib:
    /opt/homebrew/opt/gettext/lib/libintl.8.dylib (...)
    /usr/lib/libSystem.B.dylib (...)
```

要求：

```go
graph, err := ResolveDependencies(ctx, runner, qemuPath)
```

只返回两个非系统 dylib 节点，保留原始 Install Name 与 `EvalSymlinks` 后 Source Path。

拒绝：

- `@rpath`、`@executable_path` 或无法解析的输入依赖。
- 两个不同文件具有相同 basename。
- 依赖逃出 `/opt/homebrew` 且不属于 `/usr/lib` 或 `/System/Library`。
- `otool` 失败。

- [ ] **步骤 5：运行依赖图测试确认红灯**

```bash
go test ./scripts/runtime/packagehost -run 'TestResolveDependencies|TestDependencyCollision' -count=1
```

预期：FAIL，Mach-O 解析实现不存在。

- [ ] **步骤 6：实现命令边界与依赖图**

```go
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type Dependency struct {
	InstallName string
	SourcePath  string
	BaseName    string
}

func ResolveDependencies(ctx context.Context, runner Runner, executable string) ([]Dependency, error)
func IsSystemDependency(installName string) bool
```

真实 Runner 使用 `exec.CommandContext`，错误必须包含命令阶段但不拼接未经清理的环境变量。

- [ ] **步骤 7：编写重定位命令红灯测试**

Fake Runner 记录命令，要求顺序：

1. strip QEMU 与 dylib。
2. 每个 dylib 设置 `-id @loader_path/<base>`。
3. QEMU 的依赖改为 `@loader_path/../lib/<base>`。
4. dylib 间依赖改为 `@loader_path/<base>`。
5. `codesign --force --sign -` 每个 dylib 和 QEMU。
6. `codesign --verify --strict`。
7. 再次 `otool -L`，拒绝任何非系统绝对路径。

- [ ] **步骤 8：实现 Host Payload 组装**

命令接口：

```bash
go run ./scripts/runtime/packagehost \
  --qemu ./out/qemu-darwin-arm64/qemu-system-x86_64 \
  --lock ./runtime/host/darwin-arm64/build.lock.json \
  --qemu-license-dir ./reference/qemu \
  --dependency-license-dir ./out/host-licenses \
  --output ./out/sealbuild-host-runtime-darwin-arm64.tar.zst
```

组装临时 payload：

```text
bin/qemu-system-x86_64
lib/*.dylib
licenses/qemu/*
licenses/<component>/*
```

使用任务 2 的 `artifact.Build` 生成 Manifest、checksums 和 tar.zst。Host Manifest 平台固定 `darwin/arm64`。

- [ ] **步骤 9：实现固定许可证获取脚本**

```bash
./scripts/runtime/fetch-host-licenses.sh \
  runtime/host/darwin-arm64/build.lock.json \
  out/host-license-sources \
  out/host-licenses
```

脚本必须使用 `jq` 结构化读取 Lock，并满足：

- 只访问 Lock 中的 Source URL。
- 先下载 `.tmp`，校验 SHA-256 后原子移动。
- 按 `licenseFiles` 精确提取，不使用模糊 `find` 选择第一个文件。
- 缺少任一许可证立即失败。
- 不尝试镜像 URL。

- [ ] **步骤 10：运行局部与全量测试**

```bash
gofmt -w scripts/runtime/packagehost
sh -n scripts/runtime/build-qemu-darwin-arm64.sh
sh -n scripts/runtime/fetch-host-licenses.sh
go test ./scripts/runtime/packagehost -count=1
go test ./internal/runtime -count=1
go test ./... -count=1
```

预期：全部 PASS。

---

### 任务 5：Guest 通过 fw_cfg 接收 mTLS 和代理

**文件：**

- 修改：`runtime/buildroot/board/sealbuild/x86_64/linux.config`
- 修改：`runtime/buildroot/board/sealbuild/x86_64/post-build.sh`
- 修改：`runtime/buildroot/board/sealbuild/x86_64/rootfs-overlay/etc/init.d/S50sealbuild-runtime`
- 修改：`runtime/buildroot/board/sealbuild/x86_64/rootfs-overlay/etc/buildkit/buildkitd.toml`
- 修改：`internal/runtime/buildroot_test.go`

- [ ] **步骤 1：编写 fw_cfg 与无共享私钥红灯测试**

新增测试要求 Kernel 配置包含：

```text
CONFIG_FW_CFG_SYSFS=y
CONFIG_FW_CFG_SYSFS_CMDLINE=y
```

要求 `post-build.sh`：

- 不引用 `SEALBUILD_TLS_DIR`。
- 不复制 `server.key`、`server.crt` 或 `ca.crt`。
- 预创建 `/var/lib/buildkit/runtime/tls` 挂载父目录所需路径。

要求 init 脚本读取：

```text
/sys/firmware/qemu_fw_cfg/by_name/opt/sealbuild/tls/ca.crt/raw
/sys/firmware/qemu_fw_cfg/by_name/opt/sealbuild/tls/server.crt/raw
/sys/firmware/qemu_fw_cfg/by_name/opt/sealbuild/tls/server.key/raw
```

并可选读取：

```text
/sys/firmware/qemu_fw_cfg/by_name/opt/sealbuild/proxy/url/raw
```

- [ ] **步骤 2：运行回归测试确认红灯**

```bash
go test ./internal/runtime -run 'TestGuestKernelEnablesFWCfg|TestGuestRootfsContainsNoTLSPrivateKey|TestGuestInitLoadsFWCfg' -count=1
```

预期：FAIL，当前 rootfs 仍复制 Spike 私钥。

- [ ] **步骤 3：实现最小 fw_cfg 加载逻辑**

init 脚本流程固定为：

```sh
runtime_dir="${state_dir}/runtime"
tls_dir="${runtime_dir}/tls"

install_fw_cfg() {
	name=$1
	destination=$2
	mode=$3
	source="/sys/firmware/qemu_fw_cfg/by_name/${name}/raw"
	[ -r "${source}" ] || fail fw-cfg
	install -m "${mode}" "${source}" "${destination}" || fail fw-cfg-copy
}
```

状态盘挂载后创建 `${tls_dir}`，复制 CA/Server Cert/Server Key，权限分别为 `0644/0644/0600`。

Proxy 项存在时读取并导出 `HTTP_PROXY`、`HTTPS_PROXY`、`http_proxy`、`https_proxy`；不存在时保持未设置，不写默认值。

BuildKit TOML 的 TLS 路径改为：

```text
/var/lib/buildkit/runtime/tls/server.crt
/var/lib/buildkit/runtime/tls/server.key
/var/lib/buildkit/runtime/tls/ca.crt
```

- [ ] **步骤 4：运行测试和 Shell 语法检查**

```bash
sh -n runtime/buildroot/board/sealbuild/x86_64/post-build.sh
sh -n runtime/buildroot/board/sealbuild/x86_64/rootfs-overlay/etc/init.d/S50sealbuild-runtime
go test ./internal/runtime -count=1
```

预期：PASS。

---

### 任务 6：把 Guest 状态盘迁移到 qcow2

**文件：**

- 修改：`scripts/runtime/build-guest.sh`
- 修改：`scripts/runtime/package-guest.sh`
- 创建：`scripts/runtime/collect-guest-licenses.sh`
- 修改：`runtime/manifest.lock.json`
- 修改：`internal/runtime/buildroot_test.go`

- [ ] **步骤 1：编写 qcow2 构建约束红灯测试**

测试要求：

- `build-guest.sh` 接受第三个参数 `QEMU_IMG`。
- 验证 `qemu-img --version` 包含 `11.0.2`。
- 创建 32 GiB raw ext4 临时盘。
- 使用固定参数转换为 qcow2。
- 最终 Artifact 只包含 `buildkit-state.qcow2`，不包含 `.ext4` 状态盘和 `tls/`。
- `package-guest.sh` 调用通用 Artifact Builder。
- Buildroot 标准包许可证来自 `make legal-info`，三个预编译外部包许可证来自固定源码归档。

- [ ] **步骤 2：运行测试确认红灯**

```bash
go test ./internal/runtime -run 'TestGuestBuildUsesPinnedQEMUImg|TestGuestArtifactUsesQCOW2' -count=1
```

预期：FAIL。

- [ ] **步骤 3：实现固定 qcow2 生成**

新接口：

```bash
./scripts/runtime/build-guest.sh BUILDROOT_DIR OUTPUT_DIR QEMU_IMG
```

核心命令：

```sh
truncate --size 32G "${raw_state_image}"
mkfs.ext4 -F -L sealbuild-state "${raw_state_image}"
"${qemu_img}" convert \
  -f raw \
  -O qcow2 \
  -o compat=1.1,lazy_refcounts=on \
  "${raw_state_image}" \
  "${state_image}.tmp"
mv "${state_image}.tmp" "${state_image}"
rm -f "${raw_state_image}"
```

生成后运行：

```bash
qemu-img info --output=json buildkit-state.qcow2
```

并使用结构化 JSON 检查 format 为 `qcow2`、virtual-size 为 `34359738368`。

- [ ] **步骤 4：锁定并收集 Guest 许可证**

从 `runtime/manifest.lock.json` 删除 Host 组件 `qemu`；QEMU 只由 Darwin Host Lock 管理。然后增加 3 个只用于许可证与对应源码交付的组件：

```text
buildkit-source
source: https://github.com/moby/buildkit/archive/refs/tags/v0.31.1.tar.gz
sha256: b733b9243017cb2b8f9cb1a6bd5125a2bde5680d4063412dbc159402bffbaf1e

runc-source
source: https://github.com/opencontainers/runc/archive/refs/tags/v1.5.1.tar.gz
sha256: 32286f18899a644ec7c1589688a9600ba54cc65264f23f1f5877ba214ca76e75

cni-plugins-source
source: https://github.com/containernetworking/plugins/archive/refs/tags/v1.9.1.tar.gz
sha256: 34bd82d47e981940751619c9cc44c095bb90bfcaf8d71865cbb822c37690a764
```

`build-guest.sh` 在完成 Buildroot 构建后运行：

```bash
make -C "${buildroot_dir}" \
  O="${build_output}" \
  BR2_EXTERNAL="${project_dir}/runtime/buildroot" \
  BR2_DL_DIR="${download_dir}" \
  legal-info
```

`collect-guest-licenses.sh`：

- 复制 `${build_output}/legal-info/licenses` 的完整内容。
- 只从 Runtime Lock 指定 URL 下载 3 个源码归档。
- 逐个校验上述 SHA-256。
- 提取源码树内所有 basename 为 `LICENSE`、`LICENSE.*`、`COPYING` 或 `COPYING.*` 的普通文件，并保留组件内相对路径。
- 拒绝 symlink、路径逃逸、空许可证集合和重复输出路径。
- 输出到 `${output_dir}/guest-licenses` 临时目录后原子重命名。

- [ ] **步骤 5：改造 Guest Artifact Builder**

Guest payload：

```text
bzImage
rootfs.ext4
buildkit-state.qcow2
manifest.lock.json
licenses/*
```

调用任务 2 的 Go Builder 生成 `manifest.json`、`checksums.txt` 和 `sealbuild-guest-runtime.tar.zst`。Guest Manifest 平台固定 `linux/amd64`。

- [ ] **步骤 6：验证脚本和全量测试**

```bash
sh -n scripts/runtime/build-guest.sh
sh -n scripts/runtime/package-guest.sh
sh -n scripts/runtime/collect-guest-licenses.sh
go test ./internal/runtime -count=1
go test ./... -count=1
```

预期：PASS。

---

### 任务 7：迁移 Smoke Test 和 Linux Runtime CI

**文件：**

- 修改：`scripts/runtime/smoke-guest.sh`
- 修改：`.github/workflows/runtime-spike.yml`
- 修改：`runtime/tls/README.md`
- 修改：`internal/runtime/buildroot_test.go`

- [ ] **步骤 1：编写 Smoke 参数与安全约束红灯测试**

Smoke 新接口：

```text
smoke-guest.sh QEMU BUILDKCTL ARTIFACT_DIR TLS_DIR OUTPUT_DIR HOST_PORT [PROXY_URL]
```

测试要求：

- 状态盘文件名和 drive format 为 qcow2。
- QEMU 通过 3 个 `-fw_cfg name=...,file=...` 注入 Server TLS。
- 可选 Proxy 只接受 `http/https` 且拒绝 userinfo、query、fragment。
- 回环 Proxy 转换为 `10.0.2.2` 后写入 `0600` 临时文件，再通过 fw_cfg 注入。
- buildctl 使用原始 Proxy 环境；Proxy 未提供时不设置环境。
- QEMU 参数和串口日志不包含 Proxy URL。
- 清理删除 Proxy 临时文件。

- [ ] **步骤 2：运行测试确认红灯**

```bash
go test ./internal/runtime -run TestSmokeGuestUsesFWCfgAndQCOW2 -count=1
```

预期：FAIL。

- [ ] **步骤 3：实现迁移后的 Smoke**

QEMU 关键参数：

```text
-drive file=<state>,format=qcow2,if=virtio
-fw_cfg name=opt/sealbuild/tls/ca.crt,file=<tls>/ca.crt
-fw_cfg name=opt/sealbuild/tls/server.crt,file=<tls>/server.crt
-fw_cfg name=opt/sealbuild/tls/server.key,file=<tls>/server.key
```

Smoke 仍使用 `generate-spike-certs.sh`，但证书只位于宿主 Smoke 输出，不进入 Guest Artifact。

- [ ] **步骤 4：更新 Linux Workflow 的固定 QEMU 工具构建**

Workflow 中固定 QEMU source/Commit 不变。配置时允许 Tools，并明确构建：

```bash
ninja -C build qemu-system-x86_64 qemu-img
```

Guest 构建传入：

```bash
./scripts/runtime/build-guest.sh \
  "$RUNNER_TEMP/buildroot" \
  "$RUNNER_TEMP/runtime-spike" \
  "$RUNNER_TEMP/qemu/build/qemu-img"
```

Smoke 前单独生成证书并传入 TLS Dir。

- [ ] **步骤 5：更新 Artifact 上传清单**

上传：

```text
sealbuild-guest-runtime.tar.zst
artifact/manifest.json
artifact/checksums.txt
serial.log
worker.json
measurements.txt
first-build.tar
cached-build.tar
```

明确不上传 Server/Client 私钥。

- [ ] **步骤 6：运行本地验证**

```bash
sh -n scripts/runtime/smoke-guest.sh
go test ./internal/runtime -count=1
go test ./... -count=1
```

```bash
GOBIN="$PWD/out/tools" go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
./out/tools/actionlint .github/workflows/runtime-spike.yml
```

预期：固定的 actionlint v1.7.7 退出 0，不使用未锁定的本机工具版本。

---

### 任务 8：执行 Darwin ARM Host Runtime 自包含验收

**文件：**

- 修改：`docs/runtime-spike-results.md`
- 修改：`README.md`

- [ ] **步骤 1：运行当前机器的 QEMU 固定构建**

```bash
./scripts/runtime/build-qemu-darwin-arm64.sh \
  ./reference/qemu \
  ./out/qemu-darwin-arm64-packaging
```

预期：版本 v11.0.2，accelerator 严格只有 `tcg`。

- [ ] **步骤 2：获取许可证并生成 Host Artifact**

```bash
./scripts/runtime/fetch-host-licenses.sh \
  ./runtime/host/darwin-arm64/build.lock.json \
  ./out/host-license-sources \
  ./out/host-licenses

go run ./scripts/runtime/packagehost \
  --qemu ./out/qemu-darwin-arm64-packaging/qemu-system-x86_64 \
  --lock ./runtime/host/darwin-arm64/build.lock.json \
  --qemu-license-dir ./reference/qemu \
  --dependency-license-dir ./out/host-licenses \
  --output ./out/sealbuild-host-runtime-darwin-arm64.tar.zst
```

- [ ] **步骤 3：解压到空目录并验证 Manifest**

使用项目 Artifact Reader 或标准 `bsdtar` 解压到新的 `out/host-runtime-smoke`，然后运行：

```bash
env -i \
  HOME="$HOME" \
  PATH=/usr/bin:/bin \
  ./out/host-runtime-smoke/bin/qemu-system-x86_64 --version

env -i \
  HOME="$HOME" \
  PATH=/usr/bin:/bin \
  ./out/host-runtime-smoke/bin/qemu-system-x86_64 -accel help
```

预期：不读取 `/opt/homebrew`，版本 v11.0.2，只有 `tcg`。

- [ ] **步骤 4：验证 Mach-O 闭包和签名**

```bash
otool -L ./out/host-runtime-smoke/bin/qemu-system-x86_64
codesign --verify --strict ./out/host-runtime-smoke/bin/qemu-system-x86_64
find ./out/host-runtime-smoke/lib -type f -name '*.dylib' -exec codesign --verify --strict {} \;
```

检查所有非系统依赖只使用 `@loader_path`，不存在 `/opt/homebrew`、`/usr/local` 或用户目录。

- [ ] **步骤 5：取得新 Guest Artifact 后执行真实 Smoke**

使用 Linux Runtime CI 产出的新 Guest Artifact：

```bash
./scripts/runtime/generate-spike-certs.sh ./out/runtime-smoke-tls

./scripts/runtime/smoke-guest.sh \
  ./out/host-runtime-smoke/bin/qemu-system-x86_64 \
  ./out/buildkit-darwin-arm64/buildctl \
  ./out/guest-runtime-smoke \
  ./out/runtime-smoke-tls \
  ./out/runtime-smoke-result \
  41240 \
  http://127.0.0.1:7890
```

预期：

- 唯一 AMD64 worker。
- 首次与缓存构建成功。
- 两个 OCI Archive 严格 `linux/amd64`。
- QEMU 退出后端口 41240 无 Listener。

- [ ] **步骤 6：记录真实大小和性能**

更新 `docs/runtime-spike-results.md`：

- Host Runtime 压缩/解压大小。
- QEMU 和每个 dylib 大小。
- Guest Runtime 新压缩大小。
- qcow2 逻辑/实际大小。
- 冷启动、首次构建、缓存构建。
- 无 Homebrew 环境验证结果。

README 只声明 Runtime 打包里程碑已完成，不声明最终 Sealbuild CLI 已可用。

- [ ] **步骤 7：运行完成前全量验证**

```bash
gofmt_files=$(gofmt -l ./cmd ./internal ./scripts/runtime)
test -z "$gofmt_files"
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/sealbuild
git diff --check
```

预期：全部退出 0。

- [ ] **步骤 8：核对阶段验收矩阵**

必须有直接证据证明：

```text
Host Artifact Manifest 有效
Guest Artifact Manifest 有效
Host QEMU 无 Homebrew 运行依赖
QEMU accelerator 只有 TCG
Guest Artifact 不包含任何 TLS 私钥
Guest 使用 fw_cfg TLS 启动
状态盘为 32 GiB qcow2
真实 Dockerfile Smoke 两次成功
OCI 平台严格 linux/amd64
所有测试通过
```

任一项缺少证据时，本计划保持未完成，不进入 Runtime 安装与 VM 生命周期计划。

---

## 规格覆盖自检

本计划覆盖产品规格的第一实现分期：

- Host Runtime 自包含和许可证：任务 3、4、8。
- Guest Runtime 无共享私钥和 fw_cfg：任务 5、7、8。
- qcow2 状态盘：任务 6、7、8。
- 严格 Manifest 与原子 Artifact：任务 1、2、4、6。
- 代理安全边界：任务 5、7。
- 无 Homebrew 验收：任务 8。
- 真实 AMD64 Smoke：任务 7、8。

Runtime 首次安装、产品级 mTLS 生成、VM 生命周期、BuildKit Go Client、Registry Push、Cache Clean 和最终单文件嵌入明确属于后续 3 个计划，本计划不创建占位实现。
