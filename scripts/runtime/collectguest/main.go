package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	runtimepkg "github.com/labring/sealbuild/internal/runtime"
)

var sourceComponentNames = []string{"buildkit-source", "runc-source", "cni-plugins-source"}

type collectConfig struct {
	LockPath             string
	BuildrootLicensePath string
	SourceDirectory      string
	OutputDirectory      string
}

type downloader interface {
	Download(context.Context, string, io.Writer) error
}

type httpDownloader struct {
	client *http.Client
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("collectguest", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var config collectConfig
	flags.StringVar(&config.LockPath, "lock", "", "Guest Runtime Lock path")
	flags.StringVar(&config.BuildrootLicensePath, "buildroot-licenses", "", "Buildroot legal-info licenses directory")
	flags.StringVar(&config.SourceDirectory, "source-dir", "", "verified source archive directory")
	flags.StringVar(&config.OutputDirectory, "output", "", "Guest license output directory")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse collectguest arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional argument: %s", flags.Arg(0))
	}
	for _, required := range []struct {
		name  string
		value string
	}{
		{"--lock", config.LockPath},
		{"--buildroot-licenses", config.BuildrootLicensePath},
		{"--source-dir", config.SourceDirectory},
		{"--output", config.OutputDirectory},
	} {
		if required.value == "" {
			return fmt.Errorf("%s is required", required.name)
		}
	}
	return collectGuestLicenses(ctx, config, httpDownloader{client: http.DefaultClient})
}

