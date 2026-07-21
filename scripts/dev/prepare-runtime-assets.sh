#!/bin/sh

set -eu

if [ "$#" -ne 2 ]; then
	printf 'usage: %s HOST_ARCHIVE GUEST_ARCHIVE\n' "$0" >&2
	exit 2
fi

project_dir=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
runtimeassets_dir="${project_dir}/internal/runtimeassets"
generated_dir="${project_dir}/internal/runtimeassets/generated"

if [ -e "${generated_dir}" ]; then
	printf 'embedded Runtime asset directory already exists: %s\n' "${generated_dir}" >&2
	exit 1
fi

cd "${project_dir}"
go run ./scripts/dev/verify-runtime --host "$1" --guest "$2"

temporary_dir=$(mktemp -d "${runtimeassets_dir}/.generated.XXXXXX")
cleanup() {
	if [ -e "${temporary_dir}" ]; then
		rm -r "${temporary_dir}"
	fi
}
trap cleanup EXIT HUP INT TERM

cp "$1" "${temporary_dir}/host.tar.zst"
cp "$2" "${temporary_dir}/guest.tar.zst"
chmod 0644 "${temporary_dir}/host.tar.zst" "${temporary_dir}/guest.tar.zst"
mv "${temporary_dir}" "${generated_dir}"
