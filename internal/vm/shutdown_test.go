//go:build !windows

package vm

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRequestGuestShutdownWaitsForAcknowledgement(t *testing.T) {
	path := filepath.Join(os.TempDir(), "sealbuild-shutdown-success.sock")
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	request := make(chan string, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		line, _ := bufio.NewReader(connection).ReadString('\n')
		request <- line
		_, _ = connection.Write([]byte("SEALBUILD_RUNTIME_SHUTDOWN\n"))
	}()

	if err := RequestGuestShutdown(path, time.Second); err != nil {
		t.Fatalf("RequestGuestShutdown() error = %v", err)
	}
	if got := <-request; got != "shutdown\n" {
		t.Fatalf("request = %q, want shutdown", got)
	}
}

func TestRequestGuestShutdownRejectsUnexpectedAcknowledgement(t *testing.T) {
	path := filepath.Join(os.TempDir(), "sealbuild-shutdown-invalid.sock")
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			_, _ = bufio.NewReader(connection).ReadString('\n')
			_, _ = connection.Write([]byte("invalid\n"))
			_ = connection.Close()
		}
	}()

	err = RequestGuestShutdown(path, time.Second)
	if err == nil || !strings.Contains(err.Error(), "unexpected acknowledgement") {
		t.Fatalf("RequestGuestShutdown() error = %v", err)
	}
}
