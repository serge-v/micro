// Copyright 2013 @atotto. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build freebsd linux netbsd openbsd solaris dragonfly

package clipboard

import (
	"errors"
	"os"
	"os/exec"
)

const (
	xsel               = "xsel"
	xclip              = "xclip"
	wlcopy             = "wl-copy"
	wlpaste            = "wl-paste"
	termuxClipboardGet = "termux-clipboard-get"
	termuxClipboardSet = "termux-clipboard-set"
)

var (
	pasteCmdArgs map[string][]string
	copyCmdArgs  map[string][]string

	xselPasteArgs = map[string][]string{
		"primary":   []string{xsel, "--output"},
		"clipboard": []string{xsel, "--output", "--clipboard"},
	}
	xselCopyArgs = map[string][]string{
		"primary":   []string{xsel, "--input"},
		"clipboard": []string{xsel, "--input", "--clipboard"},
	}

	xclipPasteArgs = map[string][]string{
		"primary":   []string{xclip, "-out"},
		"clipboard": []string{xclip, "-out", "-selection", "clipboard"},
	}
	xclipCopyArgs = map[string][]string{
		"primary":   []string{xclip, "-in"},
		"clipboard": []string{xclip, "-in", "-selection", "clipboard"},
	}

	wlpasteArgs = map[string][]string{
		"primary":   []string{wlpaste, "--no-newline", "--primary"},
		"clipboard": []string{wlpaste, "--no-newline"},
	}
	wlcopyArgs = map[string][]string{
		"primary":   []string{wlcopy, "--primary"},
		"clipboard": []string{wlcopy},
	}

	termuxPasteArgs = map[string][]string{
		"primary":   []string{termuxClipboardGet},
		"clipboard": []string{termuxClipboardGet},
	}
	termuxCopyArgs = map[string][]string{
		"primary":   []string{termuxClipboardSet},
		"clipboard": []string{termuxClipboardSet},
	}

	missingCommands = errors.New("No clipboard utilities available. Please install xsel, xclip, wl-clipboard or Termux:API add-on for termux-clipboard-get/set.")

	internalClipboards map[string]string
)

func init() {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		pasteCmdArgs = wlpasteArgs
		copyCmdArgs = wlcopyArgs

		if _, err := exec.LookPath(wlcopy); err == nil {
			if _, err := exec.LookPath(wlpaste); err == nil {
				return
			}
		}
	}

	pasteCmdArgs = xclipPasteArgs
	copyCmdArgs = xclipCopyArgs

	if _, err := exec.LookPath(xclip); err == nil {
		return
	}

	pasteCmdArgs = xselPasteArgs
	copyCmdArgs = xselCopyArgs

	if _, err := exec.LookPath(xsel); err == nil {
		return
	}

	pasteCmdArgs = termuxPasteArgs
	copyCmdArgs = termuxCopyArgs

	if _, err := exec.LookPath(termuxClipboardSet); err == nil {
		if _, err := exec.LookPath(termuxClipboardGet); err == nil {
			return
		}
	}

	internalClipboards = make(map[string]string)
	Unsupported = true
}

func getPasteCommand(register string) *exec.Cmd {
	return exec.Command(pasteCmdArgs[register][0], pasteCmdArgs[register][1:]...)
}

func getCopyCommand(register string) *exec.Cmd {
	return exec.Command(copyCmdArgs[register][0], copyCmdArgs[register][1:]...)
}

func readAll(register string) (string, error) {
	if Unsupported {
		if text, ok := internalClipboards[register]; ok {
			return text, nil
		}
		return "", nil
	}
	pasteCmd := getPasteCommand(register)
	out, err := pasteCmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func writeAll(text, register string) error {
	if Unsupported {
		internalClipboards[register] = text
		return nil
	}
	copyCmd := getCopyCommand(register)
	in, err := copyCmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := copyCmd.Start(); err != nil {
		return err
	}
	if _, err := in.Write([]byte(text)); err != nil {
		return err
	}
	if err := in.Close(); err != nil {
		return err
	}
	return copyCmd.Wait()
}
