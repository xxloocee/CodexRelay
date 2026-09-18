//go:build !windows

package clientconfig

import (
	"golang.org/x/sys/unix"
	"os"
)

func lockHistoryFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
