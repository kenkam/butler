package butler

import (
	"strings"
	"testing"
)

func TestParseRequest(t *testing.T) {
	conn := strings.NewReader(`GET / HTTP/1.1
Connection: close
Accept-Encoding: gzip, deflate, br
`)

	request, err := ParseRequest(conn)
	if err != nil {
		t.Error(err)
	}

	if request.Headers[HeaderConnection][0] != "close" {
		t.Fatal("Connection: close is not present headers")
	}

	if request.Headers[HeaderAcceptEncoding][0] != "gzip, deflate, br" {
		t.Fatal("Connection: close is not present headers")
	}
}

func TestHeadRequestIgnoresBody(t *testing.T) {
	conn := strings.NewReader(`HEAD / HTTP/1.1
Connection: close
Accept-Encoding: gzip, deflate, br

Ignored body
`)

	r, _ := ParseRequest(conn)

	if len(r.Body) > 0 {
		t.Fatal("request should not have read body")
	}
}
