package cmd

import (
	"testing"
)

func TestParseBatchEnv(t *testing.T) {
	for _, tc := range []struct {
		Name    string
		Input   []string
		Want    map[string]string
		WantErr bool
	}{
		{"Empty", nil, nil, false},
		{"Single", []string{"A=1"}, map[string]string{"A": "1"}, false},
		{"Multiple", []string{"A=1", "B=2"}, map[string]string{"A": "1", "B": "2"}, false},
		// A value may legitimately contain '=' -- only the first splits.
		{"ValueWithEquals", []string{"DSN=a=b"}, map[string]string{"DSN": "a=b"}, false},
		{"EmptyValue", []string{"A="}, map[string]string{"A": ""}, false},
		{"NoEquals", []string{"A"}, nil, true},
		{"NoKey", []string{"=1"}, nil, true},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := parseBatchEnv(tc.Input)
			if tc.WantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tc.Input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.Want) {
				t.Fatalf("got %v, want %v", got, tc.Want)
			}
			for k, v := range tc.Want {
				if got[k] != v {
					t.Errorf("key %q: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestValidateServiceKind(t *testing.T) {
	for _, kind := range []string{"", "function", "batch"} {
		if err := validateServiceKind(kind); err != nil {
			t.Errorf("kind %q should be accepted: %v", kind, err)
		}
	}
	for _, kind := range []string{"job", "Batch", "BATCH"} {
		if err := validateServiceKind(kind); err == nil {
			t.Errorf("kind %q should be rejected", kind)
		}
	}
}

// Every gate the server can report should map to an explanation. An unmapped
// gate would surface as a bare identifier, which tells a user nothing about
// what to do next.
func TestBatchGateReason_CoversEveryServerGate(t *testing.T) {
	for _, gate := range []string{"follower_fit", "overcommit", "batch_budget", "dispatch_window", "stale_view"} {
		if reason := batchGateReason(gate); reason == gate {
			t.Errorf("gate %q has no explanation", gate)
		}
	}
	if got := batchGateReason("something_new"); got != "something_new" {
		t.Errorf("unknown gate should pass through, got %q", got)
	}
}

func TestZeroAsDefault(t *testing.T) {
	if got := zeroAsDefault(0, "fallback"); got != "fallback" {
		t.Errorf("got %q, want fallback", got)
	}
	if got := zeroAsDefault(3, "fallback"); got != "3" {
		t.Errorf("got %q, want 3", got)
	}
}
