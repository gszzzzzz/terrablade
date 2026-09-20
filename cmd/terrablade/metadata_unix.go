//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"syscall"
)

const writeSupported = true

func checkWriteMetadata(original, current os.FileInfo) error {
	before := original.Sys().(*syscall.Stat_t)
	after := current.Sys().(*syscall.Stat_t)
	if after.Nlink != 1 {
		return errors.New("refusing to replace a file with multiple hard links")
	}
	if before.Uid != after.Uid || before.Gid != after.Gid {
		return errors.New("file ownership changed while formatting")
	}
	return nil
}

func preserveMetadata(temp *os.File, original os.FileInfo) error {
	info, err := temp.Stat()
	if err != nil {
		return err
	}
	from := original.Sys().(*syscall.Stat_t)
	to := info.Sys().(*syscall.Stat_t)
	if from.Uid != to.Uid || from.Gid != to.Gid {
		if err := temp.Chown(int(from.Uid), int(from.Gid)); err != nil {
			return err
		}
	}
	return temp.Chmod(original.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky))
}
