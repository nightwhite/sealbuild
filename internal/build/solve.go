package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"

	buildkit "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/tonistiigi/fsutil"

	"github.com/labring/sealbuild/internal/tlsmaterial"
)

// SolveClient is the BuildKit surface needed for one Dockerfile Solve.
type SolveClient interface {
	Solve(context.Context, *llb.Definition, buildkit.SolveOpt, chan *buildkit.SolveStatus) (*buildkit.SolveResponse, error)
	Close() error
}

// SolveClientFactory opens one mTLS BuildKit Solve client.
type SolveClientFactory func(context.Context, string, tlsmaterial.Paths) (SolveClient, error)

// Solver executes one fixed linux/amd64 Dockerfile Solve.
type Solver struct {
	Open SolveClientFactory
}

// Solve builds and publishes one verified OCI archive.
func (solver Solver) Solve(ctx context.Context, address string, tls tlsmaterial.Paths, request PreparedRequest, progress io.Writer) (returnErr error) {
	if solver.Open == nil {
		return fmt.Errorf("BuildKit Solve client factory is required")
	}
	if progress == nil {
		return fmt.Errorf("BuildKit progress writer is required")
	}
	output, err := NewArchiveOutput(request.OutputPath)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, output.Abort()) }()

	contextMount, err := fsutil.NewFS(request.ContextDir)
	if err != nil {
		return fmt.Errorf("open build context: %w", err)
	}
	dockerfileMount, err := fsutil.NewFS(request.DockerfileDir)
	if err != nil {
		return fmt.Errorf("open Dockerfile directory: %w", err)
	}
	authSession, err := NewAnonymousAuth(request.HostProxy)
	if err != nil {
		return err
	}
	client, err := solver.Open(ctx, address, tls)
	if err != nil {
		return fmt.Errorf("open BuildKit Solve client: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, client.Close()) }()

	display, err := progressui.NewDisplay(progress, progressui.PlainMode)
	if err != nil {
		return fmt.Errorf("create BuildKit progress display: %w", err)
	}
	status := make(chan *buildkit.SolveStatus)
	displayResult := make(chan error, 1)
	go func() {
		_, err := display.UpdateFrom(ctx, status)
		displayResult <- err
	}()

	_, solveErr := client.Solve(ctx, nil, buildkit.SolveOpt{
		Exports: []buildkit.ExportEntry{{Type: buildkit.ExporterOCI, Output: output.Writer}},
		LocalMounts: map[string]fsutil.FS{
			"context": contextMount, "dockerfile": dockerfileMount,
		},
		Frontend: "dockerfile.v0", FrontendAttrs: maps.Clone(request.FrontendAttrs),
		Session: []session.Attachable{authSession},
	}, status)
	displayErr := <-displayResult
	if solveErr != nil || displayErr != nil {
		if solveErr != nil {
			solveErr = fmt.Errorf("solve Dockerfile: %w", solveErr)
		}
		return errors.Join(solveErr, displayErr)
	}
	if err := output.Publish(); err != nil {
		return err
	}
	return nil
}

// OpenSolveClient opens the product mTLS connection used for Solve.
func OpenSolveClient(ctx context.Context, address string, tls tlsmaterial.Paths) (SolveClient, error) {
	client, err := buildkit.New(
		ctx,
		address,
		buildkit.WithServerConfig(buildKitServerName, tls.CA),
		buildkit.WithCredentials(tls.ClientCert, tls.ClientKey),
	)
	if err != nil {
		return nil, err
	}
	return client, nil
}
