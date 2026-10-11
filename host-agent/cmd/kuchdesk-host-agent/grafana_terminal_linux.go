package main

import "golang.org/x/sys/unix"

func grafanaTerminalState(fd int) (*unix.Termios, error) {
	return unix.IoctlGetTermios(fd, unix.TCGETS)
}
func grafanaSetTerminalState(fd int, state *unix.Termios) error {
	return unix.IoctlSetTermios(fd, unix.TCSETS, state)
}
func grafanaRestoreTerminalState(fd int, state *unix.Termios) error {
	return unix.IoctlSetTermios(fd, unix.TCSETSF, state)
}
