package serviceaccess

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

type incident struct {
	UID      string    `json:"uid"`
	Attempts int       `json:"attempts"`
	Next     time.Time `json:"next"`
	Healthy  int       `json:"healthy"`
}

// Reconciler reuses native forwarding; it never owns a network listener.
type Reconciler struct {
	incidents map[string]incident
	Verify    func(context.Context) error
	Cleanup   func(context.Context)
	Read      func(context.Context) (map[string]any, error)
	Recover   func(context.Context, string) string
	save      func(map[string]incident) error
}

func (r *Reconciler) verify(ctx context.Context) error {
	if r.Verify == nil {
		return errors.New("verified environment runtime identity required")
	}
	return r.Verify(ctx)
}

// persist is shared by the attempt-budget save and the final cycle save.
func (r *Reconciler) persist(ctx context.Context, state map[string]incident) error {
	if err := r.verify(ctx); err != nil {
		return err
	}
	if r.save == nil {
		return nil
	}
	return r.save(state)
}

func (r *Reconciler) Cycle(ctx context.Context, now time.Time) bool {
	if r.verify(ctx) != nil {
		return false
	}
	if r.Cleanup != nil {
		r.Cleanup(ctx)
	}
	report, err := r.Read(ctx)
	// Cleanup/report collection may span a profile or kube-context replacement.
	// Never accept that report or mutate the owned budget until pins revalidate.
	if err != nil || r.verify(ctx) != nil {
		return false
	}
	services, ok := report["services"].(map[string]any)
	if !ok {
		return false
	}
	working := make(map[string]incident, len(r.incidents))
	for id, previous := range r.incidents {
		working[id] = previous
	}
	for id := range working {
		if _, exists := services[id]; !exists {
			delete(working, id)
		}
	}
	for id, raw := range services {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kube, _ := entry["kubernetes"].(map[string]any)
		uid, _ := kube["publicationUID"].(string)
		if entry["state"] == "disabled" || uid == "" {
			delete(working, id)
			continue
		}
		previous := working[id]
		if previous.UID != uid {
			previous = incident{UID: uid}
		}
		if entry["state"] == "reachable" {
			if previous.Healthy < 2 {
				previous.Healthy++
			}
			if previous.Healthy >= 2 {
				previous.Attempts = 0
			}
		} else {
			previous.Healthy = 0
		}
		// Preserve an incident's budget across transient health and conflicts.
		working[id] = previous
		if entry["state"] != "configured-not-listening" || entry["freshness"] != "fresh" ||
			entry["backendIdentity"] != "helm-owned-selector-and-ready-endpoint" ||
			previous.Attempts >= 3 || now.Before(previous.Next) {
			continue
		}
		previous.Attempts++
		previous.Next = now.Add(time.Duration(1<<previous.Attempts) * 30 * time.Second)
		working[id] = previous
		if r.persist(ctx, working) != nil {
			log.Print("service forwarding reconciliation skipped: retry budget could not be saved")
			return false
		}
		// A successful write consumes this attempt even if runtime verification
		// fails next. Retain that durable snapshot without aliasing later edits.
		r.incidents = make(map[string]incident, len(working))
		for savedID, saved := range working {
			r.incidents[savedID] = saved
		}
		if r.verify(ctx) != nil {
			return false
		}
		result := r.Recover(ctx, id)
		log.Printf("service forwarding endpoint=%s recovery=%s attempt=%d", id, result, previous.Attempts)
		if result == "unsupported" {
			previous.Attempts = 3
		}
		working[id] = previous
	}
	if r.verify(ctx) != nil {
		return false
	}
	r.incidents = working
	return true
}

func loadIncidents(path string) (map[string]incident, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]incident), nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() > 64<<10 {
		return nil, errors.New("private recovery state unavailable")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Getuid() {
		return nil, errors.New("private recovery state owner invalid")
	}
	data, err := os.ReadFile(path)
	state := make(map[string]incident)
	if err != nil || json.Unmarshal(data, &state) != nil || state == nil {
		return nil, errors.New("private recovery state invalid")
	}
	for _, item := range state {
		if item.Attempts < 0 || item.Attempts > 3 || item.Healthy < 0 {
			return nil, errors.New("private recovery budget invalid")
		}
	}
	return state, nil
}

