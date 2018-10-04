package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/fatih/motion/astcontext"
	"github.com/zyedidia/tcell"
)

type pluginDef struct {
	Action string
	Func   func(*View, bool) bool
}

var plugins = []pluginDef{
	{"GoInstall", (*View).goInstall},
	{"GoDef", (*View).goDef},
	{"GoDecls", (*View).goDecls},
	{"GoComplete", (*View).goComplete},
	{"SelectNext", (*View).selectNext},
	{"OpenCur", (*View).openCur},
	{"WordComplete", (*View).wordComplete},
	{"FindInFiles", (*View).findInFiles},
	{"SetJumpMode", (*View).setJumpMode},
}

func addMyPlugins(m map[string]StrCommand) {
	for _, p := range plugins {
		f := p.Func
		commandActions[p.Action] = func(args []string) {
			log.Println("command:", p.Action)
			f(CurView(), false)
		}
		m[strings.ToLower(p.Action)] = StrCommand{p.Action, []Completion{NoCompletion}}
		bindingActions[p.Action] = f
	}
}

// grep plugin

type grepPlugin struct {
	v      *View // grep results view
	target *View // target view to insert completion
}

func parseGrepLine(s string) errorLine {
	el := errorLine{}

	cc := strings.Split(s, ":")
	if len(cc) < 3 {
		return el
	}

	el.fname = cc[0]
	el.line, _ = strconv.Atoi(cc[1])
	if len(cc) == 4 {
		el.pos, _ = strconv.Atoi(cc[2])
		el.message = strings.TrimSpace(cc[3])
	} else {
		el.message = strings.TrimSpace(cc[2])
	}

	return el
}

func (v *View) findInFiles(usePlugin bool) bool {
	log.Println("findInFiles command")
	sel := v.Cursor.GetSelection()
	if sel == "" {
		v.Cursor.SelectWord()
		sel = v.Cursor.GetSelection()
	}
	if sel == "" {
		return true
	}

	p := &grepPlugin{}
	if !strings.HasPrefix(v.Buf.Path, "Find: ") {
		v.Save(false)
	}

	cmd := exec.Command("grep", sel, "-m", "100", "--exclude-dir", "vendor", "-n", "-R", ".")
	buf, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("build:", err)
		messenger.Error(err.Error())
	}
	b := NewBufferFromString(strings.TrimSpace(string(buf)), "Find: "+sel)
	v.HSplit(b)
	p.v = CurView()
	p.v.Type.Readonly = true
	p.v.handler = func(e *tcell.EventKey) bool { return p.HandleEvent(e) }

	return true
}

func (g *grepPlugin) HandleEvent(e *tcell.EventKey) bool {
	log.Printf("e: %+v", e)

	switch e.Key() {
	case tcell.KeyEnter:
		c := g.v.Cursor
		line := g.v.Buf.Line(c.Y)
		el := parseGrepLine(line)
		g.v.gotoError(el)
		return true
	case tcell.KeyEsc, tcell.KeyCtrlSpace:
		g.v.Quit(false)
		return true
	default:
		return false
	}

	return true
}

// WordComplete plugin

type wordcompletePlugin struct {
	filter string
	v      *View // words list view
	target *View // target view to insert completion
	words  []string
}

func (g *wordcompletePlugin) HandleEvent(e *tcell.EventKey) bool {
	log.Printf("e: %+v", e)

	switch e.Key() {
	case tcell.KeyRune:
		if e.Modifiers()&tcell.ModAlt == tcell.ModAlt {
			g.v.Quit(false)
			return true
		}
		g.filter += string(e.Rune())
	case tcell.KeyDEL:
		if len(g.filter) > 0 {
			g.filter = g.filter[:len(g.filter)-1]
		}
	case tcell.KeyEnter:
		c := g.v.Cursor
		line := g.v.Buf.Line(c.Y)
		g.v.Quit(false)
		c = g.target.Cursor
		targetLine := g.target.Buf.Line(c.Y)
		prefix := getLeftChunk(targetLine, c.X)
		line = strings.TrimPrefix(line, prefix)
		g.target.Buf.Insert(Loc{c.X, c.Y}, line)
		return true
	case tcell.KeyEsc, tcell.KeyCtrlSpace:
		g.v.Quit(false)
		return true
	default:
		return false
	}

	messenger.Message("filter: ", g.filter)

	words := getFiltered(g.words, g.filter)
	b := NewBufferFromString(strings.Join(words, "\n"), "")
	g.v.OpenBuffer(b)
	g.v.Type.Readonly = true
	log.Println("filter:", g.filter, "words:", len(g.words), "filtered:", len(words))

	return true
}

