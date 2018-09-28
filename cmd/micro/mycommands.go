package main

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zyedidia/tcell"
)

func addMyCommands(m map[string]StrCommand) {
	commandActions["GoInstall"] = goInstall
	commandActions["GoDef"] = goDef
	commandActions["SelectNext"] = selectNext
	commandActions["OpenCur"] = openCur

	m["goinstall"] = StrCommand{"GoInstall", []Completion{NoCompletion}}
	m["godef"] = StrCommand{"GoDef", []Completion{NoCompletion}}
	m["selectnext"] = StrCommand{"SelectNext", []Completion{NoCompletion}}
	m["opencur"] = StrCommand{"OpenCur", []Completion{NoCompletion}}
}

/*
func goDef(args []string) {
	CurView().goDef(false)
}

func (v *View) goDef(usePlugin bool) bool {
}
*/

func openCur(args []string) {
	CurView().openCur(false)
}

var findstr = ""
var lsout []byte
var filerOpened bool

func handleFilerEvent(v *View, e *tcell.EventKey) bool {
	log.Printf("e: %+v", e)

	switch e.Key() {
	case tcell.KeyEsc:
		findstr = ""
		v.Quit(false)
		return true
	case tcell.KeyDEL:
		if len(findstr) > 0 {
			findstr = findstr[:len(findstr)-1]
		}
	case tcell.KeyRune:
		if e.Modifiers()&tcell.ModAlt == tcell.ModAlt && e.Rune() == 'o' {
			v.Quit(false)
			return true
		}
		findstr += string(e.Rune())
	case tcell.KeyEnter:
		c := v.Cursor
		line := v.Buf.Line(c.Y)
		v.Quit(false)
		v.AddTab(false)
		CurView().Open(line)
		return true
	default:
		return false
	}

	lines := strings.Split(string(lsout), "\n")
	filtered := make([]string, 0, len(lines))
	messenger.Message("filter: ", findstr, len(filtered))

	for _, ln := range lines {
		if findstr != "" && !strings.HasPrefix(ln, findstr) {
			continue
		}
		filtered = append(filtered, ln)
	}
	text := strings.Join(filtered, "\n")
	b := NewBufferFromString(text, "")
	v.OpenBuffer(b)
	v.Type.Readonly = true
	filerOpened = true

	return true
}

func (v *View) openCur(usePlugin bool) bool {
	var err error
	cmd := exec.Command("ls", "-F", "-1")
	lsout, err = cmd.CombinedOutput()
	if err != nil {
		messenger.Error("ls: " + err.Error())
		return false
	}

	b := NewBufferFromString(strings.TrimSpace(string(lsout)), "")
	v.VSplit(b)
	nv := CurView()
	nv.handler = func(e *tcell.EventKey) bool { return handleFilerEvent(nv, e) }
	return true
}

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
		messenger.Message("go test -c")
	} else {
		cmd = exec.Command("go", "install")
		messenger.Message("go install")
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
