package util

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/facebookgo/symwalk"
)

// CompressDir writes sourcePath to a temporary gzipped tarball and returns its
// path. Entries whose path relative to sourcePath starts with one of ignores
// are left out.
func CompressDir(sourcePath string, ignores []string) (string, error) {
	tmpfile, err := os.CreateTemp("", "valar")
	if err != nil {
		return "", err
	}
	defer tmpfile.Close()
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return "", err
	}
	if !sourceInfo.IsDir() {
		return "", fmt.Errorf("expected directory")
	}
	gz := gzip.NewWriter(tmpfile)
	defer gz.Close()
	archive := tar.NewWriter(gz)
	defer archive.Close()

	err = symwalk.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}
		if name == "." {
			return nil
		}
		for _, prefix := range ignores {
			if strings.HasPrefix(name, prefix) {
				return nil
			}
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(name)
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(archive, file)
		return err
	})
	if err != nil {
		return "", err
	}
	// Close in order so every byte reaches the file before it is read back.
	if err := archive.Close(); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	return tmpfile.Name(), nil
}
