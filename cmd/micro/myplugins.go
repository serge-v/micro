package main

// myplugings is a set of experimental addons for micro editor for golang development.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
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
	Action string                     // micro action name
	Key    string                     // key binding
	Func   func(*View, []string) bool // binding handler
}

var myPlugins = []pluginDef{
	// Update golang external tools.
	{"GoUpdate", "", (*View).goUpdateBinaries},

	// Run go install and cycle thru the errors.
	{"GoInstall", "Alti", (*View).goInstall},

	// Go to symbol definition.
	{"GoDef", "Alt]", (*View).goDef},

	// List go definitions for the current file in the split view.
	{"GoDecls", "Altt", (*View).goDecls},

	// Show go completions in the split view.
	{"GoComplete", "CtrlSpace", (*View).goComplete},

	// Find next occurence of word under cursor.
	{"SelectNext", "Altl", (*View).selectNext},

	// Show file list from the current directory in the split view.
	{"OpenCur", "Alto", (*View).openCur},

	// Show word completions for the current file in the split view.
	{"WordComplete", "Alt'", (*View).wordComplete},

	// Find word under cursor in all files. Show results in the split view.
	{"FindInFiles", "", (*View).findInFiles},

	// Set buffer mode to jump to the file on the enter key.
	{"SetJumpMode", "", (*View).setJumpMode},

	// Exec command under the cursor an open jump view.
	{"ExecCommand", "", (*View).execCommand},

	// TextFilter command passes text selection into the filter like 'textfilter sort'.
	{"TextFilter", "", (*View).textFilter},
}

func addMyPlugins(m map[string]StrCommand) {
	for _, p := range myPlugins {
		f := p.Func
		commandActions[p.Action] = func(args []string) {
			log.Println("command:", p.Action)
			f(CurView(), args)
		}
		m[strings.ToLower(p.Action)] = StrCommand{p.Action, []Completion{NoCompletion}}
		bindingActions[p.Action] = func(v *View, usePlugin bool) bool {
			return f(v, []string{})
		}
	}
}

func bindMyKeys(def map[string]string) {
	for _, p := range myPlugins {
		if p.Key != "" {
			def[p.Key] = p.Action
		}
	}
	def["CtrlLeft"] = "PreviousTab"
	def["CtrlRight"] = "NextTab"
}

func myPluginsPostAction(funcName string, view *View, args ...interface{}) {
	log.Println("postaction:", funcName, "type:", view.Buf.FileType())

	if funcName == "OpenFile" && strings.HasSuffix(view.Buf.Path, ".err") {
		view.Buf.Settings["filetype"] = "err"
		view.setJumpMode([]string{})
	}

	chars = ""
}

var chars string

func replaceAbbrev(view *View, abbrev, replacement string, backpos int) {
	c := &view.Buf.Cursor
	loc := c.Loc
	loc.X = loc.X - len(abbrev) - 1
	c.GotoLoc(loc)
	c.Relocate()
	view.Relocate()
	view.SelectWordRight(false)
	c.DeleteSelection()
	view.Buf.Insert(loc, replacement)
	loc = c.Move(backpos, view.Buf)
	c.GotoLoc(loc)
	c.Relocate()
	view.Relocate()
}

func myPluginsOnRune(view *View, r rune) {
	abbrevs := []struct {
		what        string
		replacement string
		charsback   int
	}{
		{"ife", "if err != nil {\n\t\n\t}\n", -4},
		{"ie", "if err := ", 0},
		{";e", ";err != nil {\t\n\t\t\n\t}", -3},

		{"re", "return err", 0},
		{"rw", "return errors.Wrap(err, \"\")", -2},
		{"lf", "log.Fatal(err)", 0},
		{"tf", "t.Fatal(err)", 0},

		{"pp", "println(\"=== \")", -2},
		{"ff", "fmt.Printf(\"\",)", -3},
		{"fp", "fmt.Println()", -1},
		{"lp", "log.Println()", -1},

		{"fu", "func () {\n}\n", -7},
	}

	log.Println("onrune:", r, "["+chars+"]")
	if r == ' ' {
		for _, a := range abbrevs {
			if chars == a.what {
				replaceAbbrev(view, a.what, a.replacement, a.charsback)
				break
			}
		}
		chars = ""
		return
	}
	chars += string(r)
}

// grep plugin

type grepPlugin struct {
	target *View // target view to insert completion
}

