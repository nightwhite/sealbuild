package dev

import (
	"os"
	"strings"
	"testing"
)

func TestBuildGuestDockerUsesPinnedAMD64LinuxEnvironment(t *testing.T) {
	dockerfile, err := os.ReadFile("runtime-builder.Dockerfile")
	if err != nil {
		t.Fatalf("ReadFile(runtime-builder.Dockerfile) error = %v", err)
	}
	script, err := os.ReadFile("build-guest-docker.sh")
	if err != nil {
		t.Fatalf("ReadFile(build-guest-docker.sh) error = %v", err)
	}

	for _, required := range []string{
		"FROM --platform=linux/amd64 golang:1.26.1-bookworm@sha256:09fb8a652cf7a990b714c46a9f0f5fd2d5bc2222d995166b91907c1c05b7d0e8",
		"bc", "build-essential", "cpio", "e2fsprogs", "git", "jq", "libelf-dev",
		"libglib2.0-dev", "libpixman-1-dev", "libslirp-dev", "ninja-build", "python3-venv", "python3-wheel", "rsync", "zstd",
	} {
		if !strings.Contains(string(dockerfile), required) {
			t.Errorf("runtime-builder.Dockerfile is missing %q", required)
		}
	}

	for _, required := range []string{
		"cb857ba4c87a93e5265a9e4a3f32071abf39e14a",
		"https://download.qemu.org/qemu-11.0.2.tar.xz",
		"3745f6ea88e2e87fe0dc838b2b1d4e0a770bf48e01a1d5a186842a1fff76ccf5",
		"sha256sum --check --status",
		"docker --context default buildx build --builder default --platform linux/amd64 --load",
		"docker --context default run --rm --platform linux/amd64",
		"docker --context default volume create",
		"docker --context default volume rm",
		`chown "$(id -u):$(id -g)" /output`,
		`--user "$(id -u):$(id -g)"`,
		`--volume "${runtime_volume}:/output"`,
		`--volume "${runtime_volume}:/runtime:ro"`,
		"go run ./scripts/dev/dockerproxy",
		"scripts/runtime/build-guest.sh",
		"qemu-img",
	} {
		if !strings.Contains(string(script), required) {
			t.Errorf("build-guest-docker.sh is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"/var/run/docker.sock",
		"${HTTP_PROXY", "${HTTPS_PROXY", "${http_proxy", "${https_proxy",
		"--privileged",
		"https://gitlab.com/qemu-project/qemu.git",
		"https://gitlab.com/qemu-project/keycodemapdb.git",
		`--volume "${output_dir}:/output"`,
	} {
		if strings.Contains(string(script), forbidden) {
			t.Errorf("build-guest-docker.sh contains forbidden fragment %q", forbidden)
		}
	}
}
