package serviceaccess

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestRecoveryBindingRejectsSamePathChangedIdentity(t *testing.T) {
	root := t.TempDir()
	scripts := filepath.Join(root, "lab/scripts")
	if err := os.MkdirAll(scripts, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"version": 2, "environmentId": "fixture-environment", "machineID": "fixture-machine",
		"clusterUID": "fixture-cluster", "repository": "github.com/amirtaherkhani/kuchdesk",
		"profile": "kuchdesk-fresh", "context": "colima-kuchdesk-fresh", "stateDirectory": t.TempDir(),
	}
	write := func() {
		data, _ := json.Marshal(payload)
		if err := os.WriteFile(filepath.Join(scripts, "environment_binding.py"), []byte("print("+strconv.Quote(string(data))+")\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	expected, err := readRecoveryBinding(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"environmentId", "machineID", "clusterUID"} {
		t.Run(key, func(t *testing.T) {
			original := payload[key]
			payload[key] = "replacement"
			write()
			if err := verifyRecoveryBinding(context.Background(), root, expected); err == nil {
				t.Fatal("same-path replacement identity accepted")
			}
			payload[key] = original
		})
	}
}

func TestReplacementDuringReadPreservesIncidentsAndSkipsSaves(t *testing.T) {
	original := map[string]incident{"fixture": {UID: "old-uid", Attempts: 3}}
	active := true
	saves, recoveries := 0, 0
	r := Reconciler{
		incidents: map[string]incident{"fixture": original["fixture"]},
		Verify: func(context.Context) error {
			if !active {
				return errors.New("binding replaced")
			}
			return nil
		},
		Read: func(context.Context) (map[string]any, error) {
			active = false
			return map[string]any{"services": map[string]any{"fixture": map[string]any{
				"state": "configured-not-listening", "freshness": "fresh", "backendIdentity": "helm-owned-selector-and-ready-endpoint",
				"kubernetes": map[string]any{"publicationUID": "replacement-uid"},
			}}}, nil
		},
		save:    func(map[string]incident) error { saves++; return nil },
		Recover: func(context.Context, string) string { recoveries++; return "failed" },
	}
	r.Cycle(context.Background(), time.Now())
	if saves != 0 || recoveries != 0 || !reflect.DeepEqual(r.incidents, original) {
		t.Fatalf("replacement report changed owned state: %#v saves=%d recoveries=%d", r.incidents, saves, recoveries)
	}
}

func TestWrongLiveClusterPreservesBudgetWithoutReadOrSave(t *testing.T) {
	original := map[string]incident{"fixture": {UID: "owned-uid", Attempts: 3}}
	r := Reconciler{
		incidents: map[string]incident{"fixture": original["fixture"]},
		Verify:    func(context.Context) error { return errors.New("runtime identity mismatch") },
		Read: func(context.Context) (map[string]any, error) {
			t.Fatal("unverified cluster report accepted")
			return nil, nil
		},
		save: func(map[string]incident) error { t.Fatal("unverified cluster state saved"); return nil },
		Recover: func(context.Context, string) string {
			t.Fatal("unverified cluster recovery attempted")
			return "failed"
		},
	}
	r.Cycle(context.Background(), time.Now())
	if !reflect.DeepEqual(r.incidents, original) {
		t.Fatal("wrong cluster reset retry budget")
	}
}

func TestReplacementAfterRecoveryRejectsFinalSave(t *testing.T) {
	active, saves := true, 0
	r := Reconciler{
		Verify: func(context.Context) error {
			if !active {
				return errors.New("binding replaced")
			}
			return nil
		},
		Read: func(context.Context) (map[string]any, error) {
			return map[string]any{"services": map[string]any{"fixture": map[string]any{
				"state": "configured-not-listening", "freshness": "fresh", "backendIdentity": "helm-owned-selector-and-ready-endpoint",
				"kubernetes": map[string]any{"publicationUID": "owned-uid"},
			}}}, nil
		},
		save:    func(map[string]incident) error { saves++; return nil },
		Recover: func(context.Context, string) string { active = false; return "failed" },
	}
	r.Cycle(context.Background(), time.Now())
	if err := r.persist(context.Background(), r.incidents); err == nil {
		t.Fatal("final save accepted replacement binding")
	}
	if saves != 1 {
		t.Fatalf("expected only save before replacement, got %d", saves)
	}
}

func TestPersistedBudgetSurvivesTransientVerificationFailure(t *testing.T) {
	active, saves, recoveries := true, 0, 0
	durable := map[string]incident{"fixture": {UID: "owned-uid", Attempts: 2}}
	r := Reconciler{
		incidents: map[string]incident{"fixture": durable["fixture"]},
		Verify: func(context.Context) error {
			if !active {
				return errors.New("runtime identity temporarily mismatched")
			}
			return nil
		},
		Read: func(context.Context) (map[string]any, error) {
			return map[string]any{"services": map[string]any{"fixture": map[string]any{
				"state": "configured-not-listening", "freshness": "fresh",
				"backendIdentity": "helm-owned-selector-and-ready-endpoint",
				"kubernetes":      map[string]any{"publicationUID": "owned-uid"},
			}}}, nil
		},
		save: func(state map[string]incident) error {
			saves++
			durable = make(map[string]incident, len(state))
			for id, item := range state {
				durable[id] = item
			}
			if saves == 1 {
				active = false
			}
			return nil
		},
		Recover: func(context.Context, string) string { recoveries++; return "failed" },
	}
	now := time.Now()
	r.Cycle(context.Background(), now)
	if durable["fixture"].Attempts != 3 || r.incidents["fixture"].Attempts != 3 {
		t.Errorf("successful save must retain consumed budget in memory: durable=%d memory=%d", durable["fixture"].Attempts, r.incidents["fixture"].Attempts)
	}
	active = true
	r.Cycle(context.Background(), now.Add(time.Hour))
	if saves != 1 || recoveries != 0 || durable["fixture"].Attempts != 3 || r.incidents["fixture"].Attempts != 3 {
		t.Fatalf("restored context reused a consumed attempt: saves=%d recoveries=%d durable=%d memory=%d", saves, recoveries, durable["fixture"].Attempts, r.incidents["fixture"].Attempts)
	}
}

func TestRecoveryPathUsesBindingHelperAndFailsClosed(t *testing.T) {
	root := t.TempDir()
	scripts := filepath.Join(root, "lab/scripts")
	if err := os.MkdirAll(scripts, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "forwarding-recovery.json")
	payload, _ := json.Marshal(map[string]any{
		"version": 2, "environmentId": "fixture-environment", "machineID": "fixture-machine", "clusterUID": "fixture-cluster",
		"repository": "github.com/amirtaherkhani/kuchdesk", "profile": "kuchdesk-fresh", "context": "colima-kuchdesk-fresh",
		"stateDirectory": filepath.Dir(path),
	})
	script := filepath.Join(scripts, "environment_binding.py")
	if err := os.WriteFile(script, []byte("print("+strconv.Quote(string(payload))+")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readRecoveryBinding(context.Background(), root)
	if err != nil || got.statePath() != path {
		t.Fatalf("binding path not used: %#v %v", got, err)
	}
	if err := os.WriteFile(script, []byte("raise SystemExit(1)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readRecoveryBinding(context.Background(), root); err == nil {
		t.Fatal("invalid binding accepted")
	}
}

func TestReconciliationBudgetConflictsRemovalAndRecovery(t *testing.T) {
	entry := map[string]any{
		"state": "configured-not-listening", "freshness": "fresh",
		"backendIdentity": "helm-owned-selector-and-ready-endpoint",
		"kubernetes":      map[string]any{"publicationUID": "fixture-uid"},
	}
	services := map[string]any{"fixture": entry}
	count := 0
	r := Reconciler{
		Verify:  func(context.Context) error { return nil },
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
		Verify: func(context.Context) error { return nil },
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
	r := Reconciler{Verify: func(context.Context) error { return nil }, Cleanup: func(context.Context) { cleaned = true }, Read: func(context.Context) (map[string]any, error) {
		if !cleaned {
			t.Fatal("read preceded cleanup")
		}
		return map[string]any{}, nil
	}}
	r.Cycle(context.Background(), time.Now())
}
