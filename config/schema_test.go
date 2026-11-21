package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite schema/valar.yml.json")

const schemaPath = "../schema/valar.yml.json"

// TestSchema_MatchesCheckedInFile keeps schema/valar.yml.json in step with the
// config types. Regenerate it with: go test ./config -run TestSchema -update
func TestSchema_MatchesCheckedInFile(t *testing.T) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(Schema()); err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile(schemaPath, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("%s is out of date, run: go test ./config -run TestSchema -update", schemaPath)
	}
}

func TestReadFromFile_Validation(t *testing.T) {
	for _, tc := range []struct {
		Name    string
		Config  string
		WantErr string
	}{
		{"Minimal", "project: p\nservice: s\n", ""},
		{"Full", `project: p
service: s
build:
  constructor: go
  ignore: [.git]
  environment:
    - A=1
    - key: B
      value: "2"
      secret: true
deployment:
  skip: false
  environment: []
`, ""},
		// Deprecated lists stay accepted; pushes warn about them.
		{"DeprecatedEnvironment", "project: p\nservice: s\ndeployment:\n  environment:\n    - A=1\n", ""},
		{"NullSections", "project: p\nservice: s\nbuild:\ndeployment:\n", ""},
		{"UnknownTopLevel", "project: p\nservice: s\nregion: eu\n", `line 3: unknown key "region" at the top level`},
		{"UnknownInBuild", "project: p\nbuild:\n  constructr: go\n", `line 3: unknown key "constructr" in build`},
		{"Settings", "project: p\nservice: s\ndeployment:\n  cpu: 500\n  memory: 512\n  http: true\n  minRunning: 0\n", ""},
		{"SettingNotInteger", "project: p\ndeployment:\n  cpu: 500m\n", "cannot unmarshal"},
		{"UnknownSetting", "project: p\ndeployment:\n  disk: 10\n", `line 3: unknown key "disk" in deployment`},
		{"UnknownInEnvEntry", "build:\n  environment:\n    - key: A\n      val: x\n", `line 4: unknown key "val" in build.environment[0]`},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".valar.yml")
			if err := os.WriteFile(path, []byte(tc.Config), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir)
			var cfg ServiceConfigYAML
			err := cfg.ReadFromFile(".valar.yml")
			if tc.WantErr == "" {
				if err != nil {
					t.Fatalf("ReadFromFile: %v", err)
				}
				return
			}
			var cfgErr *Error
			if !errors.As(err, &cfgErr) {
				t.Fatalf("got %v, want a *config.Error", err)
			}
			for _, want := range []string{path, tc.WantErr} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
		})
	}
}

func TestDeploymentConfig_Settings(t *testing.T) {
	for _, tc := range []struct {
		Name   string
		Config string
		Want   string
	}{
		{"None", "project: p\ndeployment:\n  skip: false\n", "null"},
		{"NoSection", "project: p\n", "null"},
		{"NullKey", "project: p\ndeployment:\n  cpu:\n", "null"},
		{"Some", "project: p\ndeployment:\n  memory: 2048\n", `{"memory":2048}`},
		{"All", "project: p\ndeployment:\n  cpu: 500\n  memory: 512\n  http: true\n  minRunning: 0\n", `{"cpu":500,"http":true,"memory":512,"minRunning":0}`},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, ".valar.yml"), []byte(tc.Config), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir)
			var cfg ServiceConfigYAML
			if err := cfg.ReadFromFile(".valar.yml"); err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(cfg.Deployment.Settings())
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.Want {
				t.Errorf("Settings() = %s, want %s", got, tc.Want)
			}
		})
	}
}
