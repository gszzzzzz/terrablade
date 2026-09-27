package main

import (
	"errors"
	"fmt"
	"os"
)

// readFile reads one input. Directories are rejected, and so is any other
// non-regular file that would be rewritten: only a regular file can be updated
// in place. Pipes and devices are read like stdin, as with /dev/stdin or <(cmd).
// It does not protect against the path changing between Stat and ReadFile.
func readFile(path string, rewrite bool) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.New("input is a directory")
	}
	if rewrite && !info.Mode().IsRegular() {
		return nil, errors.New("--write requires a regular file")
	}
	return os.ReadFile(path)
}

// writeFile updates an existing file in place, following symbolic links and
// preserving the shared inode of hard links. Call only after successful parsing
// and a byte comparison: opening truncates immediately. If the write then
// fails, the original bytes are written back through the same descriptor, so a
// full disk or file-size limit does not leave a truncated input behind. A path
// removed since reading is not recreated.
func writeFile(path string, original, formatted []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}

	if _, err = file.Write(formatted); err != nil {
		if restoreErr := restore(file, original); restoreErr != nil {
			err = fmt.Errorf("%w; restore original content: %w", withoutPath(err), withoutPath(restoreErr))
		} else {
			err = fmt.Errorf("%w; original content restored", withoutPath(err))
		}
	}

	// Close always runs, even after a failed write, and neither failure hides
	// the other.
	if closeErr := file.Close(); closeErr != nil {
		if err == nil {
			return closeErr
		}
		return fmt.Errorf("%w; %w", err, withoutPath(closeErr))
	}
	return err
}

func restore(file *os.File, original []byte) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	_, err := file.WriteAt(original, 0)
	return err
}
