package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
		if !strings.Contains(string(output), "bootstrap") || !strings.Contains(string(output), "--kube-context") {
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
	err := runGrafanaAdminPassword(context.Background(), []string{"--kube-context", "dummy-context"}, io.Discard,
		func() (io.ReadWriteCloser, error) { return terminal, nil },
		func(ctx context.Context, cluster string) ([]byte, error) {
			if !strings.Contains(terminal.output.String(), "may not authenticate") {
				t.Fatal("missing bootstrap warning before read")
			}
			if cluster != "dummy-context" {
				t.Fatal("wrong cluster")
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

func TestGrafanaRevealCancelsBeforeSecretRead(t *testing.T) {
	for _, input := range []string{"", "cancel\n", "REVEAL GRAFANA BOOTSTRAP PASSWORD", strings.Repeat("x", 300) + "\n"} {
		terminal := &grafanaTestTerminal{Reader: strings.NewReader(input)}
		err := runGrafanaAdminPassword(context.Background(), []string{"--kube-context", "dummy"}, io.Discard,
			func() (io.ReadWriteCloser, error) { return terminal, nil },
			func(context.Context, string) ([]byte, error) {
				t.Fatal("cancelled reveal fetched secret")
				return nil, nil
			})
		if err == nil || !terminal.closed {
			t.Fatal("cancellation did not close terminal")
		}
	}
}

func TestGrafanaRevealRejectsInvalidArgsBeforeTerminalAccess(t *testing.T) {
	for _, args := range [][]string{nil, {"--kube-context", " "}, {"--kube-context", "bad\ncontext"}, {"--password", "Dummy-password-only!"}, {"--kube-context", "dummy", "Dummy-password-only!"}, {"--secret", "arbitrary"}} {
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
		err := runGrafanaAdminPassword(context.Background(), []string{"--kube-context", "dummy"}, io.Discard,
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

func TestGrafanaCLIRefusesPipesBeforeKubectl(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "called")
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"grafana-admin-password", "grafana-admin-password-reset"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGrafanaCLIProcess$")
		cmd.Env = append(os.Environ(), "PATH="+dir, "KUCHDESK_TEST_GRAFANA_CLI=1", "KUCHDESK_TEST_GRAFANA_ARGS="+command+"\n--kube-context\ndummy")
		output, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "terminal") {
			t.Fatalf("noninteractive invocation: %v %s", err, output)
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("noninteractive command invoked kubectl")
		}
	}
}

func TestGrafanaBootstrapReadUsesFixedSecretAndExistingAuthorization(t *testing.T) {
	for _, mode := range []string{"allow", "deny", "get-error", "invalid", "empty", "control", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			capture := filepath.Join(dir, "calls")
			encoded := base64.StdEncoding.EncodeToString([]byte("Dummy-password-only!"))
			if mode == "invalid" {
				encoded = "not-base64!"
			}
			if mode == "empty" {
				encoded = ""
			}
			if mode == "control" {
				encoded = base64.StdEncoding.EncodeToString([]byte("bad\x1b[31m"))
			}
			if mode == "oversize" {
				encoded = strings.Repeat("A", 9000)
			}
			script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + capture + "'\ncase \"$*\" in\n*'auth can-i'*) "
			if mode == "deny" {
				script += "printf 'no\\nDummy-password-only!' >&2; exit 1"
			} else {
				script += "printf 'yes\\n'"
			}
			script += ";;\n*) "
			if mode == "get-error" {
				script += "printf 'Dummy-password-only!' >&2; exit 1"
			} else {
				script += "printf '%s' '" + encoded + "'"
			}
			script += ";;\nesac\n"
			if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			password, err := readGrafanaBootstrapPassword(context.Background(), "dummy-context")
			calls, readErr := os.ReadFile(capture)
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := []string{"--context dummy-context --request-timeout=10s -n observability auth can-i get secret/grafana-admin"}
			if mode != "deny" {
				want = append(want, "--context dummy-context --request-timeout=10s -n observability get secret grafana-admin -o jsonpath={.data.GRAFANA_ADMIN_PASSWORD}")
			}
			if !reflect.DeepEqual(strings.Split(strings.TrimSpace(string(calls)), "\n"), want) {
				t.Fatal("wrong resource or unauthorized fetch")
			}
			if mode == "allow" {
				if err != nil || string(password) != "Dummy-password-only!" {
					t.Fatal("bootstrap read failed")
				}
			} else if err == nil || len(password) != 0 || strings.Contains(err.Error(), "Dummy-password-only!") || strings.Contains(err.Error(), encoded) && encoded != "" {
				t.Fatal("bad or unauthorized secret returned, or leaked error")
			}
		})
	}
}
