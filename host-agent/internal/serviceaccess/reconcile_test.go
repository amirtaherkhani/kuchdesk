package serviceaccess

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReconciliationBudgetConflictsRemovalAndRecovery(t *testing.T) {
	entry := map[string]any{
		"state": "configured-not-listening", "freshness": "fresh",
		"backendIdentity": "helm-owned-selector-and-ready-endpoint",
		"kubernetes":      map[string]any{"publicationUID": "fixture-uid"},
	}
	services := map[string]any{"fixture": entry}
	count := 0
	r := Reconciler{
		Read:    func(context.Context) (map[string]any, error) { return map[string]any{"services": services}, nil },
		Recover: func(context.Context, string) string { count++; return "failed" },
	}
	now := time.Now()
	for _, state := range []string{"conflict", "unknown", "stale", "disabled"} {
		entry["state"] = state
		r.Cycle(context.Background(), now)
	}
	if count != 0 {
		t.Fatal("attempted recovery of unsafe state")
	}
	entry["state"] = "configured-not-listening"
	entry["freshness"] = "stale"
	r.Cycle(context.Background(), now)
	if count != 0 {
		t.Fatal("attempted recovery from stale evidence")
	}
	entry["freshness"] = "fresh"
	r.Cycle(context.Background(), now)
	r.Cycle(context.Background(), now.Add(time.Second))
	if count != 1 {
		t.Fatal("backoff not enforced")
	}
	for i := 1; i <= 5; i++ {
		r.Cycle(context.Background(), now.Add(time.Duration(i)*10*time.Minute))
	}
	if count != 3 {
		t.Fatalf("attempt budget exceeded: %d", count)
	}
	entry["state"] = "reachable"
	r.Cycle(context.Background(), now.Add(time.Hour))
	r.Cycle(context.Background(), now.Add(time.Hour+30*time.Second))
	entry["state"] = "configured-not-listening"
	r.Cycle(context.Background(), now.Add(2*time.Hour))
	if count != 4 {
		t.Fatal("new incident did not recover after confirmed health")
	}
	delete(services, "fixture")
	r.Cycle(context.Background(), now.Add(3*time.Hour))
	if len(r.incidents) != 0 {
		t.Fatal("removed publication retained retry state")
	}
}

func TestUnsupportedRecoveryDoesNotRepeat(t *testing.T) {
	count := 0
	r := Reconciler{
		Read: func(context.Context) (map[string]any, error) {
			return map[string]any{"services": map[string]any{"udp": map[string]any{
				"state": "configured-not-listening", "freshness": "fresh",
				"backendIdentity": "helm-owned-selector-and-ready-endpoint",
				"kubernetes":      map[string]any{"publicationUID": "udp-uid"},
			}}}, nil
		},
		Recover: func(context.Context, string) string { count++; return "unsupported" },
	}
	for i := 0; i < 5; i++ {
		r.Cycle(context.Background(), time.Now().Add(time.Duration(i)*time.Hour))
	}
	if count != 1 {
		t.Fatalf("unsupported operation retried: %d", count)
	}
}

func TestRecoveryBudgetSurvivesRestartAndRejectsUnsafeState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "forwarding-recovery.json")
	state := map[string]incident{"fixture": {UID: "fixture-uid", Attempts: 3, Next: time.Now().UTC()}}
	if err := saveIncidents(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadIncidents(path)
	if err != nil || loaded["fixture"].Attempts != 3 {
		t.Fatal("retry budget did not survive reload")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadIncidents(path); err == nil {
		t.Fatal("readable private state accepted")
	}
}

func TestCleanupRunsBeforeRead(t *testing.T) {
	cleaned := false
	r := Reconciler{Cleanup: func(context.Context) { cleaned = true }, Read: func(context.Context) (map[string]any, error) {
		if !cleaned {
			t.Fatal("read preceded cleanup")
		}
		return map[string]any{}, nil
	}}
	r.Cycle(context.Background(), time.Now())
}
