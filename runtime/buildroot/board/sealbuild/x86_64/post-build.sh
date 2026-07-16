#!/bin/sh

set -eu

target_dir=$1
: "${SEALBUILD_TLS_DIR:?SEALBUILD_TLS_DIR must point to generated spike certificates}"

install -d -m 0700 "${target_dir}/etc/buildkit/tls"
install -m 0644 "${SEALBUILD_TLS_DIR}/ca.crt" "${target_dir}/etc/buildkit/tls/ca.crt"
install -m 0644 "${SEALBUILD_TLS_DIR}/server.crt" "${target_dir}/etc/buildkit/tls/server.crt"
install -m 0600 "${SEALBUILD_TLS_DIR}/server.key" "${target_dir}/etc/buildkit/tls/server.key"

rm -f "${target_dir}/etc/resolv.conf"
ln -s /run/resolv.conf "${target_dir}/etc/resolv.conf"
