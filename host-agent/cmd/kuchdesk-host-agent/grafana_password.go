package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/amirtaherkhani/kuchdesk/configyaml"
	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/localruntime"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

const grafanaPasswordUsage = "Usage: kuchdesk-host-agent grafana-admin-password --values-root PROJECT_ROOT\nReveal the Grafana bootstrap password from private local chart YAML in your terminal.\nThis value may not authenticate after a Grafana database password change.\n"

func runGrafanaAdminPassword(ctx context.Context, args []string, help io.Writer, openTerminal func() (io.ReadWriteCloser, error), readPassword func(context.Context, string) ([]byte, error)) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := io.WriteString(help, grafanaPasswordUsage); err != nil {
			return errors.New("cannot write command help")
		}
		return nil
	}
	root, err := parseGrafanaValuesArgs(args)
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
	if _, err := fmt.Fprintf(terminal, "Source: private Grafana chart YAML (bootstrap only).\nThis value may not authenticate after a Grafana database password change.\nIt will be visible in this terminal's scrollback.\nType REVEAL GRAFANA BOOTSTRAP PASSWORD to reveal: "); err != nil {
		return errors.New("cannot write terminal prompt")
	}
	if !grafanaConfirmed(ctx, terminal, "REVEAL GRAFANA BOOTSTRAP PASSWORD") {
		return errors.New("password reveal cancelled")
	}
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	password, err := readPassword(readCtx, root)
	defer clear(password)
	if err != nil {
		return errors.New("cannot read Grafana bootstrap password from private local YAML")
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

func readGrafanaBootstrapPassword(ctx context.Context, root string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.New("Grafana bootstrap read cancelled")
	}
	data, _, err := chartvalues.Read(root, "grafana", "values.local.yaml")
	if err != nil {
		return nil, errors.New("Grafana private local YAML unavailable")
	}
	defer clear(data)
	var values struct {
		AdminPassword string `yaml:"adminPassword"`
	}
	if err := yaml.Unmarshal(data, &values); err != nil || !validGrafanaPassword([]byte(values.AdminPassword)) {
		return nil, errors.New("Grafana bootstrap password missing or invalid")
	}
	return []byte(values.AdminPassword), nil
}

func parseGrafanaValuesArgs(args []string) (string, error) {
	f := flag.NewFlagSet("grafana-password", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	root := f.String("values-root", "", "absolute KuchDesk source root")
	if err := f.Parse(args); err != nil || f.NArg() != 0 || !filepath.IsAbs(*root) {
		return "", errors.New("an absolute --values-root is required")
	}
	return *root, nil
}

func parseGrafanaPasswordArgs(args []string) (string, int, error) {
	f := flag.NewFlagSet("grafana-password", flag.ContinueOnError)
	f.SetOutput(io.Discard) // Never echo unknown arguments or their values.
	cluster := f.String("kube-context", "", "explicit Kubernetes context")
	id := 1
	f.IntVar(&id, "user-id", 1, "verified Grafana admin user ID")
	if err := f.Parse(args); err != nil || f.NArg() != 0 {
		return "", 0, errors.New("invalid options; use --help")
	}
	if strings.TrimSpace(*cluster) == "" || len(*cluster) > 253 || strings.TrimSpace(*cluster) != *cluster || !validGrafanaPassword([]byte(*cluster)) {
		return "", 0, errors.New("a printable --kube-context is required")
	}
	if _, err := localruntime.Profile(*cluster); err != nil {
		return "", 0, err
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
