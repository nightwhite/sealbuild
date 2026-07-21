#!/bin/sh

set -eu

expected_qemu_version=11.0.2
expected_python_version='Python 3.14.6'
setuptools_sha256=e147c0549f27767ba362f9da434eab9c5dc0045d5304feb602a0af001089fc51

if [ "$#" -ne 6 ]; then
	printf 'usage: %s HOST_ARCHITECTURE HOMEBREW_ROOT PYTHON SETUPTOOLS_WHEEL QEMU_SOURCE OUTPUT_DIR\n' "$0" >&2
	exit 2
fi

host_architecture=$1
homebrew_root=$2
python=$3
setuptools_wheel=$4
qemu_source=$5
requested_output_dir=$6

case "${host_architecture}" in
	arm64)
		expected_uname_architecture=arm64
		expected_lipo_architecture=arm64
		;;
	amd64)
		expected_uname_architecture=x86_64
		expected_lipo_architecture=x86_64
		;;
	*)
		printf 'Darwin Host architecture must be arm64 or amd64\n' >&2
		exit 1
		;;
esac

if ! [ "$(uname -s)" = Darwin ] || ! [ "$(uname -m)" = "${expected_uname_architecture}" ]; then
	printf 'Darwin QEMU build host is %s/%s, expected Darwin/%s\n' "$(uname -s)" "$(uname -m)" "${expected_uname_architecture}" >&2
	exit 1
fi
if [ ! -x "${homebrew_root}/bin/brew" ]; then
	printf 'required Homebrew executable is missing: %s\n' "${homebrew_root}/bin/brew" >&2
	exit 1
fi
actual_homebrew_root=$("${homebrew_root}/bin/brew" --prefix)
if [ "${actual_homebrew_root}" != "${homebrew_root}" ]; then
	printf 'Homebrew root is %s, expected %s\n' "${actual_homebrew_root}" "${homebrew_root}" >&2
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

qemu_source=$(CDPATH= cd -- "${qemu_source}" && pwd)
output_parent=$(CDPATH= cd -- "$(dirname "${requested_output_dir}")" && pwd)
output_dir="${output_parent}/$(basename "${requested_output_dir}")"

actual_version=$(tr -d '\r\n' <"${qemu_source}/VERSION")
if [ "${actual_version}" != "${expected_qemu_version}" ]; then
	printf 'QEMU source version is %s, expected %s\n' "${actual_version}" "${expected_qemu_version}" >&2
	exit 1
fi
if [ -e "${output_dir}" ]; then
	printf 'QEMU build output already exists: %s\n' "${output_dir}" >&2
	exit 1
fi

mkdir "${output_dir}"
bootstrap_dir="${output_dir}/bootstrap-python"
"${python}" -m venv --system-site-packages "${bootstrap_dir}"
bootstrap_python="${bootstrap_dir}/bin/python3"
"${bootstrap_python}" -m pip install --disable-pip-version-check --no-index --no-deps "${setuptools_wheel}"
(
	cd "${output_dir}"
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

ninja -C "${output_dir}" qemu-system-x86_64

qemu="${output_dir}/qemu-system-x86_64"
"${qemu}" --version | grep -F 'QEMU emulator version 11.0.2' >/dev/null
actual_lipo_architecture=$(lipo -archs "${qemu}")
if [ "${actual_lipo_architecture}" != "${expected_lipo_architecture}" ]; then
	printf 'QEMU Mach-O architecture is %s, expected %s\n' "${actual_lipo_architecture}" "${expected_lipo_architecture}" >&2
	exit 1
fi

accelerators=$("${qemu}" -accel help)
expected_accelerators='Accelerators supported in QEMU binary:
tcg'
if [ "${accelerators}" != "${expected_accelerators}" ]; then
	printf 'QEMU accelerator set is not TCG-only:\n%s\n' "${accelerators}" >&2
	exit 1
fi
printf '%s\n' "${accelerators}"
