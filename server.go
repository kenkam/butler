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

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen             int       `yaml:"Listen"`
	ListenTLS          int       `yaml:"ListenTLS"`
	RedirectHTTP       bool      `yaml:"RedirectHTTP"`
	Backends           []Backend `yaml:"Backends"`
	CertificateFile    string    `yaml:"CertificateFile"`
	CertificateKeyFile string    `yaml:"CertificateKeyFile"`
	DocumentRoot       string    `yaml:"DocumentRoot"`
}

type Server struct {
	Hostname      string
	ListenPort    int
	ListenTLSPort int
	Certificate   tls.Certificate
	DocumentRoot  string
	backends      []Backend
	listener      net.Listener
	listenCh      chan bool
	listenTLSCh   chan bool
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

func NewServerYaml(yamlFile string) (*Server, error) {
	f, err := os.Open(yamlFile)
	if err != nil {
		return nil, err
	}

	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	c := &Config{}
	err = yaml.Unmarshal(b, c)
	if err != nil {
		return nil, err
	}

	return NewServer(c)
}

func NewServer(c *Config) (*Server, error) {
	s := &Server{
		Hostname:      "0.0.0.0",
		ListenPort:    c.Listen,
		ListenTLSPort: c.ListenTLS,
		DocumentRoot:  c.DocumentRoot,
		listenCh:      make(chan bool, 1),
		listenTLSCh:   make(chan bool, 1),
		backends:      make([]Backend, 0),
	}

	if c.CertificateFile != "" || c.CertificateKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.CertificateFile, c.CertificateKeyFile)
		if err != nil {
			return nil, err
		}
		s.Certificate = cert
	}

	for _, v := range c.Backends {
		s.AddBackend(v.Addr, v.Path)
	}

	return s, nil
}

func (server *Server) AddBackend(addr string, path string) error {
	if path == "" {
		path = "/"
	}

	for _, b := range server.backends {
		if b.Addr == addr {
			return fmt.Errorf("backend %s already exists", addr)
		}
	}

	server.backends = append(server.backends, Backend{addr, path})
	return nil
}

func (server *Server) Listen() error {
	address := fmt.Sprintf("%s:%d", server.Hostname, server.ListenPort)
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
	address := fmt.Sprintf("%s:%d", server.Hostname, server.ListenTLSPort)

	listen, err := tls.Listen("tcp", address, &tls.Config{
		Certificates: []tls.Certificate{server.Certificate},
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
	for _, b := range server.backends {
		if strings.HasPrefix(b.Path, c.Request.Path) {
			return &b, true
		}
	}

	return nil, false
}
