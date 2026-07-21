package main

import (
	"fmt"
	"os"

	buildpkg "github.com/labring/sealbuild/internal/build"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: verify-oci OCI_ARCHIVE")
	}
	if err := buildpkg.VerifyOCIArchive(args[0]); err != nil {
		return err
	}
	fmt.Println("OCI platform: linux/amd64")
	return nil
}
