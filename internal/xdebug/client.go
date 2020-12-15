package xdebug

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
)

// Editor is a callback interface for editor automation.
type Editor interface {
	OpenCmd([]string)           // open FILE
	GotoCmd([]string)           // goto LINE
	Message(msg ...interface{}) // shows message on status bar
	Error(msg ...interface{})   // shows error message on status bar
}

// Client is a xdebug client. Inits yaml-tagged fields from ./init.yaml file.
type Client struct {
	BasePath    string   `yaml:"base_path"` // source root dir i.e. file:///path/to/the/project/root/
	InitCommand string   `yaml:"init"`      // command which calls php project after xdebug started
	Breakpoints []string `yaml:breakpoints`

	Editor Editor // callback interface for editor automation

	listener *net.TCPListener // listener on :9004
	conn     *net.TCPConn     // accepted xdebugger connection
	transID  int              // transaction ID
	currLine int
	currFile string
	prevLine string
	started  bool
}

func (xc *Client) jumpToFile() {
	fname := strings.TrimPrefix(xc.currFile, xc.BasePath)
	log.Println("open", fname, xc.currLine)
	xc.Editor.OpenCmd([]string{fname})
	xc.Editor.GotoCmd([]string{strconv.Itoa(xc.currLine)})
}

func (xc *Client) handleResponse(resp Response) error {
	switch resp.Command {
	case "step_over", "step_into", "step_out":
		xc.currFile = resp.Message.Filename
		xc.currLine = resp.Message.Line
		xc.jumpToFile()
	case "stack_get":
		var b bytes.Buffer
		xc.dumpStack(&b, resp)
		log.Println("\n", b.String())
	case "run":
		if resp.Status == "stopping" && resp.Reason == "ok" {
			if err := xc.Close(); err != nil {
				log.Println(err)
			}
			xc.started = false
			xc.Editor.Message("debugger stopped. F8 to start")
			return nil
		}
		if resp.Status == "break" && resp.Reason == "ok" && resp.Message.Filename != "" {
			xc.currFile = resp.Message.Filename
			xc.currLine = resp.Message.Line
			xc.jumpToFile()
			xc.transID++

			s := fmt.Sprintf("stack_get -i %d\x00", xc.transID)
			xc.transID++
			resp, err := xc.send(s)
			if err != nil {
				return err
			}

			if resp.Command != "stack_get" {
				log.Fatal("returned command is not stack_get")
			}

			if err := xc.handleResponse(resp); err != nil {
				return err
			}

			s = fmt.Sprintf("source -i %d -f %s\x00", xc.transID, xc.currFile)
			xc.transID++
			resp, err = xc.send(s)
			if err != nil {
				return err
			}

			if err := xc.handleResponse(resp); err != nil {
				return err
			}
		}
		xc.Editor.Message(fmt.Sprintln("run:", resp.Status, resp.Reason))
	case "breakpoint_list":
		//		dumpBreakpoints(resp.Breakpoints)
	case "source":
		b, err := base64.StdEncoding.DecodeString(resp.Text)
		if err != nil {
			return err
		}

		lines := strings.Split(string(b), "\n")
		if len(lines) > 0 {
			xc.prevLine = strings.TrimSpace(lines[xc.currLine-1])
		} else {
			xc.prevLine = ""
		}
	default:
		log.Printf("%+v", resp)
	}

	return nil
}

func (xc *Client) send(s string) (Response, error) {
	var resp Response

	log.Println("send:", s)
	_, err := xc.conn.Write([]byte(s))
	if err != nil {
		return resp, err
	}

	b, err := readBlock(xc.conn)
	if err != nil {
		return resp, err
	}

	resp, err = unmarshalCommand(b)
	if err != nil {
		return resp, err
	}

	if resp.Command != "source" && resp.Command != "stack_get" && resp.Command != "eval" {
		log.Println("block:", string(b))
	}

	return resp, nil
}

