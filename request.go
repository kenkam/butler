package butler

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
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

func ParseContext(c *Context) (*Context, error) {
	headers := make(map[string][]string)
	c.Request = &Request{Headers: headers}

	scanLines := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			// We have a full newline-terminated line.
			if c.Request.Method == RequestGet || c.Request.Method == RequestHead {
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
		if c.Request.Method == RequestGet {
			if string(token) == "" {
				return 0, nil, bufio.ErrFinalToken
			}
		}
		// Request more data.
		return 0, nil, nil
	}

	c.Scanner.Split(scanLines)

	if !c.Scanner.Scan() {
		return c, errors.New("no data received")
	}

	if c.Scanner.Err() != nil {
		return c, c.Scanner.Err()
	}
	controlData := c.Scanner.Text()
	cdTokens := strings.Fields(controlData)

	// TODO Parse HTTP Version
	c.Request.Method, c.Request.Path = cdTokens[0], cdTokens[1]

	for c.Scanner.Scan() {
		line := c.Scanner.Text()

		hTokens := strings.Split(line, ":")
		if len(hTokens) == 0 {
			continue
		}

		hName := hTokens[0]
		hValue, ok := c.Request.Headers[hName]
		if !ok {
			hValue = make([]string, 0)
		}

		t := strings.TrimSpace(strings.Join(hTokens[1:], ":"))
		hValue = append(hValue, t)

		headers[hName] = hValue
	}

	return c, nil
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
