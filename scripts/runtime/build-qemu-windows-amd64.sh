#!/bin/sh

set -eu

if [ "$#" -ne 2 ]; then
	printf 'usage: %s QEMU_SOURCE OUTPUT_DIR\n' "$0" >&2
	exit 2
fi
if [ "${MSYSTEM:-}" != CLANG64 ]; then
	printf 'Windows QEMU build requires the MSYS2 CLANG64 shell\n' >&2
	exit 1
fi

source_dir=$(cygpath -u "$1")
output_dir=$(cygpath -u "$2")
actual_version=$(tr -d '\r\n' <"${source_dir}/VERSION")
if [ "${actual_version}" != 11.0.2 ]; then
	printf 'QEMU source version is %s, expected 11.0.2\n' "${actual_version}" >&2
	exit 1
fi
if [ -e "${output_dir}" ]; then
	printf 'QEMU output already exists: %s\n' "${output_dir}" >&2
	exit 1
fi

mkdir -p "${output_dir}"
cd "${output_dir}"
"${source_dir}/configure" \
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
accelerators=$(./qemu-system-x86_64.exe -accel help | tr -d '\r')
expected_accelerators='Accelerators supported in QEMU binary:
tcg'
if [ "${accelerators}" != "${expected_accelerators}" ]; then
	printf 'QEMU accelerator set is not TCG-only:\n%s\n' "${accelerators}" >&2
	exit 1
fi
printf '%s\n' "${accelerators}"
