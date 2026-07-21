#!/bin/sh

set -eu

if [ "$#" -lt 6 ] || [ "$#" -gt 7 ]; then
	printf 'usage: %s QEMU BUILDKCTL ARTIFACT_DIR TLS_DIR OUTPUT_DIR HOST_PORT [PROXY_URL]\n' "$0" >&2
	exit 2
fi

project_dir=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
qemu=$1
buildctl=$2
artifact_dir=$3
tls_dir=$4
output_dir=$5
host_port=$6
proxy_configured=false
proxy_url=
if [ "$#" -eq 7 ]; then
	proxy_url=$7
	if [ -z "${proxy_url}" ]; then
		printf 'PROXY_URL must not be empty\n' >&2
		exit 2
	fi
	proxy_configured=true
fi

serial_log="${output_dir}/serial.log"
state_image="${output_dir}/buildkit-state.qcow2"
worker_json="${output_dir}/worker.json"
results_file="${output_dir}/measurements.txt"
proxy_file=

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
	"${artifact_dir}/buildkit-state.qcow2" \
	"${tls_dir}/ca.crt" \
	"${tls_dir}/server.crt" \
	"${tls_dir}/server.key" \
	"${tls_dir}/client.crt" \
	"${tls_dir}/client.key"; do
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
cp "${artifact_dir}/buildkit-state.qcow2" "${state_image}"

if [ "${proxy_configured}" = true ]; then
	proxy_file=$(mktemp "${output_dir}/.proxy.XXXXXX")
	chmod 0600 "${proxy_file}"
	printf '%s' "${proxy_url}" | go run "${project_dir}/scripts/runtime/inspect.go" proxy "${proxy_file}"
fi

qemu_pid=
cleanup() {
	if [ -n "${qemu_pid}" ]; then
		kill "${qemu_pid}" 2>/dev/null || true
		wait "${qemu_pid}" 2>/dev/null || true
	fi
	if [ -n "${proxy_file}" ]; then
		rm -f "${proxy_file}"
	fi
}
trap cleanup EXIT INT TERM

set -- \
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
	-drive "file=${state_image},format=qcow2,if=virtio" \
	-netdev "user,id=net0,hostfwd=tcp:127.0.0.1:${host_port}-:1234" \
	-device virtio-net-pci,netdev=net0 \
	-fw_cfg "name=opt/sealbuild/tls/ca.crt,file=${tls_dir}/ca.crt" \
	-fw_cfg "name=opt/sealbuild/tls/server.crt,file=${tls_dir}/server.crt" \
	-fw_cfg "name=opt/sealbuild/tls/server.key,file=${tls_dir}/server.key" \
	-serial "file:${serial_log}"
if [ "${proxy_configured}" = true ]; then
	set -- "$@" -fw_cfg "name=opt/sealbuild/proxy/url,file=${proxy_file}"
fi

boot_started=$(date +%s)
"${qemu}" "$@" &
qemu_pid=$!

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

run_buildctl() {
	if [ "${proxy_configured}" = true ]; then
		HTTP_PROXY="${proxy_url}" \
		HTTPS_PROXY="${proxy_url}" \
		http_proxy="${proxy_url}" \
		https_proxy="${proxy_url}" \
		"${buildctl}" "$@"
	else
		"${buildctl}" "$@"
	fi
}

set -- \
	--addr "tcp://127.0.0.1:${host_port}" \
	--tlsservername sealbuild-runtime \
	--tlscacert "${tls_dir}/ca.crt" \
	--tlscert "${tls_dir}/client.crt" \
	--tlskey "${tls_dir}/client.key"

run_buildctl "$@" debug workers --format '{{json .}}' >"${worker_json}"
go run "${project_dir}/scripts/runtime/inspect.go" worker "${worker_json}"

first_started=$(date +%s)
run_buildctl "$@" build \
	--frontend dockerfile.v0 \
	--local "context=${project_dir}/runtime/smoke" \
	--local "dockerfile=${project_dir}/runtime/smoke" \
	--opt platform=linux/amd64 \
	--opt provenance=false \
	--output "type=oci,dest=${output_dir}/first-build.tar"
first_finished=$(date +%s)

cached_started=$(date +%s)
run_buildctl "$@" build \
	--frontend dockerfile.v0 \
	--local "context=${project_dir}/runtime/smoke" \
	--local "dockerfile=${project_dir}/runtime/smoke" \
	--opt platform=linux/amd64 \
	--opt provenance=false \
	--output "type=oci,dest=${output_dir}/cached-build.tar"
cached_finished=$(date +%s)

go run "${project_dir}/scripts/runtime/inspect.go" oci "${output_dir}/first-build.tar"
go run "${project_dir}/scripts/runtime/inspect.go" oci "${output_dir}/cached-build.tar"

{
	printf 'cold_boot_seconds=%s\n' "$((boot_finished - boot_started))"
	printf 'first_build_seconds=%s\n' "$((first_finished - first_started))"
	printf 'cached_build_seconds=%s\n' "$((cached_finished - cached_started))"
	wc -c "${output_dir}/first-build.tar" "${output_dir}/cached-build.tar"
} | tee "${results_file}"
