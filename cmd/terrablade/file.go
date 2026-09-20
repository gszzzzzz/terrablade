package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func readFile(path string) ([]byte, os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	// In particular, reject a FIFO before opening it could block for a writer.
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("input is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("input is not a regular file")
	}
	source, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, err
	}
	if err := file.Close(); err != nil {
		return nil, nil, err
	}
	return source, info, nil
}

// replaceFile commits one changed file. Until Rename succeeds the original is
// untouched. A sibling temporary file keeps the replacement on one filesystem.
// There is no batch transaction or power-loss durability guarantee: we sync the
// file, not its directory. Concurrent editing is unsupported; the snapshot check
// detects observable edits but cannot close the final check/rename race.
func replaceFile(path string, formatted []byte, original os.FileInfo) (err error) {
	if err := checkReplacement(path, original); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".terrablade-*")
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		temp.Close()
		if committed {
			return
		}
		if cleanupErr := os.Remove(temp.Name()); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove temporary file: %w", cleanupErr))
		}
	}()
	if _, err := temp.Write(formatted); err != nil {
		return err
	}
	// Apply metadata after writing because writes/chown can clear set-ID bits.
	if err := preserveMetadata(temp, original); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := checkReplacement(path, original); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return err
	}
	committed = true
	return nil
}

func checkReplacement(path string, original os.FileInfo) error {
	current, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if current.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to replace a symbolic link")
	}
	if !current.Mode().IsRegular() || !os.SameFile(original, current) ||
		original.Size() != current.Size() || !original.ModTime().Equal(current.ModTime()) ||
		original.Mode() != current.Mode() {
		return errors.New("file changed while formatting")
	}
	return checkWriteMetadata(original, current)
}