func (xc *Client) step(stepCmd string) error {
	s := fmt.Sprintf("%s -i %d\x00", stepCmd, xc.transID)
	resp, err := xc.send(s)
	if err != nil {
		return err
	}

	if err := xc.handleResponse(resp); err != nil {
		return err
	}

	xc.transID++

	var evalResult bytes.Buffer

	if xc.prevLine != "" && strings.Contains(xc.prevLine, " = ") && strings.HasSuffix(xc.prevLine, ";") && strings.HasPrefix(xc.prevLine, "$") {
		cc := strings.Split(strings.TrimSpace(xc.prevLine), " = ")
		eargs := base64.StdEncoding.EncodeToString([]byte("var_export(" + cc[0] + ", TRUE)"))
		s := fmt.Sprintf("eval -i %d -- %s\x00", xc.transID, eargs)
		resp, err = xc.send(s)
		if err != nil {
			return err
		}

		fmt.Fprintf(&evalResult, "\n=== \x1b[31m%s\x1b[0m ===\n", cc[0])
		dumpProperties(&evalResult, resp.Properties, 0)
		xc.transID++
	}

	s = fmt.Sprintf("stack_get -i %d\x00", xc.transID)
	xc.transID++
	resp, err = xc.send(s)
	if err != nil {
		return err
	}

	if err := xc.handleResponse(resp); err != nil {
		return err
	}

	s = fmt.Sprintf("source -i %d -f %s\x00", xc.transID, xc.currFile)
	xc.transID++
	resp, err = xc.send(s)
	if err != nil {
		return err
	}

	if err := xc.handleResponse(resp); err != nil {
		return err
	}
	log.Println(evalResult.String())

	return nil
}

// Start starts debug session. It starts listening on :9003, makes initial request to php server
// and accepts connection from xdebug. Then it sets breakpoints from init.yaml file.
func (xc *Client) Start() error {
	var err error
	xc.listener, err = listen()
	if err != nil {
		return err
	}

	if err := xc.readInitFile(); err != nil {
		return err
	}

	go xc.makeRequest()

	xc.conn, err = accept(xc.listener)
	if err != nil {
		return err
	}

	if err := xc.processParameters(); err != nil {
		return err
	}

	xc.Editor.Message("started. F5-run F6-step_out F7-step_in F8-step_over F10-exit")

	return nil
}

// Close closes accepted and listening sockets.
func (xc *Client) Close() error {
	if err := xc.listener.Close(); err != nil {
		log.Println(err)
	}
	if err := xc.conn.Close(); err != nil {
		log.Println(err)
	}

	log.Println("Client closed")

	return nil
}

func (xc *Client) makeRequest() {
	cmd := exec.Command("sh", "-c", xc.InitCommand)
	b, err := cmd.CombinedOutput()
	if err != nil {
		log.Println(err, xc.InitCommand, "out:", string(b))
		xc.Editor.Error(err)
		return
	}
	log.Println("init command:", xc.InitCommand, "\nresult:\n", string(b))
}

func dumpProperties(w io.Writer, props []property, depth int) {
	for i, p := range props {
		pad := strings.Repeat("    ", depth)
		v := p.Text
		if p.Encoding == "base64" {
			b, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				v = "error:" + err.Error()
			} else {
				v = string(b)
			}
		}
		fmt.Fprintf(w, "%s%2d %s %s:%s = %s\n", pad, i+1, p.ClassName, p.Name, p.Type, v)
		dumpProperties(w, p.Properties, 0)
	}
}

func (xc *Client) dumpStack(w io.Writer, resp Response) {
	fmt.Fprintln(w, "=== stack ===")

	for i := len(resp.Stack) - 1; i >= 0; i-- {
		s := resp.Stack[i]
		fname := strings.Replace(s.Filename, xc.BasePath, "", 1)
		fmt.Fprintf(w, "%d %s:%d \x1b[32m%s\x1b[0m\n", i, fname, s.Line, s.Where)
	}

	fmt.Fprintln(w, "=============")
}

