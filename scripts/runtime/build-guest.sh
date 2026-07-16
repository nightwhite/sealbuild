#!/bin/sh

set -eu

expected_buildroot_commit=cb857ba4c87a93e5265a9e4a3f32071abf39e14a
linux_version=6.18.7
linux_sha256=b726a4d15cf9ae06219b56d87820776e34d89fbc137e55fb54a9b9c3015b8f1e

if [ "$#" -ne 2 ]; then
	printf 'usage: %s BUILDROOT_DIR OUTPUT_DIR\n' "$0" >&2
	exit 2
fi

if [ "$(uname -s)" != Linux ]; then
	printf 'Guest Runtime builds require a Linux host\n' >&2
	exit 1
fi

project_dir=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
buildroot_dir=$(CDPATH= cd -- "$1" && pwd)
output_dir=$2
build_output="${output_dir}/buildroot"
download_dir="${output_dir}/downloads"
tls_dir="${output_dir}/tls"

actual_buildroot_commit=$(git -C "${buildroot_dir}" rev-parse HEAD)
if [ "${actual_buildroot_commit}" != "${expected_buildroot_commit}" ]; then
	printf 'Buildroot commit is %s, expected %s\n' "${actual_buildroot_commit}" "${expected_buildroot_commit}" >&2
	exit 1
fi

mkdir -p "${download_dir}/linux"
linux_archive="${download_dir}/linux/linux-${linux_version}.tar.xz"
if [ ! -f "${linux_archive}" ]; then
	curl --fail --location --output "${linux_archive}.tmp" \
		"https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-${linux_version}.tar.xz"
	mv "${linux_archive}.tmp" "${linux_archive}"
fi
printf '%s  %s\n' "${linux_sha256}" "${linux_archive}" | sha256sum --check --status

"${project_dir}/scripts/runtime/generate-spike-certs.sh" "${tls_dir}"

make -C "${buildroot_dir}" \
	O="${build_output}" \
	BR2_EXTERNAL="${project_dir}/runtime/buildroot" \
	BR2_DL_DIR="${download_dir}" \
	sealbuild_x86_64_defconfig

if grep -q '^BR2_PACKAGE_RUNC=y$' "${build_output}/.config" || \
	grep -q '^BR2_PACKAGE_MOBY_BUILDKIT=y$' "${build_output}/.config"; then
	printf 'Buildroot selected an unpinned built-in runtime package\n' >&2
	exit 1
fi

SEALBUILD_TLS_DIR="${tls_dir}" make -C "${buildroot_dir}" \
	O="${build_output}" \
	BR2_EXTERNAL="${project_dir}/runtime/buildroot" \
	BR2_DL_DIR="${download_dir}"

state_image="${output_dir}/buildkit-state.ext4"
truncate --size 4G "${state_image}"
mkfs.ext4 -F -L sealbuild-state "${state_image}"

"${project_dir}/scripts/runtime/package-guest.sh" "${output_dir}"