func parseGrepLine(s string) errorLine {
	el := errorLine{}

	cc := strings.Split(s, ":")

	if len(cc) > 0 {
		el.fname = cc[0]
	}
	if len(cc) > 1 {
		el.line, _ = strconv.Atoi(cc[1])
	}
	if len(cc) == 4 {
		el.pos, _ = strconv.Atoi(cc[2])
		el.message = strings.TrimSpace(cc[3])
	} else if len(cc) == 3 {
		el.message = strings.TrimSpace(cc[2])
	}

	return el
}

func (v *View) findInFiles(args []string) bool {
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
	setScratch()
	CurView().handler = func(e *tcell.EventKey) bool { return p.HandleEvent(e) }

	return true
}

func (g *grepPlugin) HandleEvent(e *tcell.EventKey) bool {
	log.Printf("e: %+v", e)
	v := CurView()

	switch e.Key() {
	case tcell.KeyEnter:
		c := v.Cursor
		line := v.Buf.Line(c.Y)
		el := parseGrepLine(line)
		v.gotoError(el)
		return true
	case tcell.KeyEsc, tcell.KeyCtrlSpace:
		v.Quit(false)
		return true
	default:
		return false
	}

	return true
}

// WordComplete plugin

type wordcompletePlugin struct {
	filter string
	target *View // target view to insert completion
	words  []string
}

func (g *wordcompletePlugin) HandleEvent(e *tcell.EventKey) bool {
	log.Printf("e: %+v", e)
	v := CurView()

	switch e.Key() {
	case tcell.KeyRune:
		if e.Modifiers()&tcell.ModAlt == tcell.ModAlt {
			v.Quit(false)
			return true
		}
		g.filter += string(e.Rune())
	case tcell.KeyDEL:
		if len(g.filter) > 0 {
			g.filter = g.filter[:len(g.filter)-1]
		}
	case tcell.KeyEnter:
		c := v.Cursor
		line := v.Buf.Line(c.Y)
		v.Quit(false)
		c = g.target.Cursor
		targetLine := g.target.Buf.Line(c.Y)
		prefix := getLeftChunk(targetLine, c.X)
		line = strings.TrimPrefix(line, prefix)
		g.target.Buf.Insert(Loc{c.X, c.Y}, line)
		messenger.Message("completed: ", prefix+line)
		return true
	case tcell.KeyEsc, tcell.KeyCtrlSpace:
		v.Quit(false)
		return true
	default:
		return false
	}

	messenger.Message("wordcomplete: ", g.filter)

	words := getFiltered(g.words, g.filter)
	b := NewBufferFromString(strings.Join(words, "\n"), "")
	v.OpenBuffer(b)
	setScratch()
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
	sort.Slice(words, func(i, j int) bool { return strings.ToLower(words[i]) < strings.ToLower(words[j]) })
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
		low := strings.ToLower(w)
		if prefix == low {
			continue
		}
		if strings.Contains(low, prefix) {
			res = append(res, w)
		}
	}
	return res
}

func (v *View) wordComplete(args []string) bool {
	c := v.Cursor
	line := v.Buf.Line(c.Y)

	g := &wordcompletePlugin{
		filter: strings.ToLower(getLeftChunk(line, c.X)),
		target: v,
		words:  getWords(v.Buf.Buffer(false)),
	}

	words := getFiltered(g.words, g.filter)
	if len(words) == 1 {
		line := words[0]
		c := g.target.Cursor
		targetLine := g.target.Buf.Line(c.Y)
		prefix := getLeftChunk(targetLine, c.X)
		line = strings.TrimPrefix(line, prefix)
		g.target.Buf.Insert(Loc{c.X, c.Y}, line)
		messenger.Message("word completed: ", words[0])
		return true
	}
	b := NewBufferFromString(strings.Join(words, "\n"), "")
	v.VSplit(b)
	setScratch()
	CurView().handler = func(e *tcell.EventKey) bool { return g.HandleEvent(e) }
	messenger.Message("wordcomplete: ", g.filter)

	return true
}

// GoComplete plugin

