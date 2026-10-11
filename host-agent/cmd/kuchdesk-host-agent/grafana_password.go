package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const grafanaPasswordUsage = "Usage: kuchdesk-host-agent grafana-admin-password --kube-context CONTEXT\nReveal the Grafana bootstrap password from observability/grafana-admin in your local terminal.\nThis value may not authenticate after a Grafana database password change.\n"

func runGrafanaAdminPassword(ctx context.Context, args []string, help io.Writer, openTerminal func() (io.ReadWriteCloser, error), readPassword func(context.Context, string) ([]byte, error)) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := io.WriteString(help, grafanaPasswordUsage); err != nil {
			return errors.New("cannot write command help")
		}
		return nil
	}
	cluster, _, err := parseGrafanaPasswordArgs(args, false)
	if err != nil {
		return err
	}
	terminal, err := openTerminal()
	if err != nil {
		return errors.New("requires stdin, stdout and stderr on the same interactive terminal")
	}
	defer terminal.Close()
	ctx, stop := context.WithTimeout(ctx, 5*time.Minute)
	defer stop()
	if _, err := fmt.Fprintf(terminal, "Cluster: %s\nSource: observability/grafana-admin / GRAFANA_ADMIN_PASSWORD (bootstrap only).\nThis value may not authenticate after a Grafana database password change.\nIt will be visible in this terminal's scrollback.\nType REVEAL GRAFANA BOOTSTRAP PASSWORD to reveal: ", cluster); err != nil {
		return errors.New("cannot write terminal prompt")
	}
	if !grafanaConfirmed(ctx, terminal, "REVEAL GRAFANA BOOTSTRAP PASSWORD") {
		return errors.New("password reveal cancelled")
	}
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	password, err := readPassword(readCtx, cluster)
	defer clear(password)
	if err != nil {
		return errors.New("cannot read Grafana bootstrap password using existing Kubernetes access")
	}
	if !validGrafanaPassword(password) {
		return errors.New("bootstrap password is empty, oversized or unsafe for terminal display")
	}
	if _, err := terminal.Write(password); err != nil {
		return errors.New("cannot write password to terminal")
	}
	if _, err := io.WriteString(terminal, "\n"); err != nil {
		return errors.New("cannot write terminal newline")
	}
	return nil
}

func openGrafanaTerminal() (io.ReadWriteCloser, error) {
	var first os.FileInfo
	for _, f := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		if _, err := grafanaTerminalState(int(f.Fd())); err != nil {
			return nil, errors.New("interactive terminal required")
		}
		info, err := f.Stat()
		if err != nil {
			return nil, errors.New("interactive terminal required")
		}
		if first == nil {
			first = info
		} else if !os.SameFile(first, info) {
			return nil, errors.New("same terminal required")
		}
	}
	// Duplicate the verified stdin terminal itself, rather than opening a
	// potentially different controlling terminal through /dev/tty.
	fd, err := unix.Dup(int(os.Stdin.Fd()))
	if err != nil {
		return nil, errors.New("interactive terminal unavailable")
	}
	unix.CloseOnExec(fd)
	return os.NewFile(uintptr(fd), "Grafana interactive terminal"), nil
}

func readGrafanaBootstrapPassword(ctx context.Context, kubeContext string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := []string{"--context", kubeContext, "--request-timeout=10s", "-n", "observability"}
	var access grafanaBoundedOutput
	defer func() { clear(access.data) }()
	cmd := exec.CommandContext(ctx, "kubectl", append(args, "auth", "can-i", "get", "secret/grafana-admin")...)
	cmd.Stdout = &access
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil || string(bytes.TrimSpace(access.data)) != "yes" {
		return nil, errors.New("existing access does not permit reading Grafana bootstrap Secret")
	}
	var encoded grafanaBoundedOutput
	defer func() { clear(encoded.data) }()
	cmd = exec.CommandContext(ctx, "kubectl", append(args, "get", "secret", "grafana-admin", "-o", "jsonpath={.data.GRAFANA_ADMIN_PASSWORD}")...)
	cmd.Stdout = &encoded
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, errors.New("Grafana bootstrap Secret read failed")
	}
	password := make([]byte, base64.StdEncoding.DecodedLen(len(encoded.data)))
	n, err := base64.StdEncoding.Decode(password, bytes.TrimSpace(encoded.data))
	if err != nil || !validGrafanaPassword(password[:n]) {
		clear(password)
		return nil, errors.New("invalid Grafana bootstrap password")
	}
	return password[:n], nil
}

// Bound kubectl output before decoding, including a misbehaving executable.
type grafanaBoundedOutput struct{ data []byte }

func (w *grafanaBoundedOutput) Write(p []byte) (int, error) {
	if len(w.data)+len(p) > 8192 {
		return 0, errors.New("output exceeds limit")
	}
	w.data = append(w.data, p...)
	return len(p), nil
}

func parseGrafanaPasswordArgs(args []string, reset bool) (string, int, error) {
	f := flag.NewFlagSet("grafana-password", flag.ContinueOnError)
	f.SetOutput(io.Discard) // Never echo unknown arguments or their values.
	cluster := f.String("kube-context", "", "explicit Kubernetes context")
	id := 1
	if reset {
		f.IntVar(&id, "user-id", 1, "verified Grafana admin user ID")
	}
	if err := f.Parse(args); err != nil || f.NArg() != 0 {
		return "", 0, errors.New("invalid options; use --help")
	}
	if strings.TrimSpace(*cluster) == "" || len(*cluster) > 253 || strings.TrimSpace(*cluster) != *cluster || !validGrafanaPassword([]byte(*cluster)) {
		return "", 0, errors.New("a printable --kube-context is required")
	}
	if id <= 0 {
		return "", 0, errors.New("--user-id must be positive")
	}
	return *cluster, id, nil
}

func validGrafanaPassword(value []byte) bool {
	if len(value) == 0 || len(value) > 4096 || !utf8.Valid(value) {
		return false
	}
	for len(value) > 0 {
		r, size := utf8.DecodeRune(value)
		if !unicode.IsPrint(r) {
			return false
		}
		value = value[size:]
	}
	return true
}

func grafanaConfirmed(ctx context.Context, terminal io.Reader, phrase string) bool {
	if ctx.Err() != nil {
		return false
	}
	if f, ok := terminal.(*os.File); ok {
		terminal = grafanaTerminalInput{ctx: ctx, fd: int(f.Fd())}
	}
	line, err := bufio.NewReader(io.LimitReader(terminal, 128)).ReadString('\n')
	return err == nil && ctx.Err() == nil && strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") == phrase
}
