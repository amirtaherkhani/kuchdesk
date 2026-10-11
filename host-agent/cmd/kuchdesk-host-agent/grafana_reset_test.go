package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrafanaResetRequiresHiddenMatchingEntryAndFinalConsent(t *testing.T) {
	for _, mode := range []string{"success", "mismatch", "cancel", "input-error", "reset-error", "invalid", "surrounding-space"} {
		t.Run(mode, func(t *testing.T) {
			consent := "RESET GRAFANA ADMIN PASSWORD\n"
			if mode == "cancel" {
				consent = "cancel\n"
			}
			terminal := &grafanaTestTerminal{Reader: strings.NewReader(consent)}
			first, second := []byte("Dummy-password-only!"), []byte("Dummy-password-only!")
			if mode == "mismatch" {
				second = []byte("different")
			}
			if mode == "invalid" {
				first, second = []byte("bad\n"), []byte("bad\n")
			}
			if mode == "surrounding-space" {
				first, second = []byte(" Dummy-password-only! "), []byte(" Dummy-password-only! ")
			}
			reads, submits := 0, 0
			err := runGrafanaAdminPasswordReset(context.Background(), []string{"--kube-context", "dummy-context", "--user-id", "42"}, io.Discard,
				func() (io.ReadWriteCloser, error) { return terminal, nil },
				func(ctx context.Context, terminal io.ReadWriteCloser) ([]byte, error) {
					reads++
					if mode == "input-error" {
						return first, errors.New("Dummy-password-only!")
					}
					if reads == 1 {
						return first, nil
					}
					return second, nil
				},
				func(ctx context.Context, cluster string, id int, password []byte) error {
					submits++
					if cluster != "dummy-context" || id != 42 || string(password) != "Dummy-password-only!" {
						t.Fatal("wrong reset target or input")
					}
					if _, ok := ctx.Deadline(); !ok {
						t.Fatal("unbounded reset")
					}
					if mode == "reset-error" {
						return errors.New("Dummy-password-only!")
					}
					return nil
				})
			if mode == "success" {
				if err != nil || submits != 1 {
					t.Fatal("confirmed reset failed")
				}
			} else {
				if err == nil || strings.Contains(err.Error(), "Dummy-password-only!") {
					t.Fatal("failure accepted or credential echoed")
				}
				if mode != "reset-error" && submits != 0 {
					t.Fatal("unconfirmed/invalid password submitted")
				}
			}
			if !terminal.closed || strings.Contains(terminal.output.String(), "Dummy-password-only!") {
				t.Fatal("terminal left open or password displayed")
			}
			if !bytes.Equal(first, make([]byte, len(first))) {
				t.Fatal("first password retained")
			}
			if reads >= 2 && !bytes.Equal(second, make([]byte, len(second))) {
				t.Fatal("confirmation password retained")
			}
			if !strings.Contains(terminal.output.String(), "Infisical") {
				t.Fatal("missing independent-bootstrap warning")
			}
		})
	}
}

func TestGrafanaResetRejectsPasswordArgumentsAndInvalidUserID(t *testing.T) {
	for _, args := range [][]string{{"--kube-context", "dummy", "--password", "Dummy-password-only!"}, {"--kube-context", "dummy", "--user-id", "0"}, {"--kube-context", "dummy", "--user-id", "-3"}, {"--kube-context", "dummy", "Dummy-password-only!"}} {
		err := runGrafanaAdminPasswordReset(context.Background(), args, io.Discard,
			func() (io.ReadWriteCloser, error) { t.Fatal("invalid options opened terminal"); return nil, nil }, nil, nil)
		if err == nil || strings.Contains(err.Error(), "Dummy-password-only!") {
			t.Fatal("invalid reset arguments accepted or echoed")
		}
	}
}

func TestGrafanaResetSendsPasswordOnlyOnStdinAndSanitizesOutput(t *testing.T) {
	for _, failure := range []bool{false, true} {
		dir := t.TempDir()
		capture := filepath.Join(dir, "args")
		input := filepath.Join(dir, "stdin")
		script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > '" + capture + "'\n/bin/cat > '" + input + "'\nprintf 'Dummy-password-only!'\nprintf 'Dummy-password-only!' >&2\n"
		if failure {
			script += "exit 1\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)
		err := resetGrafanaAdminPassword(context.Background(), "dummy-context", 42, []byte("Dummy-password-only!"))
		if failure {
			if err == nil || strings.Contains(err.Error(), "Dummy-password-only!") {
				t.Fatal("reset failure leaked")
			}
		} else if err != nil {
			t.Fatal(err)
		}
		args, e := os.ReadFile(capture)
		if e != nil {
			t.Fatal(e)
		}
		want := "--context dummy-context --request-timeout=30s -n observability exec -i deployment/grafana -c grafana -- grafana cli --homepath /usr/share/grafana --config /etc/grafana/grafana.ini --configOverrides cfg:default.paths.data=/var/lib/grafana admin reset-admin-password --password-from-stdin --user-id 42\n"
		if string(args) != want {
			t.Fatal("password in argv or reset target changed")
		}
		data, e := os.ReadFile(input)
		if e != nil || string(data) != "Dummy-password-only!\n" {
			t.Fatal("password was not supplied on stdin")
		}
	}
}
