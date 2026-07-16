#!/bin/sh

set -eu

if [ "$#" -ne 1 ]; then
	printf 'usage: %s OUTPUT_DIR\n' "$0" >&2
	exit 2
fi

output_dir=$1
parent_dir=$(dirname "${output_dir}")
base_name=$(basename "${output_dir}")
mkdir -p "${parent_dir}"

if [ -e "${output_dir}" ]; then
	printf 'certificate output already exists: %s\n' "${output_dir}" >&2
	exit 1
fi

temporary_dir=$(mktemp -d "${parent_dir}/.${base_name}.tmp.XXXXXX")
trap 'rm -rf "${temporary_dir}"' EXIT INT TERM

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "${temporary_dir}/ca.key"
openssl req -x509 -new -sha256 -days 30 \
	-key "${temporary_dir}/ca.key" \
	-subj '/CN=sealbuild-spike-ca' \
	-out "${temporary_dir}/ca.crt"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "${temporary_dir}/server.key"
openssl req -new -sha256 \
	-key "${temporary_dir}/server.key" \
	-subj '/CN=sealbuild-runtime' \
	-out "${temporary_dir}/server.csr"
cat >"${temporary_dir}/server.ext" <<'EOF'
basicConstraints=CA:FALSE
keyUsage=digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=DNS:sealbuild-runtime,IP:10.0.2.15
EOF
openssl x509 -req -sha256 -days 30 \
	-in "${temporary_dir}/server.csr" \
	-CA "${temporary_dir}/ca.crt" \
	-CAkey "${temporary_dir}/ca.key" \
	-CAcreateserial \
	-extfile "${temporary_dir}/server.ext" \
	-out "${temporary_dir}/server.crt"

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "${temporary_dir}/client.key"
openssl req -new -sha256 \
	-key "${temporary_dir}/client.key" \
	-subj '/CN=sealbuild-spike-client' \
	-out "${temporary_dir}/client.csr"
cat >"${temporary_dir}/client.ext" <<'EOF'
basicConstraints=CA:FALSE
keyUsage=digitalSignature,keyEncipherment
extendedKeyUsage=clientAuth
EOF
openssl x509 -req -sha256 -days 30 \
	-in "${temporary_dir}/client.csr" \
	-CA "${temporary_dir}/ca.crt" \
	-CAkey "${temporary_dir}/ca.key" \
	-CAcreateserial \
	-extfile "${temporary_dir}/client.ext" \
	-out "${temporary_dir}/client.crt"

rm -f \
	"${temporary_dir}"/*.csr \
	"${temporary_dir}"/*.ext \
	"${temporary_dir}"/*.srl \
	"${temporary_dir}/ca.key"
chmod 0600 "${temporary_dir}"/*.key
chmod 0644 "${temporary_dir}"/*.crt
mv "${temporary_dir}" "${output_dir}"
trap - EXIT INT TERM
