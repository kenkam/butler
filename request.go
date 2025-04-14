package butler

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"strings"
)

const (
	RequestGet  = "GET"
	RequestHead = "HEAD"
)

type Request struct {
	Method  string
	Path    string
	Headers map[string][]string
}

func ParseRequest(conn net.Conn) (*Request, error) {
	scanner := bufio.NewScanner(conn)
	headers := make(map[string][]string)
	request := &Request{Headers: headers}

	scanLines := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			// We have a full newline-terminated line.
			if request.Method == RequestGet || request.Method == RequestHead {
				if i == 1 {
					return 0, nil, bufio.ErrFinalToken
				}
			}

			return i + 1, dropCR(data[0:i]), nil
		}
		// If we're at EOF, we have a final, non-terminated line. Return it.
		if atEOF {
			return len(data), dropCR(data), nil
		}
		// If the request method is GET, ignore request body
		if request.Method == RequestGet {
			if string(token) == "" {
				return 0, nil, bufio.ErrFinalToken
			}
		}
		// Request more data.
		return 0, nil, nil
	}

	scanner.Split(scanLines)

	if !scanner.Scan() {
		return nil, errors.New("no data received")
	}

	if scanner.Err() != nil {
		return nil, scanner.Err()
	}
	controlData := scanner.Text()
	cdTokens := strings.Fields(controlData)

	// TODO Parse HTTP Version
	request.Method, request.Path = cdTokens[0], cdTokens[1]

	for scanner.Scan() {
		line := scanner.Text()

		hTokens := strings.Split(line, ":")
		if len(hTokens) == 0 {
			continue
		}

		hName := hTokens[0]
		hValue, ok := request.Headers[hName]
		if !ok {
			hValue = make([]string, 0)
		}

		t := strings.TrimSpace(strings.Join(hTokens[1:], ":"))
		hValue = append(hValue, t)

		headers[hName] = hValue
	}

	return request, nil
}

func (r Request) String() string {
	return fmt.Sprintf("%s %s", r.Method, r.Path)
}

func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[0 : len(data)-1]
	}
	return data
}
