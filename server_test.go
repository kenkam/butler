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
	log.SetOutput(io.Discard)

	s, err := NewServer(&Config{
		Host:         "localhost",
		Listen:       0,
		ListenTLS:    -1,
		DocumentRoot: "./testdata",
	})
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		s.Listen()
		defer s.Close()
	}()

	<-s.httpListener.readyCh

	req, _ := http.NewRequest("GET", "http://"+s.httpListener.listener.Addr().String()+"/index.html", nil)
	req.Header.Add("Accept-Encoding", "gzip")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile("./testdata/index.html")
	if r.ContentLength >= int64(len(b)) {
		t.Fatal("compressed response should be less than the original length")
	}
	if r.Header["Content-Encoding"][0] != "gzip" {
		t.Fatal("returned response does not have Content-Encoding: gzip")
	}
}

func TestServerClosesConnection(t *testing.T) {
	log.SetOutput(io.Discard)

	s, _ := NewServer(&Config{
		Host:         "localhost",
		Listen:       0,
		ListenTLS:    -1,
		DocumentRoot: "./testdata",
	})
	go func() {
		s.Listen()
		defer s.Close()
	}()

	<-s.httpListener.readyCh

	addr, _ := net.ResolveTCPAddr("tcp", s.httpListener.listener.Addr().String())
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
		t.Fatal("connection was not closed")
	}
}

func TestBackend(t *testing.T) {
	cases := []struct {
		p string
		s int
		n string
	}{
		{
			p: "/",
			s: http.StatusOK,
			n: "PathMatch",
		},
		{
			p: "/random",
			s: http.StatusNotFound,
			n: "PathMismatch",
		},
	}

	log.SetOutput(io.Discard)

	for _, c := range cases {
		t.Run(c.n, func(t *testing.T) {
			backend, _ := NewServer(&Config{
				Host:         "localhost",
				Listen:       0,
				ListenTLS:    -1,
				DocumentRoot: "./testdata",
			})

			go backend.Listen()
			<-backend.httpListener.readyCh

			proxy, _ := NewServer(&Config{
				Listen:    0,
				ListenTLS: -1,
				Backends: []Backend{
					{
						Addr: backend.httpListener.listener.Addr().String(),
						Path: "/",
					},
				},
			})
			go proxy.Listen()
			defer proxy.Close()
			<-proxy.httpListener.readyCh

			resp, _ := http.Get("http://" + proxy.httpListener.listener.Addr().String() + c.p)
			if resp.StatusCode != c.s {
				t.Fatalf("expected status code %v, got %v", c.s, resp.StatusCode)
			}
		})
	}
}

func TestMissingBackend(t *testing.T) {
	log.SetOutput(io.Discard)

	proxy, _ := NewServer(&Config{
		Listen:    0,
		ListenTLS: -1,
		Backends: []Backend{
			{
				Addr: "localhost:42069",
				Path: "/",
			},
		},
	})
	go proxy.Listen()
	defer proxy.Close()
	<-proxy.httpListener.readyCh

	resp, _ := http.Get("http://" + proxy.httpListener.listener.Addr().String())
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, but got %v", resp.StatusCode)
	}
}

func TestConfigValidation(t *testing.T) {
	cases := []struct {
		c *Config
		e bool
		n string
	}{
		{
			c: &Config{
				Listen:    -1,
				ListenTLS: -1,
			},
			e: true,
			n: "ValidPortsSet",
		},
		{
			c: &Config{
				Listen:       -1,
				RedirectHTTP: true,
			},
			e: true,
			n: "ListenSetIfRedirectHTTPSAlsoSet",
		},
		{
			c: &Config{
				ListenTLS:       443,
				CertificateFile: "./testdata/butler.crt",
			},
			e: true,
			n: "ListenTLSSetButCertificatesNotSet",
		},
	}

	for _, c := range cases {
		t.Run(c.n, func(t *testing.T) {
			_, err := NewServer(c.c)
			if (err != nil) != c.e {
				t.Fatalf("expected error %v but got %v", c.e, err)
			}
		})
	}
}
