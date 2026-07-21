// Package tlsmaterial manages installation-scoped BuildKit mutual TLS files.
package tlsmaterial

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/labring/sealbuild/internal/platformfs"
)

const serverName = "sealbuild-runtime"

// Paths identifies the five persisted installation TLS files.
type Paths struct {
	CA         string
	ServerCert string
	ServerKey  string
	ClientCert string
	ClientKey  string
}

// Generate creates a new installation-scoped CA, server identity, and client identity.
func Generate(directory string, now time.Time) (paths Paths, returnErr error) {
	if _, err := os.Lstat(directory); err == nil {
		return Paths{}, fmt.Errorf("TLS material directory already exists: %s", directory)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Paths{}, fmt.Errorf("inspect TLS material directory: %w", err)
	}
	parent := filepath.Dir(directory)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Paths{}, fmt.Errorf("create TLS material parent: %w", err)
	}
	temporaryDirectory, err := os.MkdirTemp(parent, ".tls-*.tmp")
	if err != nil {
		return Paths{}, fmt.Errorf("create temporary TLS material directory: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, os.RemoveAll(temporaryDirectory))
	}()
	temporaryPaths := pathsForDirectory(temporaryDirectory)

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Paths{}, fmt.Errorf("generate CA key: %w", err)
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Paths{}, fmt.Errorf("generate server key: %w", err)
	}
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Paths{}, fmt.Errorf("generate client key: %w", err)
	}
	notBefore := now.Add(-5 * time.Minute)
	notAfter := now.Add(3650 * 24 * time.Hour)
	caTemplate := &x509.Certificate{
		SerialNumber:          randomSerialNumber(),
		Subject:               pkix.Name{CommonName: "sealbuild-installation-ca"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	if caTemplate.SerialNumber == nil {
		return Paths{}, fmt.Errorf("generate CA serial number")
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return Paths{}, fmt.Errorf("create CA certificate: %w", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: randomSerialNumber(),
		Subject:      pkix.Name{CommonName: serverName},
		DNSNames:     []string{serverName},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	clientTemplate := &x509.Certificate{
		SerialNumber: randomSerialNumber(),
		Subject:      pkix.Name{CommonName: "sealbuild-client"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if serverTemplate.SerialNumber == nil || clientTemplate.SerialNumber == nil {
		return Paths{}, fmt.Errorf("generate leaf certificate serial number")
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		return Paths{}, fmt.Errorf("create server certificate: %w", err)
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caTemplate, &clientKey.PublicKey, caKey)
	if err != nil {
		return Paths{}, fmt.Errorf("create client certificate: %w", err)
	}

	if err := writePEMFile(temporaryPaths.CA, "CERTIFICATE", caDER, 0o644); err != nil {
		return Paths{}, err
	}
	if err := writePEMFile(temporaryPaths.ServerCert, "CERTIFICATE", serverDER, 0o644); err != nil {
		return Paths{}, err
	}
	if err := writePrivateKey(temporaryPaths.ServerKey, serverKey); err != nil {
		return Paths{}, err
	}
	if err := writePEMFile(temporaryPaths.ClientCert, "CERTIFICATE", clientDER, 0o644); err != nil {
		return Paths{}, err
	}
	if err := writePrivateKey(temporaryPaths.ClientKey, clientKey); err != nil {
		return Paths{}, err
	}
	if err := Validate(temporaryPaths, now); err != nil {
		return Paths{}, fmt.Errorf("validate generated TLS material: %w", err)
	}
	if err := syncDirectory(temporaryDirectory); err != nil {
		return Paths{}, err
	}
	if err := platformfs.PublishDirectoryNoReplace(temporaryDirectory, directory); err != nil {
		return Paths{}, fmt.Errorf("publish TLS material: %w", err)
	}
	if err := syncDirectory(parent); err != nil {
		return Paths{}, err
	}
	return pathsForDirectory(directory), nil
}

// Validate checks certificate purpose, lifetime, ownership modes, and key matching.
func Validate(paths Paths, now time.Time) error {
	directory := filepath.Dir(paths.CA)
	wantPaths := pathsForDirectory(directory)
	if paths != wantPaths {
		return fmt.Errorf("TLS material paths must use the fixed installation filenames")
	}
	wantFiles := []string{"ca.crt", "client.crt", "client.key", "server.crt", "server.key"}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read TLS material directory: %w", err)
	}
	gotFiles := make([]string, 0, len(entries))
	for _, entry := range entries {
		gotFiles = append(gotFiles, entry.Name())
	}
	slices.Sort(gotFiles)
	if !slices.Equal(gotFiles, wantFiles) {
		if slices.Contains(gotFiles, "ca.key") {
			return fmt.Errorf("CA private key must not be present")
		}
		return fmt.Errorf("TLS material directory contains an unexpected file set")
	}
	for filePath, wantMode := range map[string]os.FileMode{
		paths.CA: 0o644, paths.ServerCert: 0o644, paths.ClientCert: 0o644,
		paths.ServerKey: 0o600, paths.ClientKey: 0o600,
	} {
		info, err := os.Lstat(filePath)
		if err != nil {
			return fmt.Errorf("inspect TLS file %s: %w", filepath.Base(filePath), err)
		}
		var modeErr error
		if wantMode == 0o600 {
			modeErr = platformfs.ValidatePrivateFile(info)
		} else {
			modeErr = platformfs.ValidatePublicFile(info)
		}
		if modeErr != nil {
			label := strings.TrimSuffix(strings.ReplaceAll(filepath.Base(filePath), ".", " "), "")
			return fmt.Errorf("%s mode is invalid: %w", label, modeErr)
		}
	}

	ca, err := readCertificate(paths.CA)
	if err != nil {
		return err
	}
	server, err := readCertificate(paths.ServerCert)
	if err != nil {
		return err
	}
	client, err := readCertificate(paths.ClientCert)
	if err != nil {
		return err
	}
	if !ca.IsCA || ca.KeyUsage&x509.KeyUsageCertSign == 0 {
		return fmt.Errorf("CA certificate constraints are invalid")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := server.Verify(x509.VerifyOptions{
		Roots: roots, DNSName: serverName, CurrentTime: now,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return fmt.Errorf("verify server certificate: %w", err)
	}
	if len(server.ExtKeyUsage) != 1 || server.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth || len(server.DNSNames) != 1 || server.DNSNames[0] != serverName {
		return fmt.Errorf("server certificate purpose is invalid")
	}
	if _, err := client.Verify(x509.VerifyOptions{
		Roots: roots, CurrentTime: now,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		return fmt.Errorf("verify client certificate: %w", err)
	}
	if len(client.ExtKeyUsage) != 1 || client.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		return fmt.Errorf("client certificate purpose is invalid")
	}
	if err := verifyKeyPair(server, paths.ServerKey, "server"); err != nil {
		return err
	}
	if err := verifyKeyPair(client, paths.ClientKey, "client"); err != nil {
		return err
	}
	return nil
}

func pathsForDirectory(directory string) Paths {
	return Paths{
		CA: filepath.Join(directory, "ca.crt"), ServerCert: filepath.Join(directory, "server.crt"),
		ServerKey: filepath.Join(directory, "server.key"), ClientCert: filepath.Join(directory, "client.crt"),
		ClientKey: filepath.Join(directory, "client.key"),
	}
}

func randomSerialNumber() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil
	}
	return serial
}

func writePrivateKey(filePath string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("encode private key: %w", err)
	}
	return writePEMFile(filePath, "PRIVATE KEY", der, 0o600)
}

func writePEMFile(filePath, blockType string, der []byte, mode os.FileMode) (returnErr error) {
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create TLS file %s: %w", filepath.Base(filePath), err)
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	if err := pem.Encode(file, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		return fmt.Errorf("encode TLS file %s: %w", filepath.Base(filePath), err)
	}
	if err := file.Chmod(mode); err != nil {
		return fmt.Errorf("set TLS file permissions: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync TLS file: %w", err)
	}
	return nil
}

func readCertificate(filePath string) (*x509.Certificate, error) {
	contents, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read certificate %s: %w", filepath.Base(filePath), err)
	}
	block, rest := pem.Decode(contents)
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		return nil, fmt.Errorf("certificate %s has invalid PEM", filepath.Base(filePath))
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate %s: %w", filepath.Base(filePath), err)
	}
	return certificate, nil
}

func verifyKeyPair(certificate *x509.Certificate, keyPath, identity string) error {
	contents, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read %s private key: %w", identity, err)
	}
	block, rest := pem.Decode(contents)
	if block == nil || block.Type != "PRIVATE KEY" || len(rest) != 0 {
		return fmt.Errorf("%s private key has invalid PEM", identity)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse %s private key: %w", identity, err)
	}
	privateKey, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || privateKey.Curve != elliptic.P256() {
		return fmt.Errorf("%s private key must use ECDSA P-256", identity)
	}
	certificatePublic, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return fmt.Errorf("encode %s certificate public key: %w", identity, err)
	}
	privatePublic, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("encode %s private public key: %w", identity, err)
	}
	if !slices.Equal(certificatePublic, privatePublic) {
		return fmt.Errorf("%s certificate and private key do not match", identity)
	}
	return nil
}

func syncDirectory(directoryPath string) error {
	if err := platformfs.SyncDirectory(directoryPath); err != nil {
		return fmt.Errorf("sync TLS directory: %w", err)
	}
	return nil
}
