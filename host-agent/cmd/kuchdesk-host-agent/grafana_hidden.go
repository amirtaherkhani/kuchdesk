package main

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func readGrafanaHiddenPassword(ctx context.Context, terminal io.ReadWriteCloser) (password []byte, resultErr error) {
	f, ok := terminal.(*os.File)
	if !ok {
		return nil, errors.New("interactive terminal required")
	}
	fd := int(f.Fd())
	old, err := grafanaTerminalState(fd)
	if err != nil {
		return nil, errors.New("interactive terminal required")
	}
	hidden := *old
	hidden.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON
	hidden.Cc[unix.VMIN], hidden.Cc[unix.VTIME] = 1, 0
	if grafanaSetTerminalState(fd, &hidden) != nil {
		return nil, errors.New("hidden input unavailable")
	}
	// Flush pending paste on restore so it cannot appear at a later shell prompt.
	defer func() {
		if grafanaRestoreTerminalState(fd, old) != nil {
			clear(password)
			password = nil
			resultErr = errors.New("cannot restore terminal input")
		}
	}()
	password = make([]byte, 0, 4096)
	one := make([]byte, 1)
	defer clear(one)
	in := grafanaTerminalInput{ctx: ctx, fd: fd}
	for len(password) <= 4096 {
		if n, err := in.Read(one); n != 1 || err != nil {
			clear(password)
			return nil, errors.New("hidden input cancelled or unavailable")
		}
		switch one[0] {
		case '\r', '\n':
			return password, nil
		case 4: // Ctrl-D cancels.
			clear(password)
			return nil, errors.New("hidden input cancelled")
		case 8, 127:
			if len(password) > 0 {
				password[len(password)-1] = 0
				password = password[:len(password)-1]
			}
		default:
			if len(password) == 4096 {
				clear(password)
				return nil, errors.New("hidden input too long")
			}
			password = append(password, one[0])
		}
	}
	clear(password)
	return nil, errors.New("hidden input too long")
}

// Poll synchronously: cancellation leaves no goroutine reading future input.
type grafanaTerminalInput struct {
	ctx context.Context
	fd  int
}

func (r grafanaTerminalInput) Read(p []byte) (int, error) {
	for r.ctx.Err() == nil {
		if r.fd < 0 || r.fd >= unix.FD_SETSIZE {
			return 0, errors.New("terminal unavailable")
		}
		var fds unix.FdSet
		fds.Set(r.fd)
		timeout := unix.NsecToTimeval((100 * time.Millisecond).Nanoseconds())
		n, err := unix.Select(r.fd+1, &fds, nil, nil, &timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, errors.New("terminal unavailable")
		}
		if n > 0 {
			return unix.Read(r.fd, p)
		}
	}
	return 0, errors.New("terminal input cancelled")
}
