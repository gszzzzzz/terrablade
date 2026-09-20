package main

import (
	"errors"
	"os"
)

// readFile rejects paths that do not identify a regular file when inspected.
// It does not protect against the path changing between Stat and ReadFile.
func readFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("input is not a regular file")
	}
	return os.ReadFile(path)
}

// writeFile updates an existing file in place, following symbolic links and
// preserving the shared inode of hard links. Call only after successful parsing
// and a byte comparison: opening truncates immediately, and a later failure can
// leave partial content. A path removed since reading is not recreated.
func writeFile(path string, formatted []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}

	// Close always runs, even after a failed write, and neither failure hides
	// the other; reportError prints a joined error in full.
	_, writeErr := file.Write(formatted)
	closeErr := file.Close()
	if writeErr != nil && closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
