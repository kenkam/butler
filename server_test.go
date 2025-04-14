package butler

import (
	"io"
	"log"
	"net/http"
	"os"
	"testing"
)

func TestServerSupportsGzip(t *testing.T) {
	// Mute server logs
	log.SetOutput(io.Discard)

	s := NewServer("localhost", 7777, "./testdata")
	errCh := make(chan error)
	getCh := make(chan error)
	doneCh := make(chan bool)
	go func() {
		err := s.Listen()
		errCh <- err
	}()

	go func() {
		<-s.ListenCh

		req, _ := http.NewRequest("GET", "http://localhost:7777/index.html", nil)
		req.Header.Add("Accept-Encoding", "gzip")
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			getCh <- err
			return
		}

		b, _ := os.ReadFile("./testdata/index.html")
		if r.ContentLength >= int64(len(b)) {
			t.Error("compressed response should be less than the original length")
		}
		if r.Header["Content-Encoding"][0] != "gzip" {
			t.Error("returned response does not have Content-Encoding: gzip")
		}

		doneCh <- true
	}()

	for {
		select {
		case err := <-errCh:
			t.Error(err)
			return
		case err := <-getCh:
			t.Error(err)
			return
		case <-doneCh:
			return
		}
	}
}
