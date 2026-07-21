#!/bin/sh

set -eu

if [ "$#" -ne 4 ]; then
	printf 'usage: %s RUNTIME_LOCK BUILDROOT_LICENSES SOURCE_DIR OUTPUT_DIR\n' "$0" >&2
	exit 2
fi

project_dir=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "${project_dir}"
go run ./scripts/runtime/collectguest \
	--lock "$1" \
	--buildroot-licenses "$2" \
	--source-dir "$3" \
	--output "$4"
