package pty

import (
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// open returns a new pseudo-terminal's master and the path of its slave.
func open() (*os.File, string, error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", err
	}
	f := os.NewFile(uintptr(fd), "/dev/ptmx")
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		f.Close()
		return nil, "", err
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		f.Close()
		return nil, "", err
	}
	return f, "/dev/pts/" + strconv.Itoa(n), nil
}