func getWords(r io.Reader) []string {
	sc := bufio.NewScanner(r)
	sc.Split(bufio.ScanWords)
	var words, swords []string
	for sc.Scan() {
		cc := strings.FieldsFunc(sc.Text(), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
		for _, t := range cc {
			if len(t) > 3 {
				words = append(words, t)
			}
		}
	}
	sort.Strings(words)
	var prev string
	for _, w := range words {
		if w != prev {
			prev = w
			swords = append(swords, w)
		}
	}
	return swords
}

func getFiltered(words []string, prefix string) []string {
	var res []string
	for _, w := range words {
		if strings.HasPrefix(w, prefix) {
			res = append(res, w)
		}
	}
	return res
}

func (v *View) wordComplete(usePlugin bool) bool {
	buf := v.Buf
	if buf.FileType() != "go" {
		return false
	}

	c := v.Cursor
	line := v.Buf.Line(c.Y)

	g := &wordcompletePlugin{
		filter: getLeftChunk(line, c.X),
		target: v,
		words:  getWords(v.Buf.Buffer(false)),
	}

	words := getFiltered(g.words, g.filter)
	b := NewBufferFromString(strings.Join(words, "\n"), "")
	v.VSplit(b)
	g.v = CurView()
	g.v.Type.Readonly = true
	g.v.handler = func(e *tcell.EventKey) bool { return g.HandleEvent(e) }

	return true
}

// GoComplete plugin

type gocompletePlugin struct {
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

func (g *gocompletePlugin) HandleEvent(e *tcell.EventKey) bool {
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

	g := &gocompletePlugin{
		target: v,
	}

	for _, ln := range strings.Split(string(out), "\n") {
		s := strings.TrimSpace(ln)
		if s == "" {
			continue
		}
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

type fileopenerPlugin struct {
	v      *View
	filter string
	lsout  []byte
}

func (g *fileopenerPlugin) HandleEvent(e *tcell.EventKey) bool {
	log.Printf("e: %+v", e)

	switch e.Key() {
	case tcell.KeyEsc:
		g.filter = ""
		g.v.Quit(false)
		return true
	case tcell.KeyDEL:
		if len(g.filter) > 0 {
			g.filter = g.filter[:len(g.filter)-1]
		}
	case tcell.KeyRune:
		if e.Modifiers()&tcell.ModAlt == tcell.ModAlt && e.Rune() == 'o' {
			g.v.Quit(false)
			return true
		}
		g.filter += string(e.Rune())
	case tcell.KeyEnter:
		c := g.v.Cursor
		line := g.v.Buf.Line(c.Y)
		g.v.Quit(false)
		g.v.AddTab(false)
		CurView().Open(line)
		return true
	default:
		return false
	}

	lines := strings.Split(string(g.lsout), "\n")
	filtered := make([]string, 0, len(lines))
	messenger.Message("filter: ", g.filter, len(filtered))

	for _, ln := range lines {
		if g.filter != "" && !strings.HasPrefix(ln, g.filter) {
			continue
		}
		filtered = append(filtered, ln)
	}
	text := strings.Join(filtered, "\n")
	b := NewBufferFromString(text, "")
	g.v.OpenBuffer(b)
	g.v.Type.Readonly = true

	return true
}

func (v *View) openCur(usePlugin bool) bool {
	cmd := exec.Command("ls", "-F", "-1")
	lsout, err := cmd.CombinedOutput()
	if err != nil {
		messenger.Error("ls: " + err.Error())
		return false
	}

	b := NewBufferFromString(strings.TrimSpace(string(lsout)), "")
	v.VSplit(b)

	g := &fileopenerPlugin{
		v:     CurView(),
		lsout: lsout,
	}

	g.v.handler = func(e *tcell.EventKey) bool { return g.HandleEvent(e) }
	return true
}

func (v *View) goDef(usePlugin bool) bool {
	c := v.Cursor
	buf := v.Buf
	loc := Loc{c.X, c.Y}
	offset := ByteOffset(loc, buf)
	log.Printf("godef: %+v", loc)

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

var goinstallPlugin struct {
	lines      []errorLine
	cur        int
	hasNextErr bool
}

func (v *View) gotoError(ln errorLine) {
	errfile, _ := filepath.Abs(ln.fname)
	currfile, _ := filepath.Abs(v.Buf.Path)
	if currfile != errfile {
		found := false
		for i, t := range tabs {
			currfile, _ = filepath.Abs(t.Views[t.CurView].Buf.Path)
			if currfile == errfile {
				found = true
				log.Println("found:", currfile)
				curTab = i
				break
			}
		}
		if !found {
			v.AddTab(false)
			CurView().Open(errfile)
		}
	}
	loc := Loc{ln.pos - 1, ln.line - 1}
	CurView().Cursor.GotoLoc(loc)
	CurView().Cursor.Relocate()
	CurView().Relocate()
	messenger.Message(fmt.Sprintf("%s:%d:%d: %s", ln.fname, ln.line, ln.pos, ln.message))
}

func (v *View) goInstall(usePlugin bool) bool {
	log.Println("goInstall view command")
	p := &goinstallPlugin
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

type setmodePlugin struct {
	v *View
}

func (v *View) setJumpMode(usePlugin bool) bool {
	log.Println("setJumpMode command")
	p := &setmodePlugin{}
	p.v = CurView()
	p.v.Type.Readonly = true
	p.v.handler = func(e *tcell.EventKey) bool { return p.HandleEvent(e) }
	return true
}

func (g *setmodePlugin) HandleEvent(e *tcell.EventKey) bool {
	switch e.Key() {
	case tcell.KeyEnter:
		c := g.v.Cursor
		line := g.v.Buf.Line(c.Y)
		if line == "" {
			return true
		}
		el := parseGrepLine(line)
		if el.fname == "" {
			return true
		}
		g.v.gotoError(el)
		return true
	default:
		return false
	}
	return true
}

// godecls plugin

type godeclsPlugin struct {
	v      *View
	target *View
	filter string
	decls  []astcontext.Decl
}

func (v *View) goDecls(usePlugin bool) bool {
	log.Println("godecls command")
	p := &godeclsPlugin{
		target: v,
	}
	v.Save(false)
	cmd := exec.Command("motion", "-file", v.Buf.Path, "-mode", "decls", "-include", "func,type")
	buf, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("motion:", err)
		messenger.Error(err.Error())
		return true
	}
	var res astcontext.Result
	if err = json.Unmarshal(buf, &res); err != nil {
		log.Println("motion:", err)
		messenger.Error(err.Error())
		return true
	}

	var w bytes.Buffer
	for _, d := range res.Decls {
		fmt.Fprintf(&w, "%4d: %s\n", d.Line, d.Full)
	}
	p.decls = res.Decls

	b := NewBufferFromString(strings.TrimSuffix(w.String(), "\n"), "")
	v.HSplit(b)
	p.v = CurView()
	p.v.Type.Readonly = true
	p.v.handler = func(e *tcell.EventKey) bool { return p.HandleEvent(e) }

	return true
}

func (g *godeclsPlugin) HandleEvent(e *tcell.EventKey) bool {
	log.Printf("e: %+v", e)

	switch e.Key() {
	case tcell.KeyRune:
		if e.Modifiers()&tcell.ModAlt == tcell.ModAlt {
			g.v.Quit(false)
			return true
		}
		g.filter += string(e.Rune())
	case tcell.KeyDEL:
		if len(g.filter) > 0 {
			g.filter = g.filter[:len(g.filter)-1]
		}
	case tcell.KeyEnter:
		c := g.v.Cursor
		line := g.v.Buf.Line(c.Y)
		g.v.Quit(false)
		cc := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(cc) == 2 {
			ln, _ := strconv.Atoi(cc[0])
			el := errorLine{
				fname:   g.target.Buf.Path,
				line:    ln,
				message: cc[1],
			}
			log.Printf("goto: %+v, cc: %+v", el, cc)
			g.target.gotoError(el)
		}
		return true
	case tcell.KeyEsc, tcell.KeyCtrlSpace:
		g.v.Quit(false)
		return true
	default:
		return false
	}

	var w bytes.Buffer
	for _, d := range g.decls {
		if strings.Contains(strings.ToLower(d.Ident), g.filter) {
			fmt.Fprintf(&w, "%4d: %s\n", d.Line, d.Full)
		}
	}
	b := NewBufferFromString(strings.TrimSuffix(w.String(), "\n"), "")
	g.v.OpenBuffer(b)
	g.v.Type.Readonly = true

	return true
}