type gocompletePlugin struct {
	filter string
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
	v := CurView()

	switch e.Key() {
	case tcell.KeyRune:
		if e.Rune() == '-' {
			v.x++
			v.Width--
			return true
		}
		if e.Rune() == '=' {
			v.x--
			v.Width++
			return true
		}
		g.filter += string(e.Rune())
	case tcell.KeyDEL:
		if len(g.filter) > 0 {
			g.filter = g.filter[:len(g.filter)-1]
		}
	case tcell.KeyEnter:
		c := v.Cursor
		line := v.Buf.Line(c.Y)
		v.Quit(false)
		fields := strings.FieldsFunc(line, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
		if len(fields) < 2 {
			return true
		}

		c = g.target.Cursor
		targetLine := g.target.Buf.Line(c.Y)
		prefix := getLeftChunk(targetLine, c.X)
		ident := fields[1]
		ident = strings.TrimPrefix(ident, prefix)
		g.target.Buf.Insert(Loc{c.X, c.Y}, ident)
		messenger.Message(line)
		return true
	case tcell.KeyEsc, tcell.KeyCtrlSpace:
		v.Quit(false)
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
		if g.filter != "" && !strings.Contains(ls, g.filter) {
			continue
		}
		filtered = append(filtered, ln)
	}
	log.Println("filter:", g.filter, "lines:", len(g.lines), "filtered:", len(filtered))

	text := strings.Join(filtered, "\n")
	b := NewBufferFromString(text, "")
	v.OpenBuffer(b)
	setScratch()
	SetLocal([]string{"filetype", "go"})
	return true
}

func (v *View) goComplete(args []string) bool {
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

	if len(g.lines) == 1 {
		line := g.lines[0]
		fields := strings.FieldsFunc(line, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		})
		if len(fields) < 2 {
			return true
		}

		c = g.target.Cursor
		targetLine := g.target.Buf.Line(c.Y)
		prefix := getLeftChunk(targetLine, c.X)
		ident := fields[1]
		ident = strings.TrimPrefix(ident, prefix)
		g.target.Buf.Insert(Loc{c.X, c.Y}, ident)
		messenger.Message(line)
		return true
	}

	b := NewBufferFromString(strings.Join(g.lines, "\n"), "")
	v.VSplit(b)
	setScratch()
	SetLocal([]string{"filetype", "go"})
	CurView().handler = func(e *tcell.EventKey) bool { return g.HandleEvent(e) }
	return true
}

func setScratch() {
	CurView().Type = vtScratch
	SetLocal([]string{"ruler", "off"})
	SetLocal([]string{"autosave", "off"})
}

type fileopenerPlugin struct {
	dir    string // dir to restore
	filter string
	lsout  string
}

func (g *fileopenerPlugin) Close() {
	g.filter = ""
	CurView().Quit(false)
	if err := os.Chdir(g.dir); err != nil {
		messenger.Error(err.Error())
	}
	log.Printf("opener closed. dir: %+v", g.dir)
}

func (g *fileopenerPlugin) HandleEvent(e *tcell.EventKey) bool {
	log.Printf("e: %+v", e)
	v := CurView()

	switch e.Key() {
	case tcell.KeyEsc:
		g.Close()
		return true
	case tcell.KeyDEL:
		if len(g.filter) > 0 {
			g.filter = g.filter[:len(g.filter)-1]
		}
	case tcell.KeyRune:
		if e.Modifiers()&tcell.ModAlt == tcell.ModAlt && e.Rune() == 'o' {
			g.Close()
			return true
		}
		g.filter += string(e.Rune())
	case tcell.KeyEnter:
		c := v.Cursor
		line := v.Buf.Line(c.Y)
		fi, err := os.Stat(line)
		if !fi.IsDir() {
			v.Quit(false)
			v.AddTab(false)
			fname, err := filepath.Abs(line)
			if err != nil {
				messenger.Error(err.Error())
			}
			CurView().Open(fname)
			if err := os.Chdir(g.dir); err != nil {
				messenger.Error(err.Error())
			}
			return true
		}
		Cd([]string{line})
		g.filter = ""
		g.lsout, err = runLs()
		if err != nil {
			return true
		}
	default:
		return false
	}

	lines := strings.Split(g.lsout, "\n")
	filtered := make([]string, 0, len(lines))
	messenger.Message("filter: ", g.filter)

	for _, ln := range lines {
		if g.filter != "" && !strings.Contains(ln, g.filter) {
			continue
		}
		filtered = append(filtered, ln)
	}
	text := strings.Join(filtered, "\n")
	b := NewBufferFromString(text, "")
	v.OpenBuffer(b)
	setScratch()
	return true
}

