//go:build !darwin && !linux

package main

import (
	"errors"
	"os"
)

// os.Rename does not promise atomic replacement on non-Unix platforms. Keep
// stdout/check portable and fail closed until a platform's write policy exists.
const writeSupported = false

func checkWriteMetadata(_, _ os.FileInfo) error {
	return errors.New("--write is only supported on macOS and Linux")
}

func preserveMetadata(_ *os.File, _ os.FileInfo) error {
	return errors.New("--write is only supported on macOS and Linux")
}
