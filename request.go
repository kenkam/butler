package butler

import (
	"fmt"
	"strings"
)

type Request struct {
	method  string
	path    string
	headers map[string][]string
}

func ParseRequest(request string) *Request {
	lines := strings.Split(request, "\r\n")
	controlData := lines[0]
	cdTokens := strings.Fields(controlData)

	// TODO Parse HTTP Version
	method, path := cdTokens[0], cdTokens[1]
	headers := make(map[string][]string)

	i := 1
	for i < len(lines) && lines[i] != "" {
		hTokens := strings.Split(lines[i], ":")
		if len(hTokens) == 0 {
			continue
		}

		hName := hTokens[0]
		hValue, ok := headers[hName]
		if !ok {
			hValue = make([]string, 0)
		}

		if len(hTokens) > 1 {
			hValue = append(hValue, hTokens[1:]...)
		}

		headers[hName] = hValue
		i += 1
	}

	return &Request{method, path, headers}
}

func (r Request) String() string {
	return fmt.Sprintf("%s %s", r.method, r.path)
}
