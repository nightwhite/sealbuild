#!/bin/sh

set -eu

buildroot_commit=cb857ba4c87a93e5265a9e4a3f32071abf39e14a
qemu_url=https://download.qemu.org/qemu-11.0.2.tar.xz
qemu_sha256=3745f6ea88e2e87fe0dc838b2b1d4e0a770bf48e01a1d5a186842a1fff76ccf5
builder_image=sealbuild-runtime-builder:go1.26.1-bookworm-amd64

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	printf 'usage: %s OUTPUT_DIR [PROXY_URL]\n' "$0" >&2
	exit 2
fi

project_dir=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
output_parent=$(CDPATH= cd -- "$(dirname "$1")" && pwd)
output_dir="${output_parent}/$(basename "$1")"
if [ -e "${output_dir}" ]; then
	printf 'Docker Guest Runtime output already exists: %s\n' "${output_dir}" >&2
	exit 1
fi

docker_proxy=
if [ "$#" -eq 2 ]; then
	docker_proxy=$(cd "${project_dir}" && go run ./scripts/dev/dockerproxy "$2")
fi

docker --context default buildx build --builder default --platform linux/amd64 --load \
	--build-arg "DEV_PROXY=${docker_proxy}" \
	--tag "${builder_image}" \
	--file "${project_dir}/scripts/dev/runtime-builder.Dockerfile" \
	"${project_dir}/scripts/dev"

mkdir "${output_dir}"
runtime_volume=$(docker --context default volume create)
cleanup_volume() {
	docker --context default volume rm "${runtime_volume}" >/dev/null
}
trap cleanup_volume EXIT HUP INT TERM

docker --context default run --rm --platform linux/amd64 \
	--volume "${runtime_volume}:/output" \
	"${builder_image}" \
	chown "$(id -u):$(id -g)" /output

docker --context default run --rm --platform linux/amd64 \
	--user "$(id -u):$(id -g)" \
	--env "HOME=/output/.home" \
	--env "DEV_PROXY=${docker_proxy}" \
	--env "QEMU_URL=${qemu_url}" \
	--env "QEMU_SHA256=${qemu_sha256}" \
	--volume "${project_dir}:/workspace:ro" \
	--volume "${runtime_volume}:/output" \
	--workdir /workspace \
	"${builder_image}" \
	/bin/sh -eu -c '
		if [ -n "${DEV_PROXY}" ]; then
			export HTTP_PROXY="${DEV_PROXY}" HTTPS_PROXY="${DEV_PROXY}"
			export http_proxy="${DEV_PROXY}" https_proxy="${DEV_PROXY}"
		fi
		mkdir -p "${HOME}"
		git init /output/.buildroot-source
		git -C /output/.buildroot-source remote add origin https://github.com/buildroot/buildroot.git
		git -C /output/.buildroot-source fetch --depth 1 origin '"${buildroot_commit}"'
		git -C /output/.buildroot-source checkout --detach FETCH_HEAD
		curl --fail --location --output /output/qemu-11.0.2.tar.xz.tmp "${QEMU_URL}"
		printf "%s  %s\n" "${QEMU_SHA256}" /output/qemu-11.0.2.tar.xz.tmp | sha256sum --check --status
		mv /output/qemu-11.0.2.tar.xz.tmp /output/qemu-11.0.2.tar.xz
		mkdir /output/.qemu-source
		tar -xJf /output/qemu-11.0.2.tar.xz --strip-components=1 -C /output/.qemu-source
		cd /output/.qemu-source
		./configure \
			--target-list=x86_64-softmmu \
			--enable-slirp \
			--disable-docs \
			--disable-guest-agent \
			--disable-user \
			--disable-download
		ninja -C build qemu-img
		build/qemu-img --version | grep -F "version 11.0.2"
		cd /workspace
		./scripts/runtime/build-guest.sh \
			/output/.buildroot-source \
			/output \
			/output/.qemu-source/build/qemu-img
	'

docker --context default run --rm --platform linux/amd64 \
	--volume "${runtime_volume}:/runtime:ro" \
	--volume "${output_dir}:/export" \
	"${builder_image}" \
	/bin/sh -eu -c '
		cp /runtime/sealbuild-guest-runtime.tar.zst /export/
		cp -R /runtime/artifact /export/artifact
	'
