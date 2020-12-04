package action

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
	"gopkg.in/yaml.v2"
)

type proxyResponse struct {
	XMLName xml.Name
	Success int    `xml:"success,attr"`
	Idekey  string `xml:"idekey,attr"`
	Address string `xml:"address,attr"`
	Port    int    `xml:"port,attr"`
	Ssl     bool   `xml:"ssl,attr"`
}

func req(addr, s string) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}

	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	n, err := conn.Write([]byte(s))
	if err != nil {
		log.Fatal(err)
	}

	log.Println("write to server = ", s, n)

	buf, err := readBlock(conn)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("reply from server=", string(buf))
	//	fmt.Println(addr, s, string(buf))

	var resp proxyResponse
	err = xml.Unmarshal(buf, &resp)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%v, resp: %+v\n", err, resp)
}

func listen() (*net.TCPListener, error) {
	var err error

	tcpAddr, err := net.ResolveTCPAddr("tcp", "172.17.0.1:9003")
	if err != nil {
		return nil, fmt.Errorf("resolve. error: %w", err)
	}

	l, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		return nil, fmt.Errorf("listen. error: %w", err)

	}
	return l, nil
}

func accept(l *net.TCPListener) (*net.TCPConn, error) {
	var err error

	log.Println("waiting for connect from xdebug proxy to :9004")
	InfoBar.Message("waiting on :9004")

	conn, err := l.AcceptTCP()
	if err != nil {
		return nil, fmt.Errorf("accept. error: %w", err)
	}

	err = conn.SetReadDeadline(time.Time{})
	if err != nil {
		return nil, fmt.Errorf("deadline. error: %w", err)
	}

	err = conn.SetLinger(0)
	if err != nil {
		return nil, fmt.Errorf("linger. error: %w", err)
	}

	b, err := readBlock(conn)
	if err != nil {
		return nil, fmt.Errorf("read block. error: %w", err)
	}

	if _, err := unmarshalCommand(b); err != nil {
		return nil, err
	}

	log.Println("accepted")
	InfoBar.Message("accepted on :9004")

	return conn, nil
}

type property struct {
	Type       string     `xml:"type,attr"`
	Name       string     `xml:"name,attr"`
	ClassName  string     `xml:"classname,attr"`
	Encoding   string     `xml:"encoding,attr"`
	Text       string     `xml:",cdata"`
	Properties []property `xml:"property"`
}

type breakpoint struct {
	Type     string `xml:"type,attr"`
	Filename string `xml:"filename,attr"`
	Line     int    `xml:"lineno,attr"`
	State    string `xml:"state,attr"`
	HitCount int    `xml:"hitcount,attr"`
	HitValue int    `xml:"hitvalue,attr"`
	ID       int    `xml:"id,attr"`
}

type Response struct {
	Command  string `xml:"command,attr"`
	TrID     int    `xml:"transaction_id,attr"`
	Encoding string `xml:"encoding,attr"`
	Status   string `xml:"status,attr"`
	Reason   string `xml:"reason,attr"`
	Text     string `xml:",cdata"`
	Error    struct {
		Code    int `xml:"code,attr"`
		Message struct {
			Text string `xml:",cdata"`
		} `xml:"message"`
	} `xml:"error"`
	Message struct {
		Line     int    `xml:"lineno,attr"`
		Filename string `xml:"filename,attr"`
	} `xml:"message"`
	Stack []struct {
		Level    int    `xml:"level,attr"`
		Line     int    `xml:"lineno,attr"`
		Filename string `xml:"filename,attr"`
		Where    string `xml:"where,attr"`
		CmdBegin string `xml:"cmdbegin,attr"`
		CmdEnd   string `xml:"cmdend,attr"`
	} `xml:"stack"`
	Properties  []property   `xml:"property"`
	Breakpoints []breakpoint `xml:"breakpoint"`
}

func unmarshalCommand(b []byte) (Response, error) {
	var resp Response

	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.CharsetReader = charset.NewReaderLabel

	err := dec.Decode(&resp)
	if err != nil {
		log.Println(err, string(b))
		return Response{}, fmt.Errorf("read block. error: %w", err)
	}

	return resp, nil
}

