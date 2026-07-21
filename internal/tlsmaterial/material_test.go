package tlsmaterial

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateCreatesInstallationScopedMutualTLS(t *testing.T) {
	now := time.Date(2026, 7, 17, 4, 0, 0, 0, time.UTC)
	directory := filepath.Join(t.TempDir(), "tls")
	paths, err := Generate(directory, now)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if err := Validate(paths, now); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	wantModes := map[string]os.FileMode{
		paths.CA: 0o644, paths.ServerCert: 0o644, paths.ClientCert: 0o644,
		paths.ServerKey: 0o600, paths.ClientKey: 0o600,
	}
	for filePath, wantMode := range wantModes {
		info, err := os.Stat(filePath)
		if err != nil {
			t.Fatalf("Stat(%s) error = %v", filePath, err)
		}
		if info.Mode().Perm() != wantMode {
			t.Errorf("%s mode = %#o, want %#o", filePath, info.Mode().Perm(), wantMode)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "ca.key")); !os.IsNotExist(err) {
		t.Fatalf("CA private key Stat() error = %v, want not exist", err)
	}

	ca := readTestCertificate(t, paths.CA)
	server := readTestCertificate(t, paths.ServerCert)
	client := readTestCertificate(t, paths.ClientCert)
	if !ca.IsCA || ca.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Fatalf("CA constraints = IsCA %t KeyUsage %v", ca.IsCA, ca.KeyUsage)
	}
	if len(server.DNSNames) != 1 || server.DNSNames[0] != "sealbuild-runtime" {
		t.Fatalf("server DNSNames = %#v", server.DNSNames)
	}
	if len(server.ExtKeyUsage) != 1 || server.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Fatalf("server EKU = %#v", server.ExtKeyUsage)
	}
	if len(client.ExtKeyUsage) != 1 || client.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatalf("client EKU = %#v", client.ExtKeyUsage)
	}
	if _, ok := server.PublicKey.(*ecdsa.PublicKey); !ok {
		t.Fatalf("server public key type = %T, want ECDSA", server.PublicKey)
	}
	wantNotBefore := now.Add(-5 * time.Minute)
	wantNotAfter := now.Add(3650 * 24 * time.Hour)
	if !server.NotBefore.Equal(wantNotBefore) || !server.NotAfter.Equal(wantNotAfter) {
		t.Fatalf("server validity = %s..%s, want %s..%s", server.NotBefore, server.NotAfter, wantNotBefore, wantNotAfter)
	}
}

func TestValidateRejectsInvalidTLSMaterial(t *testing.T) {
	now := time.Date(2026, 7, 17, 4, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		mutate    func(*testing.T, Paths)
		validate  func(Paths) error
		wantError string
	}{
		{name: "expired", validate: func(paths Paths) error { return Validate(paths, now.Add(3651*24*time.Hour)) }, wantError: "certificate"},
		{name: "wrong permissions", mutate: func(t *testing.T, paths Paths) { chmodTLS(t, paths.ServerKey, 0o644) }, validate: func(paths Paths) error { return Validate(paths, now) }, wantError: "server key mode"},
		{name: "mismatched key", mutate: func(t *testing.T, paths Paths) { copyTLSFile(t, paths.ClientKey, paths.ServerKey, 0o600) }, validate: func(paths Paths) error { return Validate(paths, now) }, wantError: "server certificate and private key do not match"},
		{name: "CA private key present", mutate: func(t *testing.T, paths Paths) {
			writeTLSFile(t, filepath.Join(filepath.Dir(paths.CA), "ca.key"), "private", 0o600)
		}, validate: func(paths Paths) error { return Validate(paths, now) }, wantError: "CA private key must not be present"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths, err := Generate(filepath.Join(t.TempDir(), "tls"), now)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			if test.mutate != nil {
				test.mutate(t, paths)
			}
			err = test.validate(paths)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Validate() error = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestGenerateRejectsExistingDirectory(t *testing.T) {
	directory := t.TempDir()
	_, err := Generate(directory, time.Now())
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Generate() error = %v, want existing directory", err)
	}
}

func readTestCertificate(t *testing.T, filePath string) *x509.Certificate {
	t.Helper()
	contents, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", filePath, err)
	}
	block, _ := pem.Decode(contents)
	if block == nil {
		t.Fatalf("Decode(%s) returned nil", filePath)
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate(%s) error = %v", filePath, err)
	}
	return certificate
}

func chmodTLS(t *testing.T, filePath string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(filePath, mode); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
}

func copyTLSFile(t *testing.T, sourcePath, destinationPath string, mode os.FileMode) {
	t.Helper()
	contents, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	writeTLSFile(t, destinationPath, string(contents), mode)
}

func writeTLSFile(t *testing.T, filePath, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filePath, []byte(contents), mode); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chmod(filePath, mode); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
}
