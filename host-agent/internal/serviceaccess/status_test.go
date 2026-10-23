package serviceaccess

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadUsesOnlyFixedStatusCommand(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "lab", "scripts", "service_access.py")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("import json\nprint(json.dumps({'context':'colima-kuchdesk-fresh','services':{'nats':{'state':'unknown'}}}))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Read(context.Background(), root)
	encoded, _ := json.Marshal(report)
	if err != nil || !strings.Contains(string(encoded), `"state":"unknown"`) {
		t.Fatalf("fixed read-only status command failed: %v", err)
	}
	if _, err := Read(context.Background(), "relative-root"); err == nil {
		t.Fatal("relative project root accepted")
	}
	if err := os.Remove(script); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/bin/false", script); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(context.Background(), root); err == nil {
		t.Fatal("symlinked status script accepted")
	}
}
