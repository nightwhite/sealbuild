#!/bin/sh

set -eu

if [ "$#" -ne 1 ]; then
	printf 'usage: %s OUTPUT_DIR\n' "$0" >&2
	exit 2
fi

project_dir=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
output_dir=$1
image_dir="${output_dir}/buildroot/images"
tls_dir="${output_dir}/tls"
artifact_dir="${output_dir}/artifact"
temporary_dir="${output_dir}/.artifact.tmp"

for required_file in \
	"${image_dir}/bzImage" \
	"${image_dir}/rootfs.ext4" \
	"${output_dir}/buildkit-state.ext4" \
	"${tls_dir}/ca.crt" \
	"${tls_dir}/client.crt" \
	"${tls_dir}/client.key"; do
	if [ ! -f "${required_file}" ]; then
		printf 'required Runtime file is missing: %s\n' "${required_file}" >&2
		exit 1
	fi
done

if [ -e "${artifact_dir}" ] || [ -e "${temporary_dir}" ]; then
	printf 'Runtime artifact output already exists under %s\n' "${output_dir}" >&2
	exit 1
fi

mkdir -p "${temporary_dir}/tls"
cp "${image_dir}/bzImage" "${temporary_dir}/bzImage"
cp "${image_dir}/rootfs.ext4" "${temporary_dir}/rootfs.ext4"
cp --sparse=always "${output_dir}/buildkit-state.ext4" "${temporary_dir}/buildkit-state.ext4"
cp "${project_dir}/runtime/manifest.lock.json" "${temporary_dir}/manifest.lock.json"
cp "${tls_dir}/ca.crt" "${temporary_dir}/tls/ca.crt"
cp "${tls_dir}/client.crt" "${temporary_dir}/tls/client.crt"
cp "${tls_dir}/client.key" "${temporary_dir}/tls/client.key"

(
	cd "${temporary_dir}"
	find . -type f ! -name checksums.txt -print0 | sort -z | xargs -0 sha256sum >checksums.txt
)

mv "${temporary_dir}" "${artifact_dir}"
tar --create --sparse --directory "${artifact_dir}" --zstd --file "${output_dir}/sealbuild-guest-runtime.tar.zst" .

compressed_bytes=$(wc -c <"${output_dir}/sealbuild-guest-runtime.tar.zst")
if [ "${compressed_bytes}" -gt 89128960 ]; then
	du -ah "${artifact_dir}" | sort -h >&2
	printf 'compressed Guest Runtime is %s bytes, limit is 89128960 bytes\n' "${compressed_bytes}" >&2
	exit 1
fi

du -sh "${artifact_dir}" "${output_dir}/sealbuild-guest-runtime.tar.zst"