func runLs() (string, error) {
	cmd := exec.Command("ls", "-F", "-1", "-a")
	lsout, err := cmd.CombinedOutput()
	if err != nil {
		messenger.Error("ls: " + err.Error())
		return "error: err.Error()", err
	}

	lines := strings.Split(string(lsout), "\n")
	filtered := make([]string, 0, len(lines))
	for _, ln := range lines {
		if ln == "./" {
			continue
		}
		filtered = append(filtered, ln)
	}
	text := strings.Join(filtered, "\n")
	return text, nil
}

func (v *View) openCur(args []string) bool {
	dir, err := os.Getwd()
	if err != nil {
		messenger.Error(err.Error())
		return true
	}
	lsout, err := runLs()
	if err != nil {
		return true
	}
	b := NewBufferFromString(strings.TrimSpace(string(lsout)), "")
	v.VSplit(b)
	setScratch()
	g := &fileopenerPlugin{
		lsout: lsout,
		dir:   dir,
	}
	CurView().handler = func(e *tcell.EventKey) bool { return g.HandleEvent(e) }
	return true
}

func (v *View) goDef(args []string) bool {
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

func (v *View) selectNext(args []string) bool {
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
	log.Printf("goto: %s:%d:%d: %s", ln.fname, ln.line, ln.pos, ln.message)
	if _, err := os.Stat(ln.fname); err != nil {
		return
	}
	if ln.line == 0 {
		ln.line = 1
	}
	if ln.pos == 0 {
		ln.pos = 1
	}

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
	CurView().Center(false)
	messenger.Message(fmt.Sprintf("%s:%d:%d: %s", ln.fname, ln.line, ln.pos, ln.message))
}

func (v *View) goInstall(args []string) bool {
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
	return true
}

type setmodePlugin struct {
	v *View
}

func (v *View) setJumpMode(args []string) bool {
	log.Println("setJumpMode command")
	p := &setmodePlugin{}
	p.v = CurView()
	p.v.Type.Readonly = true
	p.v.Type.Scratch = true
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
	target *View
	filter string
	decls  []astcontext.Decl
}

func (v *View) goDecls(args []string) bool {
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
		_, _ = fmt.Fprintf(&w, "%4d: %s\n", d.Line, d.Full)
	}
	p.decls = res.Decls

	b := NewBufferFromString(strings.TrimSuffix(w.String(), "\n"), "")
	v.HSplit(b)
	setScratch()
	SetLocal([]string{"filetype", "go"})
	CurView().handler = func(e *tcell.EventKey) bool { return p.HandleEvent(e) }

	return true
}

func (g *godeclsPlugin) HandleEvent(e *tcell.EventKey) bool {
	v := CurView()

	switch e.Key() {
	case tcell.KeyRune:
		if e.Modifiers()&tcell.ModAlt == tcell.ModAlt {
			v.Quit(false)
			return true
		}
		g.filter += string(e.Rune())
	case tcell.KeyDEL:
		if len(g.filter) > 0 {
			g.filter = g.filter[:len(g.filter)-1]
		}
	case tcell.KeyEnter:
		c := v.Cursor
		line := v.Buf.Line(c.Y)
		v.Quit(false)
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
		v.Quit(false)
		return true
	default:
		return false
	}

	var w bytes.Buffer
	for _, d := range g.decls {
		if strings.Contains(strings.ToLower(d.Full), g.filter) {
			_, _ = fmt.Fprintf(&w, "%4d: %s\n", d.Line, d.Full)
		}
	}
	b := NewBufferFromString(strings.TrimSuffix(w.String(), "\n"), "")
	v.OpenBuffer(b)
	setScratch()
	SetLocal([]string{"filetype", "go"})
	return true
}

// exec plugin: executes the command under the cursor and opens split view for jumping to location

type execPlugin struct {
	target *View // target view to insert completion
}

func (v *View) execCommand(args []string) bool {
	sel := v.Cursor.GetSelection()
	if sel == "" {
		v.Cursor.SelectWord()
		sel = v.Cursor.GetSelection()
	}
	if sel == "" {
		return true
	}

	p := &execPlugin{}
	if !strings.HasPrefix(v.Buf.Path, "Exec: ") {
		v.Save(false)
	}

	cmd := exec.Command("bash", "-c", sel)
	buf, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("exec:", err)
		messenger.Error(err.Error())
	}
	b := NewBufferFromString(strings.TrimSpace(string(buf)), "Exec: "+sel)
	v.HSplit(b)
	setScratch()
	CurView().handler = func(e *tcell.EventKey) bool { return p.HandleEvent(e) }

	return true
}

