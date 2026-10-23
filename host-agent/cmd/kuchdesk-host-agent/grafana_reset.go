package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"
)

const grafanaResetUsage = "Usage: kuchdesk-host-agent grafana-admin-password-reset --kube-context CONTEXT [--user-id ID]\nReset the Grafana database admin password using hidden terminal input and explicit final consent.\nUses existing Kubernetes exec access; default admin ID 1 must be verified by the user.\nExisting bootstrap credentials may stop authenticating.\n"

func runGrafanaAdminPasswordReset(ctx context.Context, args []string, help io.Writer, openTerminal func() (io.ReadWriteCloser, error), readHidden func(context.Context, io.ReadWriteCloser) ([]byte, error), reset func(context.Context, string, int, []byte) error) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := io.WriteString(help, grafanaResetUsage); err != nil {
			return errors.New("cannot write command help")
		}
		return nil
	}
	cluster, id, err := parseGrafanaPasswordArgs(args)
	if err != nil {
		return err
	}
	terminal, err := openTerminal()
	if err != nil {
		return errors.New("requires stdin, stdout and stderr on the same interactive terminal")
	}
	defer terminal.Close()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if _, err := fmt.Fprintf(terminal, "Cluster: %s / observability / deployment/grafana\nGrafana admin user ID: %d (verify this account ID before proceeding).\nChanges the Grafana database only. Existing bootstrap credentials may stop authenticating.\nNew Grafana admin password (hidden): ", cluster, id); err != nil {
		return errors.New("cannot write terminal prompt")
	}
	first, err := readHidden(ctx, terminal)
	defer clear(first)
	if err != nil {
		return errors.New("hidden password entry cancelled or unavailable")
	}
	if _, err := io.WriteString(terminal, "\nConfirm new password (hidden): "); err != nil {
		return errors.New("cannot write terminal prompt")
	}
	second, err := readHidden(ctx, terminal)
	defer clear(second)
	if err != nil {
		return errors.New("hidden password confirmation cancelled or unavailable")
	}
	if !validGrafanaPassword(first) || !bytes.Equal(bytes.TrimSpace(first), first) || !bytes.Equal(first, second) {
		return errors.New("password is empty, oversized, unsafe, has surrounding whitespace or does not match; nothing submitted")
	}
	clear(second)
	if _, err := io.WriteString(terminal, "\nType RESET GRAFANA ADMIN PASSWORD to submit: "); err != nil {
		return errors.New("cannot write terminal prompt")
	}
	if !grafanaConfirmed(ctx, terminal, "RESET GRAFANA ADMIN PASSWORD") {
		return errors.New("password reset cancelled; nothing submitted")
	}
	resetCtx, stop := context.WithTimeout(ctx, 30*time.Second)
	defer stop()
	if err := reset(resetCtx, cluster, id, first); err != nil {
		return errors.New("Grafana reset success is unconfirmed; check login yourself before retrying and verify existing Kubernetes exec access/admin user ID")
	}
	if _, err := io.WriteString(terminal, "Grafana database password reset succeeded. Sign in yourself with the new password.\n"); err != nil {
		return errors.New("cannot write reset result; password submission already completed")
	}
	return nil
}

func resetGrafanaAdminPassword(ctx context.Context, cluster string, userID int, password []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// No password in argv/environment, remote bridge, output, logs or errors.
	input := make([]byte, len(password)+1)
	copy(input, password)
	input[len(password)] = '\n'
	defer clear(input)
	cmd := exec.CommandContext(ctx, "kubectl", "--context", cluster, "--request-timeout=30s", "-n", "observability", "exec", "-i", "deployment/grafana", "-c", "grafana", "--", "grafana", "cli", "--homepath", "/usr/share/grafana", "--config", "/etc/grafana/grafana.ini", "--configOverrides", "cfg:default.paths.data=/var/lib/grafana", "admin", "reset-admin-password", "--password-from-stdin", "--user-id", strconv.Itoa(userID))
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return errors.New("Grafana password reset command failed")
	}
	return nil
}
