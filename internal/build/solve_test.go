package build

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	buildkit "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"

	"github.com/labring/sealbuild/internal/tlsmaterial"
)

func TestSolverBuildsOneOCIArchiveWithFixedOptions(t *testing.T) {
	workspace := t.TempDir()
	contextDirectory := filepath.Join(workspace, "context")
	if err := os.Mkdir(contextDirectory, 0o755); err != nil {
		t.Fatalf("Mkdir(context) error = %v", err)
	}
	validArchive := writeOCIArchive(t, nil)
	client := &fakeSolveClient{archivePath: validArchive}
	var openedAddress string
	var openedTLS tlsmaterial.Paths
	solver := Solver{Open: func(_ context.Context, address string, tls tlsmaterial.Paths) (SolveClient, error) {
		openedAddress = address
		openedTLS = tls
		return client, nil
	}}
	request := PreparedRequest{
		ContextDir: contextDirectory, DockerfileDir: contextDirectory,
		OutputPath:    filepath.Join(workspace, "image.oci.tar"),
		FrontendAttrs: map[string]string{"filename": "Dockerfile", "platform": "linux/amd64"},
		HostProxy:     "http://127.0.0.1:7890",
	}
	tlsPaths := tlsmaterial.Paths{CA: "/tls/ca.crt", ClientCert: "/tls/client.crt", ClientKey: "/tls/client.key"}
	var progress bytes.Buffer

	if err := solver.Solve(t.Context(), "tcp://127.0.0.1:49152", tlsPaths, request, &progress); err != nil {
		t.Fatalf("Solve() error = %v", err)
	}
	if openedAddress != "tcp://127.0.0.1:49152" || openedTLS != tlsPaths {
		t.Fatalf("Open() address = %q TLS = %#v", openedAddress, openedTLS)
	}
	if client.solveCalls != 1 || client.closeCalls != 1 {
		t.Fatalf("Solve calls = %d Close calls = %d", client.solveCalls, client.closeCalls)
	}
	if client.definition != nil {
		t.Fatal("Solve definition must be nil for dockerfile frontend")
	}
	if client.options.Frontend != "dockerfile.v0" || client.options.FrontendAttrs["platform"] != "linux/amd64" {
		t.Fatalf("SolveOpt frontend = %q attrs = %#v", client.options.Frontend, client.options.FrontendAttrs)
	}
	if len(client.options.LocalMounts) != 2 || client.options.LocalMounts["context"] == nil || client.options.LocalMounts["dockerfile"] == nil {
		t.Fatalf("LocalMounts = %#v", client.options.LocalMounts)
	}
	if len(client.options.Exports) != 1 || client.options.Exports[0].Type != buildkit.ExporterOCI {
		t.Fatalf("Exports = %#v", client.options.Exports)
	}
	if len(client.options.Session) != 1 {
		t.Fatalf("Session count = %d, want 1", len(client.options.Session))
	}
	if _, ok := client.options.Session[0].(*anonymousAuth); !ok {
		t.Fatalf("Session type = %T, want anonymousAuth", client.options.Session[0])
	}
	if err := VerifyOCIArchive(request.OutputPath); err != nil {
		t.Fatalf("VerifyOCIArchive(output) error = %v", err)
	}
	if progress.Len() == 0 {
		t.Fatal("plain progress output is empty")
	}
}

func TestSolverAbortsOutputAndJoinsErrors(t *testing.T) {
	solveError := errors.New("solve failed")
	closeError := errors.New("client close failed")
	workspace := t.TempDir()
	client := &fakeSolveClient{solveErr: solveError, closeErr: closeError}
	solver := Solver{Open: func(context.Context, string, tlsmaterial.Paths) (SolveClient, error) { return client, nil }}
	request := PreparedRequest{
		ContextDir: workspace, DockerfileDir: workspace,
		OutputPath:    filepath.Join(workspace, "failed.oci.tar"),
		FrontendAttrs: map[string]string{"filename": "Dockerfile", "platform": "linux/amd64"},
	}
	err := solver.Solve(t.Context(), "tcp://127.0.0.1:49152", tlsmaterial.Paths{}, request, io.Discard)
	if !errors.Is(err, solveError) || !errors.Is(err, closeError) {
		t.Fatalf("Solve() error = %v, want joined solve and close errors", err)
	}
	if _, statErr := os.Stat(request.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Stat(output) error = %v, want not exist", statErr)
	}
}

type fakeSolveClient struct {
	archivePath string
	solveErr    error
	closeErr    error
	solveCalls  int
	closeCalls  int
	definition  *llb.Definition
	options     buildkit.SolveOpt
}

func (client *fakeSolveClient) Solve(_ context.Context, definition *llb.Definition, options buildkit.SolveOpt, status chan *buildkit.SolveStatus) (*buildkit.SolveResponse, error) {
	client.solveCalls++
	client.definition = definition
	client.options = options
	now := time.Now()
	status <- &buildkit.SolveStatus{Vertexes: []*buildkit.Vertex{{Name: "test build", Started: &now, Completed: &now, Cached: true}}}
	if client.solveErr == nil {
		writer, err := options.Exports[0].Output(nil)
		if err != nil {
			close(status)
			return nil, err
		}
		source, err := os.Open(client.archivePath)
		if err != nil {
			close(status)
			return nil, err
		}
		_, copyErr := io.Copy(writer, source)
		closeErr := errors.Join(source.Close(), writer.Close())
		if copyErr != nil || closeErr != nil {
			close(status)
			return nil, errors.Join(copyErr, closeErr)
		}
	}
	close(status)
	return &buildkit.SolveResponse{}, client.solveErr
}

func (client *fakeSolveClient) Close() error {
	client.closeCalls++
	return client.closeErr
}