type xdebugClient struct {
	listener    *net.TCPListener // listener on :9004
	conn        *net.TCPConn     // accepted xdebugger connection
	transID     int              // transaction ID
	currLine    int
	currFile    string
	prevLine    string
	pane        *BufPane
	BasePath    string   `yaml:"base_path"` // file:///path/to/the/project/root/
	InitCommand string   `yaml:"init"`      // command which calls php project after xdebug started
	Breakpoints []string `yaml:breakpoints`
}

func (xc *xdebugClient) jumpToFile() {
	fname := strings.TrimPrefix(xc.currFile, xc.BasePath)
	log.Println("open", fname, xc.currLine)
	xc.pane.OpenCmd([]string{fname})
	xc.pane.GotoCmd([]string{strconv.Itoa(xc.currLine)})
}

func (xc *xdebugClient) processResponse(resp Response) {
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
			xdebug.Close()
			xdebug = nil
			InfoBar.Error("debugger stopped. F8 to start")
			return
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
				InfoBar.Error(err)
				return
			}

			if resp.Command != "stack_get" {
				log.Fatal("returned command is not stack_get")
			}
			xc.processResponse(resp)

			s = fmt.Sprintf("source -i %d -f %s\x00", xc.transID, xc.currFile)
			xdebug.transID++
			resp, err = xc.send(s)
			if err != nil {
				InfoBar.Error(err)
				return
			}

			if resp.Command != "source" {
				InfoBar.Error("not source")
			}
			xc.processResponse(resp)
		}
		InfoBar.Message(fmt.Sprintln("run:", resp.Status, resp.Reason))
	case "breakpoint_list":
		//		dumpBreakpoints(resp.Breakpoints)
	case "source":
		b, err := base64.StdEncoding.DecodeString(resp.Text)
		if err != nil {
			log.Fatal(err)
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
}

func readBlock(r io.Reader) ([]byte, error) {
	c := make([]byte, 1)
	var length int64

	for {
		_, err := r.Read(c)
		if err != nil {
			return nil, err
		}
		if c[0] == 0 {
			break
		}
		if c[0] < '0' && c[0] > 9 {
			log.Fatal("not a number")
		}

		length = length*10 + int64(c[0]-'0')
	}

	log.Println("length", length)
	var b bytes.Buffer
	n, err := io.CopyN(&b, r, length+1)
	if err != nil {
		return nil, err
	}

	if n != length+1 {
		log.Println("n!=lenght", n, length)
	}

	return b.Bytes(), nil
}

