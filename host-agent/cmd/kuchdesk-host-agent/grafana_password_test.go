package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the actual CLI dispatch without starting DNS reconciliation.
func TestGrafanaCLIProcess(t *testing.T) {
	if os.Getenv("KUCHDESK_TEST_GRAFANA_CLI") != "1" {
		return
	}
	os.Args = append([]string{"kuchdesk-host-agent"}, strings.Split(os.Getenv("KUCHDESK_TEST_GRAFANA_ARGS"), "\n")...)
	main()
	os.Exit(0)
}

func TestGrafanaCommandHelpDoesNotRequireClusterOrTerminal(t *testing.T) {
	for _, command := range []string{"grafana-admin-password", "grafana-admin-password-reset"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGrafanaCLIProcess$")
		cmd.Env = append(os.Environ(), "KUCHDESK_TEST_GRAFANA_CLI=1", "KUCHDESK_TEST_GRAFANA_ARGS="+command+"\n--help")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("help failed: %v: %s", err, output)
		}
		option := "--values-root"
		if command == "grafana-admin-password-reset" {
			option = "--kube-context"
		}
		if !strings.Contains(string(output), "bootstrap") || !strings.Contains(string(output), option) {
			t.Fatalf("help does not explain bootstrap source and explicit cluster: %s", output)
		}
	}
}

type grafanaTestTerminal struct {
	*strings.Reader
	output bytes.Buffer
	closed bool
}

func (t *grafanaTestTerminal) Write(p []byte) (int, error) { return t.output.Write(p) }
func (t *grafanaTestTerminal) Close() error                { t.closed = true; return nil }

func TestGrafanaRevealRequiresTypedConsentAndClearsPassword(t *testing.T) {
	terminal := &grafanaTestTerminal{Reader: strings.NewReader("REVEAL GRAFANA BOOTSTRAP PASSWORD\n")}
	password := []byte("Dummy-password-only!")
	err := runGrafanaAdminPassword(context.Background(), []string{"--values-root", "/dummy-root"}, io.Discard,
		func() (io.ReadWriteCloser, error) { return terminal, nil },
		func(ctx context.Context, root string) ([]byte, error) {
			if !strings.Contains(terminal.output.String(), "may not authenticate") {
				t.Fatal("missing bootstrap warning before read")
			}
			if root != "/dummy-root" {
				t.Fatal("wrong source root")
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("unbounded password fetch")
			}
			return password, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(terminal.output.String(), "Dummy-password-only!") || !terminal.closed {
		t.Fatal("missing terminal reveal or close")
	}
	if !bytes.Equal(password, make([]byte, len(password))) {
		t.Fatal("password buffer retained")
	}
}

func TestGrafanaRevealCancelsBeforeLocalValuesRead(t *testing.T) {
	for _, input := range []string{"", "cancel\n", "REVEAL GRAFANA BOOTSTRAP PASSWORD", strings.Repeat("x", 300) + "\n"} {
		terminal := &grafanaTestTerminal{Reader: strings.NewReader(input)}
		err := runGrafanaAdminPassword(context.Background(), []string{"--values-root", "/dummy-root"}, io.Discard,
			func() (io.ReadWriteCloser, error) { return terminal, nil },
			func(context.Context, string) ([]byte, error) {
				t.Fatal("cancelled reveal read private values")
				return nil, nil
			})
		if err == nil || !terminal.closed {
			t.Fatal("cancellation did not close terminal")
		}
	}
}

func TestGrafanaRevealRejectsInvalidArgsBeforeTerminalAccess(t *testing.T) {
	for _, args := range [][]string{nil, {"--values-root", " "}, {"--values-root", "bad\nroot"}, {"--password", "Dummy-password-only!"}, {"--values-root", "/dummy-root", "Dummy-password-only!"}, {"--secret", "arbitrary"}} {
		err := runGrafanaAdminPassword(context.Background(), args, io.Discard,
			func() (io.ReadWriteCloser, error) { t.Fatal("invalid args opened terminal"); return nil, nil },
			func(context.Context, string) ([]byte, error) { t.Fatal("invalid args fetched secret"); return nil, nil })
		if err == nil || strings.Contains(err.Error(), "Dummy-password-only!") {
			t.Fatal("invalid args accepted or echoed")
		}
	}
}

func TestGrafanaRevealSanitizesDependencyFailures(t *testing.T) {
	for _, failOpen := range []bool{true, false} {
		terminal := &grafanaTestTerminal{Reader: strings.NewReader("REVEAL GRAFANA BOOTSTRAP PASSWORD\n")}
		err := runGrafanaAdminPassword(context.Background(), []string{"--values-root", "/dummy-root"}, io.Discard,
			func() (io.ReadWriteCloser, error) {
				if failOpen {
					return nil, errors.New("Dummy-password-only!")
				}
				return terminal, nil
			},
			func(context.Context, string) ([]byte, error) {
				if failOpen {
					t.Fatal("noninteractive read")
				}
				return nil, errors.New("Dummy-password-only!")
			})
		if err == nil || strings.Contains(err.Error(), "Dummy-password-only!") || strings.Contains(terminal.output.String(), "Dummy-password-only!") {
			t.Fatal("dependency error leaked")
		}
	}
}

func TestGrafanaCLIRefusesPipesBeforeCredentialAccess(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "called")
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"grafana-admin-password", "grafana-admin-password-reset"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGrafanaCLIProcess$")
		args := command + "\n--kube-context\ncolima-dummy"
		if command == "grafana-admin-password" {
			args = command + "\n--values-root\n/dummy-root"
		}
		cmd.Env = append(os.Environ(), "PATH="+dir, "KUCHDESK_TEST_GRAFANA_CLI=1", "KUCHDESK_TEST_GRAFANA_ARGS="+args)
		output, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "terminal") {
			t.Fatalf("noninteractive invocation: %v %s", err, output)
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("noninteractive command invoked kubectl")
		}
	}
}

func TestGrafanaBootstrapReadUsesPrivateLocalYAML(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lab/apps/observability/grafana/values.local.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("adminPassword: Dummy-password-only!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	password, err := readGrafanaBootstrapPassword(context.Background(), root)
	if err != nil || string(password) != "Dummy-password-only!" {
		t.Fatal("private YAML bootstrap read failed")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	password, err = readGrafanaBootstrapPassword(context.Background(), root)
	if err == nil || len(password) != 0 {
		t.Fatal("insecure private YAML was read")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("adminPassword: bad\x1b[31m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	password, err = readGrafanaBootstrapPassword(context.Background(), root)
	if err == nil || len(password) != 0 {
		t.Fatal("unsafe bootstrap value was read")
	}
}
