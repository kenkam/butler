package butler

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	RequestGet  = "GET"
	RequestHead = "HEAD"
)

type Request struct {
	Scheme  string
	Host    string
	Method  string
	Path    string
	Headers map[string][]string
	Body    []byte
}

func ParseRequest(conn io.Reader, scheme string) (*Request, error) {
	scanner := bufio.NewScanner(conn)
	headers := make(map[string][]string)
	request := &Request{Headers: headers, Scheme: scheme}
	headersRead := false

	scanLines := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}

		// We've read the headers, so just read all the way to the end for the body
		if headersRead {
			i := len(data)
			return i, dropCR(data[0:i]), nil
		}

		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			// If this is a blank new line
			if len(dropCR(data[0:i])) == 0 {
				// and it's a GET or HEAD request, we stop parsing the request body
				if request.Method == RequestGet || request.Method == RequestHead {
					return 0, nil, bufio.ErrFinalToken
				}
				headersRead = true
			}

			return i + 1, dropCR(data[0:i]), nil
		}

		// If we're at EOF, we have a final, non-terminated line. Return it.
		if atEOF {
			return len(data), dropCR(data), nil
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

	// Parse headers
	for scanner.Scan() && !headersRead {
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

		if hName == HeaderHost {
			request.Host = hValue[0]
		}
	}

	if request.Method == RequestGet || request.Method == RequestHead {
		return request, nil
	}

	hLength, ok := request.Headers[HeaderContentLength]
	if !ok || len(hLength) == 0 {
		return request, nil
	}

	remaining, err := strconv.Atoi(hLength[0])
	// TODO this should return bad request
	if err != nil {
		return nil, err
	}

	// Parse request body
	for remaining > 0 && scanner.Scan() {
		b := scanner.Bytes()
		remaining -= len(b)
		request.Body = append(request.Body, b...)
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
