package butler

import (
	"fmt"
	"strings"
)

type Request struct {
	method string
	path   string
}

func Parse(request string) *Request {
	lines := strings.Split(request, "\r\n")
	controlData := lines[0]
	cdTokens := strings.Fields(controlData)

	// TODO Parse HTTP Version
	method, path := cdTokens[0], cdTokens[1]
	return &Request{method, path}
}

func (r Request) String() string {
	return fmt.Sprintf("%s %s", r.method, r.path)
}