func saveIncidents(path string, state map[string]incident) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".forwarding-state-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// recoveryBinding retains the entire validated ownership identity, not just its
// stable state location. Rebinding at the same location invalidates this worker.
type recoveryBinding struct {
	Version        int    `json:"version"`
	EnvironmentID  string `json:"environmentId"`
	Repository     string `json:"repository"`
	Profile        string `json:"profile"`
	Context        string `json:"context"`
	ColimaHome     string `json:"colimaHome"`
	ProfileDevice  uint64 `json:"profileDevice"`
	ProfileInode   uint64 `json:"profileInode"`
	MachineID      string `json:"machineID"`
	ClusterUID     string `json:"clusterUID"`
	StateDirectory string `json:"stateDirectory"`
}

func (b recoveryBinding) statePath() string {
	return filepath.Join(b.StateDirectory, "forwarding-recovery.json")
}

// root still locates installed source code; it no longer owns runtime state.
func readRecoveryBinding(ctx context.Context, root string) (recoveryBinding, error) {
	var binding recoveryBinding
	if !filepath.IsAbs(root) {
		return binding, errors.New("absolute KuchDesk project root required")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", filepath.Join(root, "lab/scripts/environment_binding.py"), "status", "--root", root)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil || len(output) > 64<<10 || json.Unmarshal(output, &binding) != nil ||
		binding.Version != 2 || binding.EnvironmentID == "" || binding.MachineID == "" || binding.ClusterUID == "" ||
		binding.Repository != "github.com/amirtaherkhani/kuchdesk" || binding.Profile != "kuchdesk-fresh" || binding.Context != "colima-kuchdesk-fresh" ||
		!filepath.IsAbs(binding.StateDirectory) || filepath.Clean(binding.StateDirectory) != binding.StateDirectory {
		return recoveryBinding{}, errors.New("validated environment recovery binding unavailable")
	}
	return binding, nil
}

func verifyRecoveryBinding(ctx context.Context, root string, expected recoveryBinding) error {
	current, err := readRecoveryBinding(ctx, root)
	if err != nil || current != expected {
		return errors.New("environment binding changed")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// This command only compares independently observed VM/cluster pins. Status
	// remains read-only and cannot authorize reconciliation on another cluster.
	cmd := exec.CommandContext(ctx, "python3", filepath.Join(root, "lab/scripts/service_access.py"), "verify-binding")
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		return errors.New("environment runtime identity unavailable or mismatched")
	}
	current, err = readRecoveryBinding(ctx, root)
	if err != nil || current != expected {
		return errors.New("environment binding changed during runtime verification")
	}
	return nil
}

// RunReconciler is opt-in under the existing Host Bridge lifecycle.
func RunReconciler(ctx context.Context, root string) {
	binding, err := readRecoveryBinding(ctx, root)
	if err != nil {
		log.Print("service forwarding reconciliation disabled: environment binding unavailable")
		return
	}
	if verifyRecoveryBinding(ctx, root, binding) != nil {
		log.Print("service forwarding reconciliation disabled: runtime identity unavailable")
		return
	}
	statePath := binding.statePath()
	state, err := loadIncidents(statePath)
	if err != nil {
		log.Print("service forwarding reconciliation disabled: private state unavailable")
		return
	}
	r := Reconciler{
		incidents: state,
		Verify:    func(ctx context.Context) error { return verifyRecoveryBinding(ctx, root, binding) },
		Cleanup: func(ctx context.Context) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "python3", filepath.Join(root, "lab/scripts/service_access.py"), "cleanup")
			cmd.Dir = root
			if err := cmd.Run(); err != nil {
				log.Print("native forward cleanup unavailable; owned mapping preserved")
			}
		},
		save: func(state map[string]incident) error {
			if err := verifyRecoveryBinding(ctx, root, binding); err != nil {
				return err
			}
			return saveIncidents(statePath, state)
		},
		Read: func(ctx context.Context) (map[string]any, error) { return Read(ctx, root) },
		Recover: func(ctx context.Context, id string) string {
			ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "python3", filepath.Join(root, "lab/scripts/service_access.py"), "recover", "--service", id, "--grpc-recovery")
			cmd.Dir = root
			output, err := cmd.Output()
			var result struct {
				State string `json:"state"`
			}
			if err != nil || len(output) > 64<<10 || json.Unmarshal(output, &result) != nil {
				return "failed"
			}
			return result.State
		},
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if !r.Cycle(ctx, now) {
				continue
			}
			if r.persist(ctx, r.incidents) != nil {
				log.Print("service forwarding reconciliation stopped: retry budget could not be saved")
				return
			}
		}
	}
}
