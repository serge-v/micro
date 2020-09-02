package action

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/zyedidia/micro/v2/internal/buffer"
	"github.com/zyedidia/micro/v2/internal/util"
	"github.com/zyedidia/tcell"
)

type qfixPane struct {
	*BufPane
	filter string
	text   string
	gocode bool
	quit   bool
	target *BufPane
}

func compgen(b *buffer.Buffer) ([]string, []string) {
	c := b.GetActiveCursor()
	input, argstart := buffer.GetArg(b)

	cmd := exec.Command("bash", "-c", "compgen -c "+input)
	buf, err := cmd.Output()
	if err != nil {
		log.Println(err)
		return nil, nil
	}

	lines := strings.Split(strings.TrimSpace(string(buf)), "\n")

	var suggestions []string

	for _, option := range lines {
		if strings.HasPrefix(option, input) {
			suggestions = append(suggestions, option)
		}
	}

	sort.Strings(suggestions)
	completions := make([]string, len(suggestions))
	for i := range suggestions {
		completions[i] = util.SliceEndStr(suggestions[i], c.X-argstart)
	}
	return completions, suggestions

}

// ExecCmd executes the command with arguments from the current directory.
func (h *BufPane) ExecCmd(args []string) {
	if len(args) == 0 {
		InfoBar.Message("usage: exec [-flags] command args...")
		return
	}
	if h != nil && h.Buf != nil && h.Buf.Modified() {
		saved := h.Save()
		log.Println("save:", saved)
	}

	// substitute parameters

	var list []string
	var sel string
	var offset string
	var word string
	var line string
	var pos string

	if h != nil {
		loc := buffer.Loc{h.Cursor.X, h.Cursor.Y}
		line = strconv.Itoa(h.Cursor.Y + 1)
		pos = strconv.Itoa(h.Cursor.X + 1)
		offs := buffer.ByteOffset(loc, h.Buf)
		offset = strconv.Itoa(offs)
		if h.Cursor.HasSelection() {
			sel = string(h.Cursor.GetSelection())
		} else {
			h.Cursor.SelectWord()
			word = strings.TrimSpace(string(h.Cursor.GetSelection()))
			h.Cursor.Deselect(true)
		}

		repl := strings.NewReplacer(
			"{s}", sel,
			"{w}", word,
			"{f}", h.Buf.AbsPath,
			"{o}", offset,
		)

		for _, a := range args {
			a = repl.Replace(a)
			list = append(list, a)
		}
	}

	// exec command and get all output

	cmd := exec.Command(list[0], list[1:]...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "MICRO_FILE_PATH="+h.Buf.AbsPath)
	cmd.Env = append(cmd.Env, "MICRO_FILE_OFFSET="+offset)
	cmd.Env = append(cmd.Env, "MICRO_FILE_LINE="+line)
	cmd.Env = append(cmd.Env, "MICRO_FILE_POS="+pos)
	cmd.Env = append(cmd.Env, "MICRO_SELECTION="+sel)
	cmd.Env = append(cmd.Env, "MICRO_CURR_WORD="+word)

	buf, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(buf))
	if text == "" && err != nil {
		log.Println("exec:", err)
		InfoBar.Error(err.Error())
		return
	}

	// create horizontal pane with output text
	if text == "" && err == nil {
		InfoBar.Message(list[0], ":0")
		return
	}

	e := findQfixPane("cmd:" + list[0])
	if e == nil {
		b := buffer.NewBufferFromString(text, "cmd:"+list[0], buffer.BTScratch)
		e = &qfixPane{
			BufPane: NewBufPaneFromBuf(b, h.tab),
			text:    text,
			gocode:  args[0] == "gocode",
			quit:    args[0] == "gocode" || args[0] == "motion",
			target:  h,
		}

		bottom := true
		if h != nil {
			bottom = h.Buf.Settings["splitbottom"].(bool)
			e.splitID = MainTab().GetNode(h.splitID).HSplit(bottom)
		} else {
			e.splitID = MainTab().GetNode(0).HSplit(bottom)
		}
		MainTab().Panes = append(MainTab().Panes, e)
		MainTab().Resize()
		MainTab().SetActive(len(MainTab().Panes) - 1)
	} else {
		loc := buffer.Loc{}
		e.Buf.Insert(loc, text+"\n=========== "+time.Now().String()+"\n\n")
	}
	e.HandleCommand("goto 0:0")
}

