package pty

import (
	"errors"
	"os"
)

func open() (pty, tty *os.File, err error) {
	return nil, nil, errors.New("Unsupported")
}
