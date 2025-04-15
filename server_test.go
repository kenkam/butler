package butler

import (
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestServerSupportsGzip(t *testing.T) {
	// Mute server logs
	log.SetOutput(io.Discard)

	s, _ := NewServer(&Config{
		Listen:       7777,
		DocumentRoot: "./testdata",
	})
	go func() {
		s.Listen()
		defer s.Close()
	}()

	<-s.listenCh

	req, _ := http.NewRequest("GET", "http://localhost:7777/index.html", nil)
	req.Header.Add("Accept-Encoding", "gzip")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Error(err)
	}

	b, _ := os.ReadFile("./testdata/index.html")
	if r.ContentLength >= int64(len(b)) {
		t.Error("compressed response should be less than the original length")
	}
	if r.Header["Content-Encoding"][0] != "gzip" {
		t.Error("returned response does not have Content-Encoding: gzip")
	}

	s.Close()
}

func TestServerClosesConnection(t *testing.T) {
	// Mute server logs
	log.SetOutput(io.Discard)

	s, _ := NewServer(&Config{
		Listen:       7777,
		DocumentRoot: "./testdata",
	})
	go func() {
		s.Listen()
		defer s.Close()
	}()

	<-s.listenCh

	addr, _ := net.ResolveTCPAddr("tcp", "localhost:7777")
	conn, _ := net.DialTCP("tcp", nil, addr)

	payload := `GET /index.html HTTP/1.1
Connection: close

`
	conn.Write([]byte(payload))
	closed := make(chan bool)

	go func() {
		b := make([]byte, 1024)
		for {
			// Read blocks if the connection is not closed remotely
			// If the connection is closed then this will busy spin, so
			// we check if n == 0 and quit
			n, _ := conn.Read(b)

			if n == 0 {
				closed <- true
				return
			}
		}
	}()

	select {
	case <-closed:
		return
	case <-time.After(200 * time.Millisecond):
		t.Error("connection was not closed")
	}
}
