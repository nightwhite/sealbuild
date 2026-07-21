package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/labring/sealbuild/internal/cache"
	"github.com/labring/sealbuild/internal/lockfile"
	"github.com/labring/sealbuild/internal/platformfs"
	"github.com/labring/sealbuild/internal/tlsmaterial"
)

const installationSchemaVersion = 1

// Installation identifies one verified Host/Guest Runtime and persistent state disk.
type Installation struct {
	CompatibilityID string
	Root            string
	Host            string
	Guest           string
	StateDisk       string
	TLS             tlsmaterial.Paths
}

// Installer installs immutable Runtime bundles below one Sealbuild cache layout.
type Installer struct {
	Layout cache.Layout
}

type installationManifest struct {
	SchemaVersion   int    `json:"schemaVersion"`
	CompatibilityID string `json:"compatibilityID"`
	HostSHA256      string `json:"hostSHA256"`
	GuestSHA256     string `json:"guestSHA256"`
}

// Install installs or revalidates one Runtime bundle and initializes its state disk.
func (installer Installer) Install(ctx context.Context, bundle Bundle) (installation Installation, returnErr error) {
	if err := installer.Layout.Validate(); err != nil {
		return Installation{}, err
	}
	compatibilityID, err := bundle.CompatibilityID()
	if err != nil {
		return Installation{}, err
	}
	if err := createCacheDirectories(installer.Layout); err != nil {
		return Installation{}, err
	}
	runtimeDirectory, err := installer.Layout.RuntimeDir(compatibilityID)
	if err != nil {
		return Installation{}, err
	}
	runtimeLockPath, err := installer.Layout.RuntimeLockPath(compatibilityID)
	if err != nil {
		return Installation{}, err
	}
	runtimeLock, err := lockfile.TryAcquire(runtimeLockPath)
	if err != nil {
		return Installation{}, fmt.Errorf("acquire Runtime installation lock: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, runtimeLock.Close()) }()

	if _, err := os.Lstat(runtimeDirectory); err == nil {
		installation, err = verifyInstallation(runtimeDirectory, compatibilityID, bundle)
		if err != nil {
			return Installation{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Installation{}, fmt.Errorf("inspect Runtime installation: %w", err)
	} else {
		installation, err = installRuntime(ctx, installer.Layout, runtimeDirectory, compatibilityID, bundle)
		if err != nil {
			return Installation{}, err
		}
	}

	stateDisk, err := initializeStateDisk(installer.Layout, installation.Guest, compatibilityID)
	if err != nil {
		return Installation{}, err
	}
	installation.StateDisk = stateDisk
	return installation, nil
}

func installRuntime(ctx context.Context, layout cache.Layout, finalDirectory, compatibilityID string, bundle Bundle) (installation Installation, returnErr error) {
	runtimeParent := filepath.Dir(finalDirectory)
	temporaryDirectory, err := os.MkdirTemp(runtimeParent, "."+compatibilityID+"-*.tmp")
	if err != nil {
		return Installation{}, fmt.Errorf("create temporary Runtime installation: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, os.RemoveAll(temporaryDirectory)) }()
	hostDirectory := filepath.Join(temporaryDirectory, "host")
	guestDirectory := filepath.Join(temporaryDirectory, "guest")
	if err := os.Mkdir(hostDirectory, 0o755); err != nil {
		return Installation{}, fmt.Errorf("create Host Runtime directory: %w", err)
	}
	if err := os.Mkdir(guestDirectory, 0o755); err != nil {
		return Installation{}, fmt.Errorf("create Guest Runtime directory: %w", err)
	}
	hostResult, err := ExtractAsset(ctx, bundle.Host, ArtifactKindHost, hostDirectory)
	if err != nil {
		return Installation{}, fmt.Errorf("extract Host Runtime: %w", err)
	}
	if err := validateHostRuntimePlatform(hostResult.Manifest.Platform); err != nil {
		return Installation{}, err
	}
	if _, err := ExtractAsset(ctx, bundle.Guest, ArtifactKindGuest, guestDirectory); err != nil {
		return Installation{}, fmt.Errorf("extract Guest Runtime: %w", err)
	}
	if _, err := tlsmaterial.Generate(filepath.Join(temporaryDirectory, "tls"), time.Now().UTC()); err != nil {
		return Installation{}, fmt.Errorf("generate installation TLS material: %w", err)
	}
	metadata := installationManifest{
		SchemaVersion: installationSchemaVersion, CompatibilityID: compatibilityID,
		HostSHA256: bundle.Host.SHA256, GuestSHA256: bundle.Guest.SHA256,
	}
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return Installation{}, fmt.Errorf("encode installation manifest: %w", err)
	}
	metadataBytes = append(metadataBytes, '\n')
	if err := writeSyncedFile(filepath.Join(temporaryDirectory, "installation.json"), metadataBytes, 0o644); err != nil {
		return Installation{}, err
	}
	if err := writeSyncedFile(filepath.Join(temporaryDirectory, "complete"), []byte(compatibilityID+"\n"), 0o644); err != nil {
		return Installation{}, err
	}
	if err := syncInstallDirectory(temporaryDirectory); err != nil {
		return Installation{}, err
	}
	if err := platformfs.PublishDirectoryNoReplace(temporaryDirectory, finalDirectory); err != nil {
		return Installation{}, fmt.Errorf("publish Runtime installation: %w", err)
	}
	if err := syncInstallDirectory(runtimeParent); err != nil {
		return Installation{}, err
	}
	return installationPaths(finalDirectory, compatibilityID), nil
}

func verifyInstallation(directory, compatibilityID string, bundle Bundle) (Installation, error) {
	manifestFile, err := os.Open(filepath.Join(directory, "installation.json"))
	if err != nil {
		return Installation{}, fmt.Errorf("open installation manifest: %w", err)
	}
	decoder := json.NewDecoder(manifestFile)
	decoder.DisallowUnknownFields()
	var metadata installationManifest
	decodeErr := decoder.Decode(&metadata)
	var trailing any
	trailingErr := decoder.Decode(&trailing)
	closeErr := manifestFile.Close()
	if decodeErr != nil || trailingErr != io.EOF || closeErr != nil {
		return Installation{}, errors.Join(fmt.Errorf("decode installation manifest: %w", decodeErr), trailingErr, closeErr)
	}
	want := installationManifest{
		SchemaVersion: installationSchemaVersion, CompatibilityID: compatibilityID,
		HostSHA256: bundle.Host.SHA256, GuestSHA256: bundle.Guest.SHA256,
	}
	if metadata != want {
		return Installation{}, fmt.Errorf("installation manifest does not match Runtime bundle")
	}
	complete, err := os.ReadFile(filepath.Join(directory, "complete"))
	if err != nil {
		return Installation{}, fmt.Errorf("read Runtime completion marker: %w", err)
	}
	if string(complete) != compatibilityID+"\n" {
		return Installation{}, fmt.Errorf("Runtime completion marker is invalid")
	}
	if err := verifyInstalledArtifact(filepath.Join(directory, "host"), ArtifactKindHost, expectedHostPlatform()); err != nil {
		return Installation{}, fmt.Errorf("verify installed Host Runtime: %w", err)
	}
	if err := verifyInstalledArtifact(filepath.Join(directory, "guest"), ArtifactKindGuest, Platform{OS: "linux", Architecture: "amd64"}); err != nil {
		return Installation{}, fmt.Errorf("verify installed Guest Runtime: %w", err)
	}
	installation := installationPaths(directory, compatibilityID)
	if err := tlsmaterial.Validate(installation.TLS, time.Now().UTC()); err != nil {
		return Installation{}, fmt.Errorf("verify installation TLS material: %w", err)
	}
	return installation, nil
}

func verifyInstalledArtifact(directory string, kind ArtifactKind, platform Platform) error {
	manifestBytes, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return fmt.Errorf("read artifact manifest: %w", err)
	}
	manifest, err := LoadArtifactManifest(bytes.NewReader(manifestBytes))
	if err != nil {
		return err
	}
	if manifest.Kind != kind {
		return fmt.Errorf("artifact kind is %s, expected %s", manifest.Kind, kind)
	}
	if manifest.Platform != platform {
		return fmt.Errorf("artifact platform is %s/%s, expected %s/%s", manifest.Platform.OS, manifest.Platform.Architecture, platform.OS, platform.Architecture)
	}
	for _, expected := range manifest.Files {
		filePath := filepath.Join(directory, filepath.FromSlash(expected.Path))
		info, err := os.Lstat(filePath)
		if err != nil {
			return fmt.Errorf("inspect payload %s: %w", expected.Path, err)
		}
		if platformfs.ValidateArtifactFile(info, expected.Mode) != nil || info.Size() != expected.Size {
			return fmt.Errorf("payload %s metadata does not match manifest", expected.Path)
		}
		checksum, err := hashInstalledFile(filePath)
		if err != nil {
			return err
		}
		if checksum != expected.SHA256 {
			return fmt.Errorf("payload %s SHA-256 does not match manifest", expected.Path)
		}
	}
	checksums, err := os.ReadFile(filepath.Join(directory, "checksums.txt"))
	if err != nil {
		return fmt.Errorf("read artifact checksums: %w", err)
	}
	manifestHash := sha256.Sum256(manifestBytes)
	if !bytes.Equal(checksums, expectedChecksums(manifest.Files, fmt.Sprintf("%x", manifestHash))) {
		return fmt.Errorf("artifact checksums do not match manifest")
	}
	return nil
}

func initializeStateDisk(layout cache.Layout, guestDirectory, compatibilityID string) (stateDisk string, returnErr error) {
	stateLockPath, err := layout.StateLockPath(compatibilityID)
	if err != nil {
		return "", err
	}
	stateLock, err := lockfile.TryAcquire(stateLockPath)
	if err != nil {
		return "", fmt.Errorf("acquire Runtime state lock: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, stateLock.Close()) }()
	stateDirectory, err := layout.StateDir(compatibilityID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(stateDirectory, 0o700); err != nil {
		return "", fmt.Errorf("create Runtime state directory: %w", err)
	}
	stateDisk = filepath.Join(stateDirectory, "buildkit-state.qcow2")
	if info, err := os.Lstat(stateDisk); err == nil {
		if platformfs.ValidatePrivateFile(info) != nil || info.Size() <= 0 {
			return "", fmt.Errorf("existing Runtime state disk is invalid")
		}
		return stateDisk, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect Runtime state disk: %w", err)
	}
	templatePath := filepath.Join(guestDirectory, "buildkit-state.qcow2")
	temporary, err := os.CreateTemp(stateDirectory, ".buildkit-state-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary Runtime state disk: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		removeErr := os.Remove(temporaryPath)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		returnErr = errors.Join(returnErr, removeErr)
	}()
	template, err := os.Open(templatePath)
	if err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("open Runtime state template: %w", err)
	}
	_, copyErr := io.Copy(temporary, template)
	closeErr := errors.Join(template.Close(), temporary.Chmod(0o600), temporary.Sync(), temporary.Close())
	if copyErr != nil || closeErr != nil {
		return "", errors.Join(fmt.Errorf("copy Runtime state template: %w", copyErr), closeErr)
	}
	if err := platformfs.PublishFileNoReplace(temporaryPath, stateDisk); err != nil {
		return "", fmt.Errorf("publish Runtime state disk: %w", err)
	}
	if err := syncInstallDirectory(stateDirectory); err != nil {
		return "", err
	}
	return stateDisk, nil
}

func installationPaths(root, compatibilityID string) Installation {
	tlsDirectory := filepath.Join(root, "tls")
	return Installation{
		CompatibilityID: compatibilityID, Root: root,
		Host: filepath.Join(root, "host"), Guest: filepath.Join(root, "guest"),
		TLS: tlsmaterial.Paths{
			CA: filepath.Join(tlsDirectory, "ca.crt"), ServerCert: filepath.Join(tlsDirectory, "server.crt"),
			ServerKey: filepath.Join(tlsDirectory, "server.key"), ClientCert: filepath.Join(tlsDirectory, "client.crt"),
			ClientKey: filepath.Join(tlsDirectory, "client.key"),
		},
	}
}

func createCacheDirectories(layout cache.Layout) error {
	for _, directory := range []string{
		layout.Root,
		filepath.Join(layout.Root, "runtime"), filepath.Join(layout.Root, "state"),
		filepath.Join(layout.Root, "locks"), layout.LogDir(),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create Sealbuild cache directory %s: %w", directory, err)
		}
	}
	return nil
}

func writeSyncedFile(filePath string, contents []byte, mode os.FileMode) (returnErr error) {
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create installation file %s: %w", filepath.Base(filePath), err)
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	if _, err := file.Write(contents); err != nil {
		return fmt.Errorf("write installation file: %w", err)
	}
	if err := file.Chmod(mode); err != nil {
		return fmt.Errorf("set installation file permissions: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync installation file: %w", err)
	}
	return nil
}

func hashInstalledFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open installed payload: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash installed payload: %w", err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func syncInstallDirectory(directoryPath string) error {
	if err := platformfs.SyncDirectory(directoryPath); err != nil {
		return fmt.Errorf("sync installation directory: %w", err)
	}
	return nil
}