func collectGuestLicenses(ctx context.Context, config collectConfig, sourceDownloader downloader) (returnErr error) {
	lockFile, err := os.Open(config.LockPath)
	if err != nil {
		return fmt.Errorf("open Guest Runtime Lock: %w", err)
	}
	lock, err := runtimepkg.LoadLock(lockFile)
	closeErr := lockFile.Close()
	if err != nil {
		return errors.Join(err, closeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close Guest Runtime Lock: %w", closeErr)
	}

	sourceComponents, err := lockedSourceComponents(lock)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(config.OutputDirectory); err == nil {
		return fmt.Errorf("Guest license output already exists: %s", config.OutputDirectory)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect Guest license output: %w", err)
	}
	if err := os.MkdirAll(config.SourceDirectory, 0o755); err != nil {
		return fmt.Errorf("create Guest license source directory: %w", err)
	}
	outputParent := filepath.Dir(config.OutputDirectory)
	if err := os.MkdirAll(outputParent, 0o755); err != nil {
		return fmt.Errorf("create Guest license output parent: %w", err)
	}
	temporaryOutput, err := os.MkdirTemp(outputParent, ".guest-licenses-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary Guest license output: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, os.RemoveAll(temporaryOutput))
	}()

	legalCount, err := copyStrictTree(
		config.BuildrootLicensePath,
		filepath.Join(temporaryOutput, "buildroot"),
	)
	if err != nil {
		return fmt.Errorf("copy Buildroot legal-info licenses: %w", err)
	}
	if legalCount == 0 {
		return fmt.Errorf("Buildroot legal-info licenses must not be empty")
	}

	publishedArchives := make([]string, 0, len(sourceComponents))
	defer func() {
		if returnErr != nil {
			for _, archivePath := range publishedArchives {
				returnErr = errors.Join(returnErr, os.Remove(archivePath))
			}
		}
	}()
	for _, component := range sourceComponents {
		outputName := strings.TrimSuffix(component.Name, "-source")
		archivePath := filepath.Join(config.SourceDirectory, outputName+".tar.gz")
		if _, err := os.Lstat(archivePath); err == nil {
			return fmt.Errorf("Guest license source archive already exists: %s", archivePath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect Guest license source archive: %w", err)
		}
		temporaryArchive, err := os.CreateTemp(config.SourceDirectory, "."+outputName+"-*.tmp")
		if err != nil {
			return fmt.Errorf("create temporary %s source archive: %w", outputName, err)
		}
		temporaryArchivePath := temporaryArchive.Name()
		removeTemporaryArchive := true
		defer func() {
			if removeTemporaryArchive {
				returnErr = errors.Join(returnErr, os.Remove(temporaryArchivePath))
			}
		}()

		hash := sha256.New()
		if err := sourceDownloader.Download(ctx, component.Source, io.MultiWriter(temporaryArchive, hash)); err != nil {
			_ = temporaryArchive.Close()
			return fmt.Errorf("download %s source: %w", outputName, err)
		}
		if err := temporaryArchive.Sync(); err != nil {
			_ = temporaryArchive.Close()
			return fmt.Errorf("sync %s source archive: %w", outputName, err)
		}
		if err := temporaryArchive.Close(); err != nil {
			return fmt.Errorf("close %s source archive: %w", outputName, err)
		}
		actualSHA256 := fmt.Sprintf("%x", hash.Sum(nil))
		if actualSHA256 != component.SHA256 {
			return fmt.Errorf("%s source SHA-256 is %s, expected %s", outputName, actualSHA256, component.SHA256)
		}
		if err := extractLicenseFiles(temporaryArchivePath, filepath.Join(temporaryOutput, outputName)); err != nil {
			return fmt.Errorf("extract %s licenses: %w", outputName, err)
		}
		if err := os.Link(temporaryArchivePath, archivePath); err != nil {
			return fmt.Errorf("publish %s source archive: %w", outputName, err)
		}
		publishedArchives = append(publishedArchives, archivePath)
		if err := os.Remove(temporaryArchivePath); err != nil {
			return fmt.Errorf("remove temporary %s source archive: %w", outputName, err)
		}
		removeTemporaryArchive = false
	}

	if err := os.Rename(temporaryOutput, config.OutputDirectory); err != nil {
		return fmt.Errorf("publish Guest licenses: %w", err)
	}
	return nil
}

func (downloader httpDownloader) Download(ctx context.Context, source string, destination io.Writer) (returnErr error) {
	parsed, err := url.Parse(source)
	if err != nil {
		return fmt.Errorf("parse source URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("source URL must use https")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return fmt.Errorf("create source request: %w", err)
	}
	response, err := downloader.client.Do(request)
	if err != nil {
		return fmt.Errorf("request source: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, response.Body.Close())
	}()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("source returned HTTP %s", response.Status)
	}
	if _, err := io.Copy(destination, response.Body); err != nil {
		return fmt.Errorf("read source response: %w", err)
	}
	return nil
}

func lockedSourceComponents(lock runtimepkg.Lock) ([]runtimepkg.Component, error) {
	components := make(map[string]runtimepkg.Component, len(sourceComponentNames))
	for _, component := range lock.Components {
		for _, sourceName := range sourceComponentNames {
			if component.Name == sourceName {
				components[sourceName] = component
			}
		}
	}
	result := make([]runtimepkg.Component, 0, len(sourceComponentNames))
	for _, name := range sourceComponentNames {
		component, exists := components[name]
		if !exists {
			return nil, fmt.Errorf("Guest Runtime Lock is missing %s", name)
		}
		result = append(result, component)
	}
	return result, nil
}

func extractLicenseFiles(archivePath, destinationRoot string) (returnErr error) {
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open source archive: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, archiveFile.Close()) }()
	gzipReader, err := gzip.NewReader(archiveFile)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, gzipReader.Close()) }()
	if err := os.Mkdir(destinationRoot, 0o755); err != nil {
		return fmt.Errorf("create component license directory: %w", err)
	}

	tarReader := tar.NewReader(gzipReader)
	topLevel := ""
	licensePaths := make(map[string]struct{})
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}
		if header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeXHeader {
			continue
		}
		entryPath := strings.TrimSuffix(header.Name, "/")
		if entryPath == "" || path.IsAbs(entryPath) || strings.Contains(entryPath, `\`) || path.Clean(entryPath) != entryPath || entryPath == ".." || strings.HasPrefix(entryPath, "../") {
			return fmt.Errorf("source archive contains unsafe path %q", header.Name)
		}
		parts := strings.Split(entryPath, "/")
		if topLevel == "" {
			topLevel = parts[0]
		} else if parts[0] != topLevel {
			return fmt.Errorf("source archive contains multiple top-level directories %q and %q", topLevel, parts[0])
		}
		if len(parts) == 1 {
			continue
		}
		relativePath := strings.Join(parts[1:], "/")
		if !isLicenseBaseName(path.Base(relativePath)) {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return fmt.Errorf("license entry %s must be a regular file", header.Name)
		}
		if header.Size <= 0 {
			return fmt.Errorf("license entry %s must not be empty", header.Name)
		}
		if _, exists := licensePaths[relativePath]; exists {
			return fmt.Errorf("license output path %s is duplicated", relativePath)
		}
		licensePaths[relativePath] = struct{}{}

		destinationPath := filepath.Join(destinationRoot, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			return fmt.Errorf("create license parent: %w", err)
		}
		destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("create license file: %w", err)
		}
		if _, err := io.CopyN(destination, tarReader, header.Size); err != nil {
			_ = destination.Close()
			return fmt.Errorf("copy license file: %w", err)
		}
		if err := destination.Sync(); err != nil {
			_ = destination.Close()
			return fmt.Errorf("sync license file: %w", err)
		}
		if err := destination.Close(); err != nil {
			return fmt.Errorf("close license file: %w", err)
		}
	}
	if len(licensePaths) == 0 {
		return fmt.Errorf("source archive contains no license files")
	}
	return nil
}

func isLicenseBaseName(baseName string) bool {
	return baseName == "LICENSE" || strings.HasPrefix(baseName, "LICENSE.") ||
		baseName == "COPYING" || strings.HasPrefix(baseName, "COPYING.")
}

func copyStrictTree(sourceDirectory, destinationDirectory string) (int, error) {
	entries, err := os.ReadDir(sourceDirectory)
	if err != nil {
		return 0, fmt.Errorf("read source directory: %w", err)
	}
	if err := os.Mkdir(destinationDirectory, 0o755); err != nil {
		return 0, fmt.Errorf("create destination directory: %w", err)
	}
	fileCount := 0
	for _, entry := range entries {
		sourcePath := filepath.Join(sourceDirectory, entry.Name())
		destinationPath := filepath.Join(destinationDirectory, entry.Name())
		info, err := os.Lstat(sourcePath)
		if err != nil {
			return 0, fmt.Errorf("inspect source entry: %w", err)
		}
		switch {
		case info.IsDir():
			childCount, err := copyStrictTree(sourcePath, destinationPath)
			if err != nil {
				return 0, err
			}
			fileCount += childCount
		case info.Mode().IsRegular():
			if info.Size() <= 0 {
				return 0, fmt.Errorf("source file %s must not be empty", sourcePath)
			}
			if err := copyFile(sourcePath, destinationPath); err != nil {
				return 0, err
			}
			fileCount++
		default:
			return 0, fmt.Errorf("source entry %s must be a regular file or directory", sourcePath)
		}
	}
	return fileCount, nil
}

func copyFile(sourcePath, destinationPath string) (returnErr error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, source.Close()) }()
	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create destination file: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, destination.Close()) }()
	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("copy file: %w", err)
	}
	if err := destination.Sync(); err != nil {
		return fmt.Errorf("sync destination file: %w", err)
	}
	return nil
}
