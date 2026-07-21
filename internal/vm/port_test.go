package vm

import (
	"fmt"
	"net"
	"testing"
)

func TestLoopbackPortsReservesAndReleasesOnePort(t *testing.T) {
	port, release, err := (LoopbackPorts{}).ReserveLoopback()
	if err != nil {
		t.Fatalf("ReserveLoopback() error = %v", err)
	}
	if port == 0 {
		t.Fatal("ReserveLoopback() port = 0")
	}
	address := fmt.Sprintf("127.0.0.1:%d", port)
	if listener, err := net.Listen("tcp4", address); err == nil {
		listener.Close()
		t.Fatalf("second Listen(%s) succeeded while port was reserved", address)
	}
	if err := release(); err != nil {
		t.Fatalf("release() error = %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("repeated release() error = %v", err)
	}

	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatalf("Listen(%s) after release error = %v", address, err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("listener Close() error = %v", err)
	}
}
