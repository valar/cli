package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectConstructor(t *testing.T) {
	for _, tc := range []struct {
		Name     string
		Files    []string
		Want     string
		WantNote string
	}{
		{"Go", []string{"go.mod", "main.go"}, "go", "detected constructor: go (go.mod)\n"},
		{"Node", []string{"package.json", "index.js"}, "node", "detected constructor: node (package.json)\n"},
		{"PythonRequirements", []string{"requirements.txt", "app.py"}, "python", "detected constructor: python (requirements.txt)\n"},
		{"PythonProject", []string{"pyproject.toml"}, "python", "detected constructor: python (pyproject.toml)\n"},
		{"Static", []string{"index.html", "style.css"}, "static", "detected constructor: static (index.html)\n"},
		{"Dockerfile", []string{"Dockerfile", "server.rs"}, "docker", "detected constructor: docker (Dockerfile)\n"},
		// A site built with npm has an index.html too; package.json wins.
		{"NodeOverStatic", []string{"index.html", "package.json"}, "node", "detected constructor: node (package.json)\n"},
		{"GoOverDockerfile", []string{"Dockerfile", "go.mod"}, "go", "detected constructor: go (go.mod)\n"},
		{"DirectoryIsNoMarker", []string{"go.mod/", "index.html"}, "static", "detected constructor: static (index.html)\n"},
		{"Nothing", []string{"README.md"}, "", ""},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tc.Files {
				path := filepath.Join(dir, name)
				var err error
				if strings.HasSuffix(name, "/") {
					err = os.Mkdir(path, 0o755)
				} else {
					err = os.WriteFile(path, nil, 0o644)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			var note bytes.Buffer
			got, err := detectConstructor(dir, &note)
			if tc.Want == "" {
				if err == nil {
					t.Fatalf("detected %q, want an error", got)
				}
				if kind := classify(err, kindFailed); kind != kindInvalid {
					t.Errorf("error kind %s, want invalid", kind)
				}
				for _, want := range []string{dir, "--type", "node-static", "docker"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q lacks %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("detectConstructor: %v", err)
			}
			if got != tc.Want || note.String() != tc.WantNote {
				t.Errorf("got %q with note %q, want %q with %q", got, note.String(), tc.Want, tc.WantNote)
			}
		})
	}
}
