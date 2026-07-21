#!/bin/sh

set -eu

expected_qemu_commit=e545d8bb9d63e9dd61542b88463183314cff9482

if [ "$#" -ne 2 ]; then
	printf 'usage: %s QEMU_SOURCE OUTPUT_DIR\n' "$0" >&2
	exit 2
fi

if ! [ "$(uname -s)" = Linux ] || ! [ "$(uname -m)" = x86_64 ]; then
	printf 'Linux AMD64 QEMU builds require a Linux x86_64 host\n' >&2
	exit 1
fi

qemu_source=$(CDPATH= cd -- "$1" && pwd)
output_parent=$(CDPATH= cd -- "$(dirname "$2")" && pwd)
output_dir="${output_parent}/$(basename "$2")"

actual_qemu_commit=$(git -C "${qemu_source}" rev-parse HEAD)
if [ "${actual_qemu_commit}" != "${expected_qemu_commit}" ]; then
	printf 'QEMU commit is %s, expected %s\n' "${actual_qemu_commit}" "${expected_qemu_commit}" >&2
	exit 1
fi
if [ -e "${output_dir}" ]; then
	printf 'QEMU build output already exists: %s\n' "${output_dir}" >&2
	exit 1
fi

mkdir "${output_dir}"
(
	cd "${output_dir}"
	"${qemu_source}/configure" \
		--target-list=x86_64-softmmu \
		--enable-tcg \
		--enable-slirp \
		--disable-kvm \
		--disable-xen \
		--disable-gtk \
		--disable-sdl \
		--disable-docs \
		--disable-guest-agent \
		--disable-tools \
		--disable-user \
		--disable-bsd-user \
		--disable-linux-user \
		--disable-download
)

ninja -C "${output_dir}" qemu-system-x86_64

qemu="${output_dir}/qemu-system-x86_64"
strip --strip-unneeded "${qemu}"
"${qemu}" --version | grep -F 'QEMU emulator version 11.0.2' >/dev/null
file "${qemu}" | grep -F 'ELF 64-bit LSB pie executable, x86-64' >/dev/null

accelerators=$("${qemu}" -accel help)
expected_accelerators='Accelerators supported in QEMU binary:
tcg'
if [ "${accelerators}" != "${expected_accelerators}" ]; then
	printf 'QEMU accelerator set is not TCG-only:\n%s\n' "${accelerators}" >&2
	exit 1
fi
printf '%s\n' "${accelerators}"