func dumpBreakpoints(br []breakpoint) {
	fmt.Println("=== breakpoints ===")
	for i, b := range br {
		fmt.Printf("%2d %s:%d %s\n", i+1, b.Filename, b.Line, b.Type)
	}
}

func (xc *Client) readInitFile() error {
	f, err := os.Open("init.yaml")
	if err != nil && os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("init.yaml open. error: %w", err)
	}

	defer f.Close()

	err = yaml.NewDecoder(f).Decode(xc)
	if err != nil {
		return fmt.Errorf("init.yaml decode. error: %w", err)
	}

	log.Printf("xdebug: %+v", xc)

	return nil
}

func (xc *Client) processParameters() error {

	for _, b := range xc.Breakpoints {
		cc := strings.Split(b, " ")
		s := fmt.Sprintf("breakpoint_set -i %d -t line -f %s%s -n %s\x00", xc.transID, xc.BasePath, cc[0], cc[1])
		xc.transID++
		resp, err := xc.send(s)
		if err != nil {
			return fmt.Errorf("set breakpoint. error: %w", err)
		}
		if err := xc.handleResponse(resp); err != nil {
			return err
		}
	}

	return nil
}

// ProcessCommand handles commands from the editor. It interacts with the editor by
// calling Editor interface methods.
func (xc *Client) ProcessCommand(args []string) error {
	var t string

	if len(args) > 0 {
		t = args[0]
	}

	if !xc.started && (t == "s" || t == "n" || t == "c") {
		t = "start"
	}

	if !xc.started && t != "start" {
		return fmt.Errorf("phpdebug is not started")
	}

	switch t {
	case "start":
		if xc.started {
			return fmt.Errorf("phpdebug already started")
		}
		if err := xc.Start(); err != nil {
			return err
		}
		xc.started = true
		if err := xc.step("step_into"); err != nil {
			return err
		}
	case "stop":
		if err := xc.Close(); err != nil {
			return err
		}
		xc.started = false
	case "so":
		if err := xc.step("step_out"); err != nil {
			return err
		}
	case "s":
		if err := xc.step("step_into"); err != nil {
			return err
		}
	case "n":
		if err := xc.step("step_over"); err != nil {
			return err
		}
	case "c":
		s := fmt.Sprintf("run -i %d\x00", xc.transID)
		resp, err := xc.send(s)
		if err != nil {
			return err
		}

		if err := xc.handleResponse(resp); err != nil {
			return err
		}
	case "b":
		s := fmt.Sprintf("breakpoint_set -i %d -t line -f %s -n %d\x00", xc.transID, xc.currFile, xc.currLine)
		xc.transID++
		resp, err := xc.send(s)
		if err != nil {
			return err
		}
		if err := xc.handleResponse(resp); err != nil {
			return err
		}
		s = fmt.Sprintf("breakpoint_list -i %d\x00", xc.transID)
		xc.transID++
		resp, err = xc.send(s)
		if err != nil {
			return err
		}
		if err := xc.handleResponse(resp); err != nil {
			return err
		}
	case "bl":
		s := fmt.Sprintf("breakpoint_list -i %d\x00", xc.transID)
		xc.transID++
		resp, err := xc.send(s)
		if err != nil {
			xc.Editor.Error(err)
			return err
		}
		if err := xc.handleResponse(resp); err != nil {
			return err
		}
	case "e":
		args := strings.Join(args[1:], " ")
		eargs := base64.StdEncoding.EncodeToString([]byte("var_export(" + args + ", TRUE)"))
		s := fmt.Sprintf("eval -i %d -- %s\x00", xc.transID, eargs)
		resp, err := xc.send(s)
		if err != nil {
			return err
		}
		var b bytes.Buffer
		dumpProperties(&b, resp.Properties, 0)
		log.Println("=============== eval\n\x1b[31m", args, "\x1b[0m\n", b.String())
	default:
		s := fmt.Sprintf("%s -i %d\x00", t, xc.transID)
		resp, err := xc.send(s)
		if err != nil {
			return err
		}
		if err := xc.handleResponse(resp); err != nil {
			return err
		}
	}

	return nil
}
