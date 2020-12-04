package xdebug

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"log"

	"golang.org/x/net/html/charset"
)

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

type proxyResponse struct {
	XMLName xml.Name
	Success int    `xml:"success,attr"`
	Idekey  string `xml:"idekey,attr"`
	Address string `xml:"address,attr"`
	Port    int    `xml:"port,attr"`
	Ssl     bool   `xml:"ssl,attr"`
}
