// Package platformfs isolates filesystem guarantees that differ between Unix and Windows.
package platformfs

import (
	"fmt"
	"os"
)

func validateRegularFile(info os.FileInfo) error {
	if info == nil || !info.Mode().IsRegular() {
		return fmt.Errorf("path must be a regular file")
	}
	return nil
}
