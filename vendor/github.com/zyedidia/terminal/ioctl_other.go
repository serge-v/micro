// +build plan9 nacl windows

package terminal

import (
	"os"
)

func ioctl(f *os.File, cmd, p uintptr) error {
	return nil
}

func (t *VT) ptyResize() error {
	return nil
}
