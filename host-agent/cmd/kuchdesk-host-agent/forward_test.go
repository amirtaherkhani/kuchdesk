package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestForwardEarlyExitReturnsSanitizedErrorPromptly(t *testing.T) {
	script := filepath.Join(t.TempDir(), "kubectl")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'lookup refused token=synthetic-private\\n' >&2\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	_, _, err := forwardAPI(ctx, config{Kubectl: script, APIPort: 5380})
	if err == nil || !strings.Contains(err.Error(), "lookup refused") || strings.Contains(err.Error(), "synthetic-private") {
		t.Fatalf("missing or unsafe child failure: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("child exit was hidden until the context/startup deadline")
	}
}

func TestForwardStartupBurstAndRepeatedStopAreBounded(t *testing.T) {
	script := filepath.Join(t.TempDir(), "kubectl")
	content := "#!/bin/sh\ni=0; while [ $i -lt 40 ]; do echo startup; i=$((i+1)); done\necho 'Forwarding from 127.0.0.1:49123 -> 5380'\nexec sleep 10\n"
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	port, stop, err := forwardAPI(ctx, config{Kubectl: script, APIPort: 5380})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	stop()
	stop()
	if port != "49123" || time.Since(start) > time.Second {
		t.Fatal("readiness was dropped or shutdown blocked")
	}
}
