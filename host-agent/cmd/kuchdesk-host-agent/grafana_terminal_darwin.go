package main

import "golang.org/x/sys/unix"

func grafanaTerminalState(fd int) (*unix.Termios, error) {
	return unix.IoctlGetTermios(fd, unix.TIOCGETA)
}
func grafanaSetTerminalState(fd int, state *unix.Termios) error {
	return unix.IoctlSetTermios(fd, unix.TIOCSETA, state)
}
func grafanaRestoreTerminalState(fd int, state *unix.Termios) error {
	return unix.IoctlSetTermios(fd, unix.TIOCSETAF, state)
}
