package main

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func addMyCommands(m map[string]StrCommand) {
	commandActions["GoInstall"] = goInstall
	commandActions["GoDef"] = goDef
	commandActions["SelectNext"] = selectNext

	m["goinstall"] = StrCommand{"GoInstall", []Completion{NoCompletion}}
	m["godef"] = StrCommand{"GoDef", []Completion{NoCompletion}}
	m["selectnext"] = StrCommand{"SelectNext", []Completion{NoCompletion}}
}

/*
func goDef(args []string) {
	CurView().goDef(false)
}

func (v *View) goDef(usePlugin bool) bool {
}
*/

func goDef(args []string) {
	CurView().goDef(false)
}

func (v *View) goDef(usePlugin bool) bool {
	c := v.Cursor
	buf := v.Buf
	loc := Loc{c.X, c.Y}
	offset := ByteOffset(loc, buf)

	cmd := exec.Command("godef", "-f", buf.Path, "-o", strconv.Itoa(offset))
	out, err := cmd.CombinedOutput()
	if err != nil {
		messenger.Error("godef: " + err.Error())
		return false
	}
	lines := strings.Split(string(out), "\n")
	for _, ln := range lines {
		cc := strings.Split(ln, ":")
		if len(cc) < 3 {
			continue
		}
		log.Printf("godef: %+v", cc)
		line, _ := strconv.Atoi(cc[1])
		pos, _ := strconv.Atoi(cc[2])
		el := errorLine{
			fname: cc[0],
			line:  line,
			pos:   pos,
		}
		v.gotoError(el)
		return true
	}
	return true
}

func selectNext(args []string) {
	CurView().selectNext(false)
}

func (v *View) selectNext(usePlugin bool) bool {
	c := v.Cursor
	w := c.GetSelection()
	if w != "" {
		searchStart = c.CurSelection[1]
		Search(w, v, true)
		return true
	}
	c.SelectWord()

	return true
}

type errorLine struct {
	fname   string
	line    int
	pos     int
	message string
}

var myplugin struct {
	lines      []errorLine
	cur        int
	hasNextErr bool
}

func goInstall(args []string) {
	log.Println("goInstall command")
	CurView().goInstall(false)
}

func (v *View) gotoError(ln errorLine) {
	errfile, _ := filepath.Abs(ln.fname)
	currfile, _ := filepath.Abs(v.Buf.Path)
	if currfile != errfile {
		v.AddTab(false)
		CurView().Open(errfile)
	}
	loc := Loc{ln.pos - 1, ln.line - 1}
	CurView().Cursor.GotoLoc(loc)
	CurView().Cursor.Relocate()
	CurView().Relocate()
	messenger.Message(fmt.Sprintf("%s:%d:%d: %s", ln.fname, ln.line, ln.pos, ln.message))
}

func (v *View) goInstall(usePlugin bool) bool {
	log.Println("goInstall view command")
	p := &myplugin
	if p.hasNextErr {
		p.cur++
		if len(p.lines) >= p.cur+1 {
			messenger.Message("no more errors")
			p.hasNextErr = false
			p.cur = 0
			p.lines = nil
			return true
		}
		ln := p.lines[p.cur]
		v.gotoError(ln)
		return true
	}

	p.lines = nil

	var cmd *exec.Cmd
	if strings.HasSuffix(v.Buf.Path, "_test.go") {
		cmd = exec.Command("go", "test", "-c")
	} else {
		cmd = exec.Command("go", "install")
	}
	buf, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("build:", err)
		messenger.Error(err.Error())
	}
	lines := strings.Split(string(buf), "\n")
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, "#") {
			continue
		}
		cc := strings.SplitN(ln, ":", 4)
		if len(cc) != 4 {
			continue
		}
		file := cc[0]
		line, _ := strconv.Atoi(cc[1])
		pos, _ := strconv.Atoi(cc[2])
		msg := strings.TrimSpace(cc[3])

		el := errorLine{
			fname:   file,
			line:    line,
			pos:     pos,
			message: msg,
		}
		p.lines = append(p.lines, el)
	}
	p.cur = 0
	p.hasNextErr = false
	if len(p.lines) > 0 {
		ln := p.lines[0]
		v.gotoError(ln)
		p.hasNextErr = len(p.lines) > 1
	} else {
		messenger.Message("no errors")
	}
	log.Println("err lines:", len(p.lines))
	return true
}
