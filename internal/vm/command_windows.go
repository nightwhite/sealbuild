//go:build windows

package vm

func qemuCommand(config Config, qemuArguments []string) (string, []string, error) {
	return config.QEMUPath, qemuArguments, nil
}
