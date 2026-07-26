package cmd

import (
	"testing"
	"time"

	"github.com/valar/cli/api"
)

func TestServiceKind(t *testing.T) {
	for _, tc := range []struct{ In, Want string }{
		{"function", "function"},
		{"batch", "batch"},
		// A server predating the field must not be reported as "function":
		// that would present a guess as fact.
		{"", "-"},
		{"something-new", "something-new"},
	} {
		if got := serviceKind(tc.In); got != tc.Want {
			t.Errorf("serviceKind(%q) = %q, want %q", tc.In, got, tc.Want)
		}
	}
}

// Humanising the zero timestamp yields "a long while ago", which reads as
// deployed in the distant past. A batch service is never deployed at all.
func TestDeployedColumns_NeverDeployed(t *testing.T) {
	svc := api.Service{Name: "batchdemo", Kind: "batch"}
	if got := lastDeployed(svc); got != "-" {
		t.Errorf("lastDeployed = %q, want -", got)
	}
	if got := deployedVersion(svc); got != "-" {
		t.Errorf("deployedVersion = %q, want -", got)
	}
}

func TestDeployedColumns_Deployed(t *testing.T) {
	svc := api.Service{Name: "web", Kind: "function", Deployment: 7, DeployedAt: time.Now().Add(-time.Hour)}
	if got := deployedVersion(svc); got != "7" {
		t.Errorf("deployedVersion = %q, want 7", got)
	}
	if got := lastDeployed(svc); got == "-" {
		t.Error("lastDeployed should render a time for a deployed service")
	}
}

func TestServiceIsBatch(t *testing.T) {
	if !(api.Service{Kind: "batch"}).IsBatch() {
		t.Error("batch service should report IsBatch")
	}
	for _, kind := range []string{"function", ""} {
		if (api.Service{Kind: kind}).IsBatch() {
			t.Errorf("kind %q should not report IsBatch", kind)
		}
	}
}
