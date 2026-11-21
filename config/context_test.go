package config_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valar/cli/config"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{config.EnvToken, config.EnvEndpoint, config.EnvProject, "VALARCONFIG"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const fileConfig = `activeContext: default
endpoints:
  default:
    token: file-token
    url: https://valar.example/v2
contexts:
  default:
    endpoint: default
    project: fileproject
`

func TestCLIConfig_EnvironmentOverridesContext(t *testing.T) {
	clearEnv(t)
	t.Setenv("VALARCONFIG", writeFile(t, t.TempDir(), "config", fileConfig))
	t.Setenv(config.EnvToken, "env-token")
	t.Setenv(config.EnvProject, "envproject")

	cfg, err := config.NewCLIConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Token(); got != "env-token" {
		t.Errorf("Token() = %q, want env-token", got)
	}
	if got := cfg.Project(); got != "envproject" {
		t.Errorf("Project() = %q, want envproject", got)
	}
	if got := cfg.Endpoint(); got != "https://valar.example/v2" {
		t.Errorf("Endpoint() = %q, want the configured one", got)
	}

	t.Setenv(config.EnvEndpoint, "https://self.hosted/v2")
	cfg, err = config.NewCLIConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Endpoint(); got != "https://self.hosted/v2" {
		t.Errorf("Endpoint() = %q, want VALAR_ENDPOINT", got)
	}
}

func TestCLIConfig_TokenWithoutConfigFile(t *testing.T) {
	clearEnv(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.EnvToken, "env-token")

	stderr := captureStderr(t, func() {
		cfg, err := config.NewCLIConfigFromEnvironment()
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.Endpoint(); got != config.DefaultEndpoint {
			t.Errorf("Endpoint() = %q, want %q", got, config.DefaultEndpoint)
		}
	})
	if stderr != "" {
		t.Errorf("a missing default config file printed %q", stderr)
	}
}

func TestServiceConfig_ServiceOverrideKeepsBuild(t *testing.T) {
	clearEnv(t)
	t.Setenv("VALARCONFIG", writeFile(t, t.TempDir(), "config", fileConfig))
	dir := t.TempDir()
	writeFile(t, dir, ".valar.yml", "project: p\nservice: web\nbuild:\n  constructor: static\ndeployment: {}\n")
	t.Chdir(dir)

	cli, err := config.NewCLIConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	preview := "webpr12"
	cfg, err := config.NewServiceConfigWithFallback(".valar.yml", &preview, cli)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project() != "p" || cfg.Service() != "webpr12" || cfg.Build().Constructor != "static" {
		t.Errorf("got project %q, service %q, constructor %q", cfg.Project(), cfg.Service(), cfg.Build().Constructor)
	}

	t.Setenv(config.EnvProject, "q")
	cli, err = config.NewCLIConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = config.NewServiceConfigWithFallback(".valar.yml", nil, cli)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project() != "q" || cfg.Service() != "web" {
		t.Errorf("got project %q, service %q, want q and web", cfg.Project(), cfg.Service())
	}
}

func TestServiceConfig_BrokenFileIsAnError(t *testing.T) {
	clearEnv(t)
	t.Setenv("VALARCONFIG", writeFile(t, t.TempDir(), "config", fileConfig))
	dir := t.TempDir()
	writeFile(t, dir, ".valar.yml", "project: [unclosed\n")
	t.Chdir(dir)

	cli, err := config.NewCLIConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.NewServiceConfigWithFallback(".valar.yml", nil, cli); err == nil {
		t.Error("a malformed .valar.yml was accepted")
	}
}

func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	f()
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

func perm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestCLIConfig_WriteKeepsTokensPrivate(t *testing.T) {
	for _, tc := range []struct {
		Name     string
		Explicit bool
		Existing os.FileMode // 0 for no file
		WantNote bool
	}{
		{"NewDefault", false, 0, false},
		{"PrivateDefault", false, 0o600, false},
		{"WorldReadableDefault", false, 0o644, true},
		{"GroupReadableExplicit", true, 0o640, true},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			clearEnv(t)
			home := t.TempDir()
			t.Setenv("HOME", home)
			path := filepath.Join(home, ".valar", "config")
			if tc.Explicit {
				path = filepath.Join(t.TempDir(), "valarconfig")
				t.Setenv("VALARCONFIG", path)
			}
			if tc.Existing != 0 {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Dir(path), filepath.Base(path), fileConfig)
				if err := os.Chmod(path, tc.Existing); err != nil {
					t.Fatal(err)
				}
			}

			var cfg *config.CLIConfig
			note := captureStderr(t, func() {
				var err error
				if cfg, err = config.NewCLIConfigFromEnvironment(); err != nil {
					t.Fatal(err)
				}
				cfg.ActiveContext = "default"
				if err := cfg.Write(); err != nil {
					t.Fatalf("Write: %v", err)
				}
			})

			if got := perm(t, path); got != 0o600 {
				t.Errorf("config mode %o, want 600", got)
			}
			if !tc.Explicit && tc.Existing == 0 {
				if got := perm(t, filepath.Join(home, ".valar")); got != 0o700 {
					t.Errorf("directory mode %o, want 700", got)
				}
			}
			if tc.WantNote != (note != "") {
				t.Errorf("stderr %q, want a note: %v", note, tc.WantNote)
			}
			if tc.WantNote && (strings.Count(note, "\n") != 1 || !strings.Contains(note, path) || !strings.Contains(note, "0600")) {
				t.Errorf("note %q is not one line naming %s and its new mode", note, path)
			}
			if tc.Existing != 0 {
				data, err := os.ReadFile(path)
				if err != nil || !strings.Contains(string(data), "file-token") {
					t.Errorf("rewritten config lost its contents: %q, %v", data, err)
				}
			}
		})
	}
}
