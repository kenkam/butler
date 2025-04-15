package butler

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
)

type Server struct {
	Hostname     string
	Port         int
	DocumentRoot string
	Backends     []Backend
	listener     net.Listener
	listenCh     chan bool
	listenTLSCh  chan bool
}

type Backend struct {
	Addr string
	Path string
}

type Context struct {
	Conn     net.Conn
	Request  *Request
	Response *Response
}

func NewServer(host string, port int, docRoot string) *Server {
	return &Server{host, port, docRoot, make([]Backend, 0), nil, make(chan bool, 1), make(chan bool, 1)}
}

func (server *Server) AddBackend(addr string, path string) error {
	if path == "" {
		path = "/"
	}

	for _, b := range server.Backends {
		if b.Addr == addr {
			return fmt.Errorf("backend %s already exists", addr)
		}
	}

	server.Backends = append(server.Backends, Backend{addr, path})
	return nil
}

func (server *Server) Listen() error {
	address := fmt.Sprintf("%s:%d", server.Hostname, server.Port)
	listen, err := net.Listen("tcp", address)
	server.listener = listen
	if err != nil {
		return err
	}

	slog.Info("butler listening on " + address)
	server.listenCh <- true

	for {
		conn, err := listen.Accept()
		if err != nil {
			return err
		}

		slog.Debug("accepted connection from " + conn.RemoteAddr().String())
		go server.listenAndHandleRequests(conn)
	}
}

func (server *Server) ListenTLS() error {
	address := fmt.Sprintf("%s:%d", server.Hostname, server.Port)
	certs, err := tls.LoadX509KeyPair("/home/kenneth/Certs/butler.crt", "/home/kenneth/Certs/butler.key")
	if err != nil {
		return err
	}

	listen, err := tls.Listen("tcp", address, &tls.Config{
		Certificates: []tls.Certificate{certs},
	})

	if err != nil {
		return err
	}

	slog.Info("butler listening on " + address)
	server.listenTLSCh <- true

	for {
		conn, err := listen.Accept()
		if err != nil {
			return err
		}

		slog.Debug("accepted connection from " + conn.RemoteAddr().String())
		go server.listenAndHandleRequests(conn)
	}
}

func (server Server) Close() error {
	return server.listener.Close()
}

func (server Server) listenAndHandleRequests(conn net.Conn) {
	for {
		c := &Context{Conn: conn}

		r, err := ParseRequest(conn)
		if err != nil {
			slog.Debug(fmt.Sprintf("error reading from %s: %s, closing connection...", conn.RemoteAddr(), err.Error()))
			c.Conn.Close()
			return
		}

		c.Request = r
		slog.Debug(fmt.Sprintf("%s %s", conn.RemoteAddr(), c.Request))

		err = server.handleRequest(c)
		if err != nil {
			slog.Error(fmt.Sprintf("failed handling request %s for %s: %s", c.Request, c.Conn.RemoteAddr(), err))
			c.Conn.Close()
			return
		}

		cHeaders := c.Request.Headers[HeaderConnection]
		if len(cHeaders) > 0 {
			connection := cHeaders[0]
			if strings.EqualFold(connection, "close") {
				slog.Debug(fmt.Sprintf("no keep-alive requested, closing connection for %s", c.Conn.RemoteAddr()))
				c.Conn.Close()
				return
			}
		}
	}
}

func (server Server) handleRequest(c *Context) error {
	var response *Response

	if backend, ok := server.GetBackend(c); ok {
		url := "http://" + backend.Addr + c.Request.Path
		r, err := http.NewRequest(c.Request.Method, url, strings.NewReader(""))
		if err != nil {
			return err
		}

		for k, vs := range c.Request.Headers {
			for _, v := range vs {
				r.Header.Add(k, v)
			}
		}

		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			return err
		}

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		response = StatusCode(resp.StatusCode, b)
	} else {
		response = server.serveFromDocumentRoot(c)
	}

	gzip := false
	hEncoding, hasEncodingHeader := c.Request.Headers[HeaderAcceptEncoding]
	responseGzipped := slices.Contains(response.Headers[HeaderAcceptEncoding], "gzip")
	if hasEncodingHeader && !responseGzipped {
		v := strings.Split(hEncoding[0], ", ")
		if slices.Contains(v, "gzip") {
			gzip = true
		}
	}

	response.Headers["Server"] = []string{"butler/0.1"}

	written, err := c.Conn.Write(response.Bytes(gzip, c.Request.Method == RequestHead))
	if err != nil {
		return errors.New("failed writing response")
	}

	c.Response = response
	slog.Info(fmt.Sprintf("%s %s (%d bytes)", c.Conn.RemoteAddr(), c.Request, written))
	return nil
}

func (server *Server) serveFromDocumentRoot(c *Context) *Response {
	var r *Response

	if c.Request.Path == "/" {
		c.Request.Path = "/index.html"
	}

	path := path.Join(server.DocumentRoot, c.Request.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		_, isPathError := err.(*os.PathError)
		if isPathError {
			r = NotFound()
		}
	} else {
		r = Ok(data)
	}

	return r
}

func (server *Server) GetBackend(c *Context) (*Backend, bool) {
	for _, b := range server.Backends {
		if strings.HasPrefix(b.Path, c.Request.Path) {
			return &b, true
		}
	}

	return nil, false
}
