package main

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/zyedidia/tcell"
)

func addMyCommands(m map[string]StrCommand) {
	commandActions["GoInstall"] = goInstall
	commandActions["GoDef"] = goDef
	commandActions["GoComplete"] = goComplete
	commandActions["SelectNext"] = selectNext
	commandActions["OpenCur"] = openCur

	m["goinstall"] = StrCommand{"GoInstall", []Completion{NoCompletion}}
	m["godef"] = StrCommand{"GoDef", []Completion{NoCompletion}}
	m["gocomplete"] = StrCommand{"GoComplete", []Completion{NoCompletion}}
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

type gocomplete struct {
	filter string
	v      *View // gocode view
	target *View // target view to insert completion
	lines  []string
}

func getLeftChunk(line string, pos int) string {
	line = line[:pos]
	cc := strings.FieldsFunc(line, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(cc) == 0 {
		return ""
	}
	last := cc[len(cc)-1]
	return last
}

func (g *gocomplete) HandleEvent(e *tcell.EventKey) bool {
	log.Printf("e: %+v", e)

	switch e.Key() {
	case tcell.KeyRune:
		g.filter += string(e.Rune())
	case tcell.KeyDEL:
		if len(g.filter) > 0 {
			g.filter = g.filter[:len(g.filter)-1]
		}
	case tcell.KeyEnter:
		c := g.v.Cursor
		line := g.v.Buf.Line(c.Y)
		g.v.Quit(false)
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return true
		}

		c = g.target.Cursor
		targetLine := g.target.Buf.Line(c.Y)
		prefix := getLeftChunk(targetLine, c.X)
		line = fields[1]
		line = strings.TrimPrefix(line, prefix)
		g.target.Buf.Insert(Loc{c.X, c.Y}, line)
		return true
	case tcell.KeyEsc, tcell.KeyCtrlSpace:
		g.v.Quit(false)
		return true
	default:
		return false
	}

	filtered := make([]string, 0, len(g.lines))
	messenger.Message("filter: ", g.filter)

	for _, ln := range g.lines {
		ls := strings.ToLower(ln)
		fields := strings.Fields(ls)
		if len(fields) > 1 {
			ls = strings.Join(fields[1:], " ")
		}
		if g.filter != "" && !strings.HasPrefix(ls, g.filter) {
			continue
		}
		filtered = append(filtered, ln)
	}
	log.Println("filter:", g.filter, "lines:", len(g.lines), "filtered:", len(filtered))

	text := strings.Join(filtered, "\n")
	b := NewBufferFromString(text, "")
	g.v.OpenBuffer(b)
	g.v.Type.Readonly = true

	return true
}

func goComplete(args []string) {
	CurView().goComplete(false)
}

func (v *View) goComplete(usePlugin bool) bool {
	buf := v.Buf
	if buf.FileType() != "go" {
		return false
	}
	v.Save(false)
	c := v.Cursor
	loc := Loc{c.X, c.Y}
	offset := ByteOffset(loc, buf)
	cmd := exec.Command("gocode", "-in", buf.Path, "autocomplete", strconv.Itoa(offset))
	out, err := cmd.CombinedOutput()
	if err != nil {
		messenger.Error("gocode: ", err.Error())
		return true
	}

	g := &gocomplete{
		target: v,
	}

	for _, ln := range strings.Split(string(out), "\n") {
		s := strings.TrimSpace(ln)
		if strings.HasPrefix(s, "Found ") {
			continue
		}
		g.lines = append(g.lines, s)
	}

	b := NewBufferFromString(strings.Join(g.lines, "\n"), "")
	v.VSplit(b)
	g.v = CurView()
	g.v.Type.Readonly = true
	g.v.handler = func(e *tcell.EventKey) bool { return g.HandleEvent(e) }

	return true
}

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

	v.Save(false)
	p.lines = nil

	var cmd *exec.Cmd
	if strings.HasSuffix(v.Buf.Path, "_test.go") {
		messenger.Message("go test -c")
		cmd = exec.Command("go", "test", "-c")
	} else {
		messenger.Message("go install")
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
