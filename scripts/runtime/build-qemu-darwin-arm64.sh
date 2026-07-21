#!/bin/sh

set -eu

expected_qemu_commit=e545d8bb9d63e9dd61542b88463183314cff9482
python=/opt/homebrew/bin/python3.14
expected_python_version='Python 3.14.6'
setuptools_wheel=/opt/homebrew/Cellar/python@3.14/3.14.6/Frameworks/Python.framework/Versions/3.14/lib/python3.14/test/wheeldata/setuptools-79.0.1-py3-none-any.whl
setuptools_sha256=e147c0549f27767ba362f9da434eab9c5dc0045d5304feb602a0af001089fc51

if [ "$#" -ne 2 ]; then
	printf 'usage: %s QEMU_SOURCE OUTPUT_DIR\n' "$0" >&2
	exit 2
fi

if ! [ "$(uname -s)" = Darwin ] || ! [ "$(uname -m)" = arm64 ]; then
	printf 'Darwin ARM QEMU builds require an Apple Silicon Mac\n' >&2
	exit 1
fi
if [ ! -x "${python}" ]; then
	printf 'required QEMU build Python is missing: %s\n' "${python}" >&2
	exit 1
fi
if [ "$("${python}" --version)" != "${expected_python_version}" ]; then
	printf 'QEMU build Python must be %s\n' "${expected_python_version}" >&2
	exit 1
fi
if [ ! -f "${setuptools_wheel}" ]; then
	printf 'required offline setuptools wheel is missing: %s\n' "${setuptools_wheel}" >&2
	exit 1
fi
printf '%s  %s\n' "${setuptools_sha256}" "${setuptools_wheel}" | shasum -a 256 --check --status

qemu_source=$(CDPATH= cd -- "$1" && pwd)
output_parent=$(CDPATH= cd -- "$(dirname "$2")" && pwd)
output_dir="${output_parent}/$(basename "$2")"
build_dir="${output_dir}"

actual_qemu_commit=$(git -C "${qemu_source}" rev-parse HEAD)
if [ "${actual_qemu_commit}" != "${expected_qemu_commit}" ]; then
	printf 'QEMU commit is %s, expected %s\n' "${actual_qemu_commit}" "${expected_qemu_commit}" >&2
	exit 1
fi
if [ -e "${output_dir}" ]; then
	printf 'QEMU build output already exists: %s\n' "${output_dir}" >&2
	exit 1
fi

mkdir "${build_dir}"
bootstrap_dir="${build_dir}/bootstrap-python"
"${python}" -m venv --system-site-packages "${bootstrap_dir}"
bootstrap_python="${bootstrap_dir}/bin/python3"
"${bootstrap_python}" -m pip install --disable-pip-version-check --no-index --no-deps "${setuptools_wheel}"
(
	cd "${build_dir}"
	"${qemu_source}/configure" \
		--python="${bootstrap_python}" \
		--target-list=x86_64-softmmu \
		--enable-tcg \
		--enable-slirp \
		--disable-hvf \
		--disable-cocoa \
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

ninja -C "${build_dir}" qemu-system-x86_64

qemu="${build_dir}/qemu-system-x86_64"
"${qemu}" --version | grep -F 'QEMU emulator version 11.0.2' >/dev/null
file "${qemu}" | grep -F 'Mach-O 64-bit executable arm64' >/dev/null

accelerators=$("${qemu}" -accel help)
expected_accelerators='Accelerators supported in QEMU binary:
tcg'
if [ "${accelerators}" != "${expected_accelerators}" ]; then
	printf 'QEMU accelerator set is not TCG-only:\n%s\n' "${accelerators}" >&2
	exit 1
fi
printf '%s\n' "${accelerators}"
