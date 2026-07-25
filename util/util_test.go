package util_test

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/valar/cli/util"
)

// readArchive returns the regular-file entries of a gzipped tarball keyed by
// name, plus the names of every entry it contains.
func readArchive(t *testing.T, path string) (map[string]string, []string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()

	files := map[string]string{}
	var names []string
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read entry: %v", err)
		}
		names = append(names, header.Name)
		if header.Typeflag != tar.TypeReg {
			continue
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read %s: %v", header.Name, err)
		}
		files[header.Name] = string(body)
	}
	return files, names
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCompressDir_ArchivesTreeAndHonoursIgnores(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "main.go"), "package main\n")
	writeFile(t, filepath.Join(source, "nested", "lib.go"), "package nested\n")
	writeFile(t, filepath.Join(source, ".git", "HEAD"), "ref: refs/heads/master\n")

	archive, err := util.CompressDir(source, []string{".git"})
	if err != nil {
		t.Fatalf("CompressDir: %v", err)
	}
	defer os.Remove(archive)

	files, names := readArchive(t, archive)

	if got, want := files["main.go"], "package main\n"; got != want {
		t.Errorf("main.go = %q, want %q", got, want)
	}
	// Paths are slash-separated and relative to the source directory.
	if got, want := files["nested/lib.go"], "package nested\n"; got != want {
		t.Errorf("nested/lib.go = %q, want %q", got, want)
	}
	for _, name := range names {
		if name == ".git" || name == ".git/HEAD" {
			t.Errorf("ignored entry %q made it into the archive", name)
		}
		if name == "." {
			t.Error("archive contains a \".\" entry")
		}
	}
	if len(files) != 2 {
		t.Errorf("got %d regular files %v, want 2", len(files), names)
	}
}

func TestCompressDir_RejectsNonDirectories(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plain.txt")
	writeFile(t, file, "hello")

	if _, err := util.CompressDir(file, nil); err == nil {
		t.Fatal("expected an error for a non-directory source")
	}
}
