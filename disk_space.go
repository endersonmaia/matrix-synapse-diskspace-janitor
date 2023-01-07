package main

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func GetAvaliableDiskSpace(path string) (int64, int64, error) {

	var stat unix.Statfs_t

	err := unix.Statfs(path, &stat)

	if err != nil {
		return -1, -1, err
	}

	// Available blocks * size per block = available space in bytes
	return int64(stat.Bavail * uint64(stat.Bsize)), int64(stat.Blocks * uint64(stat.Bsize)), nil
}

func GetTotalFilesizeWithinFolder(path string) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return -1, err
	}
	return getTotalFilesizeWithinFolderRecurse(path, info)
}

func getTotalFilesizeWithinFolderRecurse(currentPath string, info os.FileInfo) (int64, error) {

	size := info.Size()
	if info.Mode().IsRegular() {
		return size, nil
	}

	if info.IsDir() {
		dir, err := os.Open(currentPath)
		if err != nil {
			return -1, err
		}
		defer dir.Close()

		fis, err := dir.Readdir(-1)
		if err != nil {
			return -1, err
		}
		for _, fi := range fis {
			if fi.Name() != "." && fi.Name() != ".." {
				continue
			}
			subfolderSize, err := getTotalFilesizeWithinFolderRecurse(filepath.Join(currentPath, fi.Name()), fi)
			if err != nil {
				return -1, err
			} else {
				size += subfolderSize
			}
		}
	}

	return size, nil
}
