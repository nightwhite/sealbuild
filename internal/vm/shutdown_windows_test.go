//go:build windows

package vm

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

func TestWindowsRequestGuestShutdownWaitsForAcknowledgement(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
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
		_, _ = connection.Write([]byte(shutdownAcknowledgement + "\n"))
	}()

	if err := RequestGuestShutdown(listener.Addr().String(), time.Second); err != nil {
		t.Fatalf("RequestGuestShutdown() error = %v", err)
	}
	if got := <-request; got != "shutdown\n" {
		t.Fatalf("request = %q, want shutdown", got)
	}
}

func TestWindowsRequestGuestShutdownRejectsUnexpectedAcknowledgement(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			_, _ = connection.Write([]byte("invalid\n"))
			_ = connection.Close()
		}
	}()

	err = RequestGuestShutdown(listener.Addr().String(), time.Second)
	if err == nil || !strings.Contains(err.Error(), "unexpected acknowledgement") {
		t.Fatalf("RequestGuestShutdown() error = %v", err)
	}
}