func (xc *xdebugClient) send(s string) (Response, error) {
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

func (xc *xdebugClient) step(stepCmd string) error {
	s := fmt.Sprintf("%s -i %d\x00", stepCmd, xc.transID)
	resp, err := xc.send(s)
	if err != nil {
		return err
	}

	xc.processResponse(resp)
	xc.transID++

	var evalResult bytes.Buffer

	if xc.prevLine != "" && strings.Contains(xc.prevLine, " = ") && strings.HasSuffix(xc.prevLine, ";") && strings.HasPrefix(xc.prevLine, "$") {
		cc := strings.Split(strings.TrimSpace(xc.prevLine), " = ")
		eargs := base64.StdEncoding.EncodeToString([]byte("var_export(" + cc[0] + ", TRUE)"))
		s := fmt.Sprintf("eval -i %d -- %s\x00", xdebug.transID, eargs)
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

	xc.processResponse(resp)

	s = fmt.Sprintf("source -i %d -f %s\x00", xc.transID, xc.currFile)
	xc.transID++
	resp, err = xc.send(s)
	if err != nil {
		return err
	}

	if resp.Command != "source" {
		log.Fatal("returned command is not source")
	}
	xc.processResponse(resp)
	log.Println(evalResult.String())

	return nil
}

func (xc *xdebugClient) Start() error {
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

	InfoBar.Message("started. F5-run F6-step_out F7-step_in F8-step_over F10-exit")

	return nil
}

func (xc *xdebugClient) Close() error {
	if err := xc.listener.Close(); err != nil {
		log.Println(err)
	}
	if err := xc.conn.Close(); err != nil {
		log.Println(err)
	}

	log.Println("xdebugClient closed")

	return nil
}

var xdebug *xdebugClient

func (h *BufPane) PhpCmd(args []string) {
	var t string

	if len(args) > 0 {
		t = args[0]
	}

	if xdebug == nil && (t == "s" || t == "n" || t == "c") {
		t = "start"
	}

	if xdebug == nil && t != "start" {
		InfoBar.Error("phpdebug is not started")
		return
	}

	switch t {
	case "start":
		if xdebug != nil {
			InfoBar.Error("phpdebug already started")
			return
		}
		xdebug = &xdebugClient{pane: h}
		if err := xdebug.Start(); err != nil {
			log.Println(err)
			InfoBar.Error(err)
			return
		}
		xdebug.step("step_into")
	case "stop":
		xdebug.Close()
		xdebug = nil
	case "so":
		xdebug.step("step_out")
	case "s":
		xdebug.step("step_into")
	case "n":
		xdebug.step("step_over")
	case "c":
		s := fmt.Sprintf("run -i %d\x00", xdebug.transID)
		resp, err := xdebug.send(s)
		if err != nil {
			InfoBar.Error(err)
			break
		}

		xdebug.processResponse(resp)
	case "b":
		s := fmt.Sprintf("breakpoint_set -i %d -t line -f %s -n %d\x00", xdebug.transID, xdebug.currFile, xdebug.currLine)
		xdebug.transID++
		resp, err := xdebug.send(s)
		if err != nil {
			InfoBar.Error(err)
			break
		}
		xdebug.processResponse(resp)
		s = fmt.Sprintf("breakpoint_list -i %d\x00", xdebug.transID)
		xdebug.transID++
		resp, err = xdebug.send(s)
		if err != nil {
			InfoBar.Error(err)
			break
		}
		xdebug.processResponse(resp)
	case "bl":
		s := fmt.Sprintf("breakpoint_list -i %d\x00", xdebug.transID)
		xdebug.transID++
		resp, err := xdebug.send(s)
		if err != nil {
			InfoBar.Error(err)
			break
		}
		xdebug.processResponse(resp)
	case "e":
		args := strings.Join(args[1:], " ")
		eargs := base64.StdEncoding.EncodeToString([]byte("var_export(" + args + ", TRUE)"))
		s := fmt.Sprintf("eval -i %d -- %s\x00", xdebug.transID, eargs)
		resp, err := xdebug.send(s)
		if err != nil {
			InfoBar.Error(err)
			break
		}
		var b bytes.Buffer
		dumpProperties(&b, resp.Properties, 0)
		log.Println("=============== eval\n\x1b[31m", args, "\x1b[0m\n", b.String())
	default:
		s := fmt.Sprintf("%s -i %d\x00", t, xdebug.transID)
		resp, err := xdebug.send(s)
		if err != nil {
			InfoBar.Error(err)
			break
		}
		xdebug.processResponse(resp)
	}
}

func (xc *xdebugClient) makeRequest() {
	cmd := exec.Command("sh", "-c", xc.InitCommand)
	b, err := cmd.CombinedOutput()
	if err != nil {
		log.Println(err, xc.InitCommand, "out:", string(b))
		InfoBar.Error(err)
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

func (xc *xdebugClient) dumpStack(w io.Writer, resp Response) {
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

func (xc *xdebugClient) readInitFile() error {
	f, err := os.Open("init.yaml")
	if err != nil && os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("init.yaml open. error: %w", err)
	}

	defer f.Close()

	err = yaml.NewDecoder(f).Decode(xdebug)
	if err != nil {
		return fmt.Errorf("init.yaml decode. error: %w", err)
	}

	log.Printf("xdebug: %+v", xdebug)

	return nil
}

func (xc *xdebugClient) processParameters() error {

	for _, b := range xdebug.Breakpoints {
		cc := strings.Split(b, " ")
		s := fmt.Sprintf("breakpoint_set -i %d -t line -f %s -n %s\x00", xc.transID, cc[0], cc[1])
		xc.transID++
		resp, err := xdebug.send(s)
		if err != nil {
			return fmt.Errorf("set breakpoint. error: %w", err)
		}
		xdebug.processResponse(resp)
	}

	return nil
}
