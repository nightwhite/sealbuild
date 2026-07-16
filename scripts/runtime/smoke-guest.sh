#!/bin/sh

set -eu

if [ "$#" -ne 5 ]; then
	printf 'usage: %s QEMU BUILDKCTL ARTIFACT_DIR OUTPUT_DIR HOST_PORT\n' "$0" >&2
	exit 2
fi

project_dir=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
qemu=$1
buildctl=$2
artifact_dir=$3
output_dir=$4
host_port=$5
serial_log="${output_dir}/serial.log"
state_image="${output_dir}/buildkit-state.ext4"
worker_json="${output_dir}/worker.json"
results_file="${output_dir}/measurements.txt"

case "${host_port}" in
	''|*[!0-9]*)
		printf 'HOST_PORT must be a decimal TCP port\n' >&2
		exit 2
		;;
esac
if [ "${host_port}" -lt 1024 ] || [ "${host_port}" -gt 65535 ]; then
	printf 'HOST_PORT must be between 1024 and 65535\n' >&2
	exit 2
fi

for required_file in \
	"${qemu}" \
	"${buildctl}" \
	"${artifact_dir}/bzImage" \
	"${artifact_dir}/rootfs.ext4" \
	"${artifact_dir}/buildkit-state.ext4" \
	"${artifact_dir}/tls/ca.crt" \
	"${artifact_dir}/tls/client.crt" \
	"${artifact_dir}/tls/client.key"; do
	if [ ! -f "${required_file}" ]; then
		printf 'required smoke input is missing: %s\n' "${required_file}" >&2
		exit 1
	fi
done

if [ -e "${output_dir}" ]; then
	printf 'smoke output already exists: %s\n' "${output_dir}" >&2
	exit 1
fi
mkdir -p "${output_dir}"
cp --sparse=always "${artifact_dir}/buildkit-state.ext4" "${state_image}"

boot_started=$(date +%s)
"${qemu}" \
	-accel tcg,thread=multi \
	-machine q35 \
	-cpu max \
	-smp 2 \
	-m 2048 \
	-nodefaults \
	-no-reboot \
	-nographic \
	-kernel "${artifact_dir}/bzImage" \
	-append 'root=/dev/vda ro console=ttyS0,115200 panic=1' \
	-drive "file=${artifact_dir}/rootfs.ext4,format=raw,if=virtio,readonly=on" \
	-drive "file=${state_image},format=raw,if=virtio" \
	-netdev "user,id=net0,hostfwd=tcp:127.0.0.1:${host_port}-:1234" \
	-device virtio-net-pci,netdev=net0 \
	-serial "file:${serial_log}" &
qemu_pid=$!

cleanup() {
	kill "${qemu_pid}" 2>/dev/null || true
	wait "${qemu_pid}" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

attempt=1
while [ "${attempt}" -le 300 ]; do
	if ! kill -0 "${qemu_pid}" 2>/dev/null; then
		cat "${serial_log}" >&2
		printf 'QEMU exited before Guest Runtime became ready\n' >&2
		exit 1
	fi
	if grep -q 'SEALBUILD_RUNTIME_FAILED' "${serial_log}" 2>/dev/null; then
		cat "${serial_log}" >&2
		printf 'Guest Runtime reported startup failure\n' >&2
		exit 1
	fi
	if grep -q 'SEALBUILD_RUNTIME_READY' "${serial_log}" 2>/dev/null; then
		break
	fi
	attempt=$((attempt + 1))
	sleep 1
done
if [ "${attempt}" -gt 300 ]; then
	cat "${serial_log}" >&2
	printf 'Guest Runtime readiness timed out after 300 seconds\n' >&2
	exit 1
fi
boot_finished=$(date +%s)

set -- \
	--addr "tcp://127.0.0.1:${host_port}" \
	--tlsservername sealbuild-runtime \
	--tlscacert "${artifact_dir}/tls/ca.crt" \
	--tlscert "${artifact_dir}/tls/client.crt" \
	--tlskey "${artifact_dir}/tls/client.key"

"${buildctl}" "$@" debug workers --format '{{json .}}' >"${worker_json}"
jq --exit-status '
  length == 1 and
  .[0].platforms == [{"architecture":"amd64","os":"linux"}]
' "${worker_json}" >/dev/null

first_started=$(date +%s)
"${buildctl}" "$@" build \
	--frontend dockerfile.v0 \
	--local "context=${project_dir}/runtime/smoke" \
	--local "dockerfile=${project_dir}/runtime/smoke" \
	--opt platform=linux/amd64 \
	--opt provenance=false \
	--output "type=oci,dest=${output_dir}/first-build.tar"
first_finished=$(date +%s)

cached_started=$(date +%s)
"${buildctl}" "$@" build \
	--frontend dockerfile.v0 \
	--local "context=${project_dir}/runtime/smoke" \
	--local "dockerfile=${project_dir}/runtime/smoke" \
	--opt platform=linux/amd64 \
	--opt provenance=false \
	--output "type=oci,dest=${output_dir}/cached-build.tar"
cached_finished=$(date +%s)

go run "${project_dir}/scripts/runtime/inspect-oci-platform.go" "${output_dir}/first-build.tar"
go run "${project_dir}/scripts/runtime/inspect-oci-platform.go" "${output_dir}/cached-build.tar"

{
	printf 'cold_boot_seconds=%s\n' "$((boot_finished - boot_started))"
	printf 'first_build_seconds=%s\n' "$((first_finished - first_started))"
	printf 'cached_build_seconds=%s\n' "$((cached_finished - cached_started))"
	wc -c "${output_dir}/first-build.tar" "${output_dir}/cached-build.tar"
} | tee "${results_file}"
