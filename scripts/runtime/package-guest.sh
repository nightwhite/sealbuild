#!/bin/sh

set -eu

if [ "$#" -ne 1 ]; then
	printf 'usage: %s OUTPUT_DIR\n' "$0" >&2
	exit 2
fi

project_dir=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
output_dir=$1

for required_path in \
	"${output_dir}/buildroot/images/bzImage" \
	"${output_dir}/buildroot/images/rootfs.ext2" \
	"${output_dir}/buildkit-state.qcow2" \
	"${output_dir}/guest-licenses"; do
	if [ ! -e "${required_path}" ]; then
		printf 'required Runtime input is missing: %s\n' "${required_path}" >&2
		exit 1
	fi
done

cd "${project_dir}"
go run ./scripts/runtime/packageguest \
	--output-dir "${output_dir}" \
	--lock "${project_dir}/runtime/manifest.lock.json"
