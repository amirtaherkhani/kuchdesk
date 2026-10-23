package serviceaccess

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Read runs the catalog-constrained, read-only Host status command.
func Read(ctx context.Context, root string) (map[string]any, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("absolute KuchDesk project root required")
	}
	script := filepath.Join(root, "lab", "scripts", "service_access.py")
	info, err := os.Lstat(script)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("service access status command unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "python3", script, "status")
	command.Dir = root
	output, err := command.Output()
	var report map[string]any
	if err != nil || len(output) > 1<<20 || json.Unmarshal(output, &report) != nil || report == nil {
		return nil, errors.New("service access status unavailable")
	}
	return report, nil
}
