package action

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/zyedidia/micro/internal/buffer"
	"github.com/zyedidia/tcell"
)

type qfixPane struct {
	*BufPane
	filter string
	text   string
}

// ExecCmd executes the command with arguments from the current directory.
func (h *BufPane) ExecCmd(args []string) {
	if len(args) == 0 {
		InfoBar.Message("usage: exec [-flags] command args...")
		return
	}

	// substitute parameters

	var list []string
	loc := buffer.Loc{h.Cursor.X, h.Cursor.Y}
	offs := buffer.ByteOffset(loc, h.Buf)
	offset := strconv.Itoa(offs)

	for _, a := range args {
		log.Println("arg", a)
		switch {
		case a == "{s}" && h.Cursor.HasSelection():
			a = string(h.Cursor.GetSelection())
		case a == "{w}":
			h.Cursor.SelectWord()
			a = strings.TrimSpace(string(h.Cursor.GetSelection()))
			h.Cursor.Deselect(true)
			if a == "" {
				continue
			}
		case a == "{o}":
			a = offset
		case a == "{f}":
			a = h.Buf.AbsPath
		}
		list = append(list, a)
	}

	// exec command and get all output

	cmd := exec.Command(list[0], list[1:]...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "MICRO_FILE_PATH="+h.Buf.AbsPath)
	cmd.Env = append(cmd.Env, "MICRO_FILE_OFFSET="+offset)

	buf, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("exec:", err, "out:", string(buf))
		InfoBar.Error(err.Error())
	}

	// create horizontal pane with output text

	b := buffer.NewBufferFromString(strings.TrimSpace(string(buf)), list[0], buffer.BTLog)
	e := &qfixPane{
		BufPane: NewBufPaneFromBuf(b),
		text:    string(buf),
	}

	e.splitID = MainTab().GetNode(h.splitID).HSplit(h.Buf.Settings["splitbottom"].(bool))
	MainTab().Panes = append(MainTab().Panes, e)
	MainTab().Resize()
	MainTab().SetActive(len(MainTab().Panes) - 1)
}

func (h *qfixPane) HandleEvent(event tcell.Event) {
	prevfilter := h.filter
	switch e := event.(type) {
	case *tcell.EventKey:
		switch e.Key() {
		case tcell.KeyRune:
			h.filter += string(e.Rune())
		case tcell.KeyDEL:
			if len(h.filter) > 0 {
				h.filter = h.filter[:len(h.filter)-1]
			}
		case tcell.KeyEnter:
			c := h.Cursor
			line := h.Buf.Line(c.Y)
			gl := parseGrepLine(line)
			h.jumpToLine(gl)
			return
		case tcell.KeyEsc:
			h.Quit()
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

	var foundPane Pane

	for i, t := range Tabs.List {
		for j, p := range t.Panes {
			bp, ok := p.(*BufPane)
			if !ok || fname != bp.Buf.AbsPath {
				continue
			}
			Tabs.SetActive(i)
			t.SetActive(j)
			bp.SetActive(true)
			foundPane = t.CurPane()
			break
		}
	}

	if foundPane != nil {
		foundPane.HandleCommand(fmt.Sprintf("goto %d:%d", ln.line, ln.pos))
	} else {
		h.HandleCommand(fmt.Sprintf("tab %s:%d:%d", ln.fname, ln.line, ln.pos))
	}

	bp, ok := Tabs.List[Tabs.Active()].CurPane().(*BufPane)
	if ok {
		bp.Center()
	}

	InfoBar.Message(fmt.Sprintf("%s:%d:%d: %s", ln.fname, ln.line, ln.pos, ln.message))
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
