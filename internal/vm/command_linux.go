//go:build linux

package vm

func qemuCommand(config Config, qemuArguments []string) (string, []string, error) {
	return linuxQEMUCommand(config.QEMUPath, qemuArguments)
}
