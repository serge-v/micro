package xdebug

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

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

	if err := l.SetDeadline(time.Now().Add(time.Second * 5)); err != nil {
		return nil, fmt.Errorf("set accept deadline. error: %w", err)
	}

	conn, err := l.AcceptTCP()
	if err != nil {
		return nil, fmt.Errorf("accept. error: %w", err)
	}

	b, err := readBlock(conn)
	if err != nil {
		return nil, fmt.Errorf("read block. error: %w", err)
	}

	if _, err := unmarshalCommand(b); err != nil {
		return nil, err
	}

	log.Println("accepted")

	return conn, nil
}

func initProxy(addr, s string) {
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
