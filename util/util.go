package util

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/facebookgo/symwalk"
	"github.com/mholt/archiver/v3"
)

func CompressDir(sourcePath string, ignores []string) (string, error) {
	// Generate source pkg
	tmpfile, err := ioutil.TempFile("", "valar")
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
	tgz := archiver.NewTarGz()
	tgz.Create(tmpfile)
	defer tgz.Close()
	err = symwalk.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}
		for _, prefix := range ignores {
			if strings.HasPrefix(name, prefix) {
				return nil
			}
		}
		var file io.ReadCloser
		if info.Mode().IsRegular() {
			file, err = os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
		}
		return tgz.Write(archiver.File{
			FileInfo: archiver.FileInfo{
				FileInfo:   info,
				CustomName: name,
			},
			ReadCloser: file,
		})
	})
	if err != nil {
		return "", err
	}
	return tmpfile.Name(), nil
}