func (g *execPlugin) HandleEvent(e *tcell.EventKey) bool {
	v := CurView()

	switch e.Key() {
	case tcell.KeyEnter:
		c := v.Cursor
		line := v.Buf.Line(c.Y)
		el := parseGrepLine(line)
		if el.fname == "" {
			return true
		}
		v.gotoError(el)
		return true
	case tcell.KeyEsc, tcell.KeyCtrlSpace:
		v.Quit(false)
		return true
	default:
		return false
	}

	return true
}

// text filter

func (v *View) textFilter(args []string) bool {
	if len(args) == 0 {
		messenger.Error("usage: textfilter arguments")
		return true
	}
	log.Println("TextFilter command", args)
	sel := v.Cursor.GetSelection()
	if sel == "" {
		v.Cursor.SelectWord()
		sel = v.Cursor.GetSelection()
	}
	if sel == "" {
		return true
	}
	var bout, berr bytes.Buffer
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(sel)
	cmd.Stderr = &berr
	cmd.Stdout = &bout
	err := cmd.Run()
	if err != nil {
		messenger.Error(err.Error() + " " + berr.String())
		return true
	}
	v.Cursor.DeleteSelection()
	v.Buf.Insert(v.Cursor.Loc, bout.String())
	return true
}

func printAutocomplete() {
	a := flag.Arg(flag.NArg() - 1)
	p := flag.Arg(flag.NArg() - 2)

	switch a {
	case "-ruler":
		{
			list := strings.Split("on,off", ",")
			for _, s := range list {
				if strings.HasPrefix(s, p) {
					fmt.Println(s)
				}
			}
			return
		}
	}

	log.Println("arg:", a, "par:", p)

	if strings.HasPrefix(p, "-") {
		flag.VisitAll(func(f *flag.Flag) {
			if f.Name == "c" {
				return
			}
			if strings.HasPrefix("-"+f.Name, flag.Arg(1)) {
				fmt.Println("-" + f.Name)
			}
		})
		return
	}

	if strings.HasPrefix(p, "~") {
		p = strings.Replace(p, "~", os.Getenv("HOME"), 1)
	}

	files, err := filepath.Glob(p + "*")
	if err != nil {
		log.Printf("glob error: %v", err)
	}
	log.Printf("p: %s, files: %v", p, files)
	for _, file := range files {
		if strings.HasPrefix(file, p) {
			n := file
			fi, _ := os.Stat(n)
			if fi.IsDir() {
				n += "/"
			} else {
				n += " "
			}
			fmt.Println(n)
		}
	}
}

var (
	autocompleteScript = `complete -o nospace -C "micro -c" micro`
	acfile             = os.Getenv("HOME") + "/.config/bash_completion/micro"
)

func initAutocomplete(file, script string) {
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		panic(err)
	}

	actext, _ := ioutil.ReadFile(file)
	if string(actext) != script {
		if err := ioutil.WriteFile(file, []byte(script), 0644); err != nil {
			panic(err)
		}
		fmt.Println("autocomplete installed. Run source ~/.bashrc now")
	}

	what := `for i in ~/.config/bash_completion/*; do source $i ; done`
	bashrc := os.Getenv("HOME") + "/.bashrc"
	buf, _ := ioutil.ReadFile(bashrc)
	lines := strings.Split(string(buf), "\n")
	found := false
	for _, ln := range lines {
		if ln == what {
			found = true
			break
		}
	}

	if !found {
		f, err := os.OpenFile(bashrc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
		if err != nil {
			panic(err)
		}

		fmt.Fprintf(f, "\n%s\n", what)
		f.Close()
		fmt.Println("common autocomplete installed. Run source ~/.bashrc now")
	}
}

func (v *View) goUpdateBinaries(args []string) bool {
	list := []string{
		"golang.org/x/tools/cmd/goimports",
		"github.com/rogpeppe/godef",
		"github.com/fatih/motion",
		"github.com/mdempsky/gocode",
	}

	for _, item := range list {
		cmd := exec.Command("go", "get", "-u", item)
		if err := cmd.Run(); err != nil {
			messenger.Error(cmd.Args, err.Error())
			return true
		}
	}
	messenger.Message("golang tools updated")
	return true
}
