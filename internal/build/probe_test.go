package build

import (
	"context"
	"errors"
	"strings"
	"testing"

	buildkit "github.com/moby/buildkit/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/labring/sealbuild/internal/tlsmaterial"
)

func TestValidateWorkersAcceptsOneLinuxAMD64Worker(t *testing.T) {
	workers := []*buildkit.WorkerInfo{{
		ID: "worker0",
		Platforms: []ocispec.Platform{
			{OS: "linux", Architecture: "amd64"},
			{OS: "linux", Architecture: "amd64", Variant: "v2"},
			{OS: "linux", Architecture: "amd64", Variant: "v3"},
		},
	}}
	if err := ValidateWorkers(workers); err != nil {
		t.Fatalf("ValidateWorkers() error = %v", err)
	}
}

func TestValidateWorkersRejectsInvalidWorkerSets(t *testing.T) {
	tests := []struct {
		name      string
		workers   []*buildkit.WorkerInfo
		wantError string
	}{
		{name: "zero workers", wantError: "exactly one"},
		{name: "multiple workers", workers: []*buildkit.WorkerInfo{{ID: "one"}, {ID: "two"}}, wantError: "exactly one"},
		{name: "variant without baseline", workers: workersWithPlatforms(ocispec.Platform{OS: "linux", Architecture: "amd64", Variant: "v2"}), wantError: "baseline linux/amd64"},
		{name: "ARM platform", workers: workersWithPlatforms(ocispec.Platform{OS: "linux", Architecture: "amd64"}, ocispec.Platform{OS: "linux", Architecture: "arm64"}), wantError: "unsupported platform linux/arm64"},
		{name: "Windows platform", workers: workersWithPlatforms(ocispec.Platform{OS: "linux", Architecture: "amd64"}, ocispec.Platform{OS: "windows", Architecture: "amd64"}), wantError: "unsupported platform windows/amd64"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateWorkers(test.workers)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("ValidateWorkers() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestProbeOpensListsAndClosesOneClient(t *testing.T) {
	tlsPaths := tlsmaterial.Paths{CA: "/tls/ca.crt", ClientCert: "/tls/client.crt", ClientKey: "/tls/client.key"}
	client := &fakeWorkerClient{workers: workersWithPlatforms(ocispec.Platform{OS: "linux", Architecture: "amd64"})}
	var gotAddress string
	var gotTLS tlsmaterial.Paths
	probe := Probe{Open: func(_ context.Context, address string, paths tlsmaterial.Paths) (WorkerClient, error) {
		gotAddress = address
		gotTLS = paths
		return client, nil
	}}

	if err := probe.Ready(t.Context(), "tcp://127.0.0.1:49152", tlsPaths); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	if gotAddress != "tcp://127.0.0.1:49152" || gotTLS != tlsPaths {
		t.Fatalf("Open() got address %q TLS %#v", gotAddress, gotTLS)
	}
	if client.listCalls != 1 || client.closeCalls != 1 {
		t.Fatalf("ListWorkers calls = %d Close calls = %d, want 1 each", client.listCalls, client.closeCalls)
	}
}

func TestProbeJoinsWorkerAndCloseErrors(t *testing.T) {
	listError := errors.New("list workers failed")
	closeError := errors.New("close client failed")
	client := &fakeWorkerClient{listErr: listError, closeErr: closeError}
	probe := Probe{Open: func(context.Context, string, tlsmaterial.Paths) (WorkerClient, error) { return client, nil }}

	err := probe.Ready(t.Context(), "tcp://127.0.0.1:49152", tlsmaterial.Paths{})
	if !errors.Is(err, listError) || !errors.Is(err, closeError) {
		t.Fatalf("Ready() error = %v, want joined list and close errors", err)
	}
}

func workersWithPlatforms(platforms ...ocispec.Platform) []*buildkit.WorkerInfo {
	return []*buildkit.WorkerInfo{{ID: "worker0", Platforms: platforms}}
}

type fakeWorkerClient struct {
	workers    []*buildkit.WorkerInfo
	listErr    error
	closeErr   error
	listCalls  int
	closeCalls int
}

func (client *fakeWorkerClient) ListWorkers(context.Context, ...buildkit.ListWorkersOption) ([]*buildkit.WorkerInfo, error) {
	client.listCalls++
	return client.workers, client.listErr
}

func (client *fakeWorkerClient) Close() error {
	client.closeCalls++
	return client.closeErr
}
