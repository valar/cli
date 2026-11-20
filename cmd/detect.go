package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// constructorMarkers are checked in order; the first file present picks the
// constructor.
var constructorMarkers = []struct{ File, Constructor string }{
	{"go.mod", "go"},
	{"package.json", "node"},
	{"requirements.txt", "python"},
	{"pyproject.toml", "python"},
	{"index.html", "static"},
	{"Dockerfile", "docker"},
}

// detectConstructor picks the constructor for the project in dir from its
// files and names the choice on w.
func detectConstructor(dir string, w io.Writer) (string, error) {
	for _, m := range constructorMarkers {
		if info, err := os.Stat(filepath.Join(dir, m.File)); err == nil && !info.IsDir() {
			fmt.Fprintf(w, "detected constructor: %s (%s)\n", m.Constructor, m.File)
			return m.Constructor, nil
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return "", newError(kindInvalid, "cannot detect the constructor: %s has none of go.mod, package.json, requirements.txt, pyproject.toml, index.html or Dockerfile; pass --type with one of go, node, node-static, python, static or docker", dir)
}
