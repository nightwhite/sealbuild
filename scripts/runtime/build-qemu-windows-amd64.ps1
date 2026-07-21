param(
    [Parameter(Mandatory = $true)]
    [string]$Msys2Root,

    [Parameter(Mandatory = $true)]
    [string]$QemuSource,

    [Parameter(Mandatory = $true)]
    [string]$OutputDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$expectedRevision = 'e545d8bb9d63e9dd61542b88463183314cff9482'
$bash = Join-Path $Msys2Root 'usr\bin\bash.exe'
if (-not (Test-Path -LiteralPath $bash -PathType Leaf)) {
    throw "MSYS2 bash is missing: $bash"
}

$actualRevision = (& git -C $QemuSource rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or $actualRevision -ne $expectedRevision) {
    throw "QEMU revision is $actualRevision, expected $expectedRevision"
}
if (Test-Path -LiteralPath $OutputDirectory) {
    throw "QEMU output already exists: $OutputDirectory"
}

$script = @'
set -eu
export MSYSTEM=CLANG64
export CHERE_INVOKING=1
source /etc/profile
source_dir=$(cygpath -u "$1")
output_dir=$(cygpath -u "$2")
mkdir -p "$output_dir"
cd "$output_dir"
"$source_dir/configure" \
  --target-list=x86_64-softmmu \
  --enable-tcg \
  --enable-slirp \
  --enable-zstd \
  --enable-strip \
  --disable-modules \
  --disable-whpx \
  --disable-kvm \
  --disable-hvf \
  --disable-gtk \
  --disable-sdl \
  --disable-opengl \
  --disable-docs \
  --disable-guest-agent \
  --disable-tools \
  --disable-user \
  --disable-bsd-user \
  --disable-linux-user \
  --disable-download
ninja qemu-system-x86_64.exe
./qemu-system-x86_64.exe --version | grep -F 'QEMU emulator version 11.0.2'
accelerators=$(./qemu-system-x86_64.exe -accel help)
expected='Accelerators supported in QEMU binary:
tcg'
test "$accelerators" = "$expected"
printf '%s\n' "$accelerators"
'@

& $bash -lc $script -- $QemuSource $OutputDirectory
if ($LASTEXITCODE -ne 0) {
    throw "QEMU Windows build failed with exit code $LASTEXITCODE"
}