func (h *qfixPane) HandleEvent(event tcell.Event) {
	prevfilter := h.filter
	switch e := event.(type) {
	case *tcell.EventKey:
		switch e.Key() {
		case tcell.KeyRune:
			h.filter += string(e.Rune())
			InfoBar.Message("filter: " + h.filter)
		case tcell.KeyDEL:
			if len(h.filter) > 0 {
				h.filter = h.filter[:len(h.filter)-1]
			}
			InfoBar.Message("filter: " + h.filter)
		case tcell.KeyEnter:
			if h.gocode {
				h.autocompleteLine()
				return
			}

			c := h.Cursor
			line := strings.TrimSpace(h.Buf.Line(c.Y))
			if line == "" {
				return
			}
			if h.quit {
				h.Quit()
			}
			gl := parseGrepLine(line)
			h.jumpToLine(gl)
			InfoBar.Message("")
			log.Printf("jump: %+v", gl)
			return
		case tcell.KeyEsc:
			h.Quit()
			InfoBar.Message("")
			return
		}
	}

	if prevfilter != h.filter {
		text := filterLines(h.text, h.filter)
		b := buffer.NewBufferFromString(text, "", buffer.BTLog)
		h.OpenBuffer(b)
	}

	h.BufPane.HandleEvent(event)
}

func (h *qfixPane) autocompleteLine() {
	c := h.Cursor
	line := h.Buf.Line(c.Y)
	h.Quit()
	fields := strings.FieldsFunc(line, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(fields) < 2 {
		return
	}

	c = h.target.Cursor
	targetLine := h.target.Buf.Line(c.Y)
	prefix := getLeftChunk(targetLine, c.X)
	ident := fields[1]
	ident = strings.TrimPrefix(ident, prefix)
	h.target.Buf.Insert(buffer.Loc{c.X, c.Y}, ident)
	InfoBar.Message(line)
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

func findQfixPane(fname string) *qfixPane {
	for i, t := range Tabs.List {
		log.Printf("tab: %d", t.ID())
		for j, p := range t.Panes {
			pane, ok := p.(*qfixPane)
			log.Printf("pane: %d %s %T", j, p.Name(), p)
			if !ok || fname != p.Name() {
				continue
			}
			Tabs.SetActive(i)
			t.SetActive(j)
			p.SetActive(true)
			return pane
		}
	}
	return nil
}

func (h *qfixPane) jumpToLine(ln grepLine) {
	if _, err := os.Stat(ln.fname); err != nil {
		return
	}
	if ln.line == 0 {
		ln.line = 1
	}
	if ln.pos == 0 {
		ln.pos = 1
	}

	fname, _ := filepath.Abs(ln.fname)
	foundPane := findQfixPane(fname)
	if foundPane != nil {
		foundPane.HandleCommand(fmt.Sprintf("goto %d:%d", ln.line, ln.pos))
	} else {
		h.HandleCommand(fmt.Sprintf("tab %s:%d:%d", ln.fname, ln.line, ln.pos))
	}

	bp := Tabs.List[Tabs.Active()].CurPane()
	bp.Center()
}

type grepLine struct {
	fname   string
	line    int
	pos     int
	message string
}

func parseGrepLine(s string) grepLine {
	line := grepLine{}

	cc := strings.SplitN(s, ":", 4)

	if len(cc) > 0 {
		line.fname = strings.TrimSpace(cc[0])
	}
	if len(cc) > 1 {
		line.line, _ = strconv.Atoi(cc[1])
	}
	if len(cc) > 3 {
		line.pos, _ = strconv.Atoi(cc[2])
		line.message = strings.TrimSpace(cc[3])
	} else if len(cc) > 2 {
		line.message = strings.TrimSpace(cc[2])
	}

	return line
}

func filterLines(text, filter string) string {
	lines := strings.Split(text, "\n")
	var out []string
	for _, ln := range lines {
		low := strings.ToLower(ln)
		if strings.Contains(low, filter) {
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}
