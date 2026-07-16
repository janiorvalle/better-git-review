package fileutil

import (
	"errors"
	"os"
	"runtime"
)

func Replace(tempPath, targetPath string) error {
	return replace(runtime.GOOS, tempPath, targetPath, os.Rename, os.Remove)
}

func replace(
	goos string,
	tempPath string,
	targetPath string,
	rename func(string, string) error,
	remove func(string) error,
) error {
	if err := rename(tempPath, targetPath); err == nil {
		return nil
	} else if goos != "windows" {
		return err
	}
	if err := remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return rename(tempPath, targetPath)
}
