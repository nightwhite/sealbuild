//go:build !windows

package vm

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"
)

const shutdownAcknowledgement = "SEALBUILD_RUNTIME_SHUTDOWN"

// RequestGuestShutdown asks the Guest to flush and unmount its persistent state.
func RequestGuestShutdown(path string, timeout time.Duration) error {
	connection, err := net.DialTimeout("unix", path, timeout)
	if err != nil {
		return fmt.Errorf("connect Guest shutdown socket: %w", err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("set Guest shutdown deadline: %w", err)
	}
	if _, err := connection.Write([]byte("shutdown\n")); err != nil {
		return fmt.Errorf("send Guest shutdown request: %w", err)
	}
	response, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read Guest shutdown acknowledgement: %w", err)
	}
	if strings.TrimSpace(response) != shutdownAcknowledgement {
		return fmt.Errorf("unexpected acknowledgement %q", strings.TrimSpace(response))
	}
	return nil
}
