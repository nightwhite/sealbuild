#!/bin/sh

set -eu

target_dir=$1

install -d -m 0755 "${target_dir}/var/lib/buildkit"
install -d -m 0755 "${target_dir}/var/lib/cni"

rm -f "${target_dir}/etc/resolv.conf"
ln -s /run/resolv.conf "${target_dir}/etc/resolv.conf"
