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
	"strconv"
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
	listener      net.Listener
	ListenCh      chan bool
	ListenTLSCh   chan bool
	handlers      []Handler
}

type Backend struct {
	Addr string `yaml:"Addr"`
	Path string `yaml:"Path"`
}

type Context struct {
	Conn     net.Conn
	Request  *Request
	Response *Response
}

// If return value is true, should skip all other handlers
type Handler interface {
	Handle(context *Context) (bool, error)
}

type RedirectHTTPHandler struct {
	config *Config
}

func (r RedirectHTTPHandler) Handle(c *Context) (bool, error) {
	if c.Request.Scheme == "http" {
		resp := StatusCode(http.StatusMovedPermanently, nil)
		host := strings.Split(c.Request.Host, ":")[0] + ":" + strconv.Itoa(r.config.ListenTLS)
		resp.Headers[HeaderLocation] = []string{"https://" + host + c.Request.Path}
		resp.Content = []byte(`<HTML><HEAD><meta http-equiv="content-type" content="text/html;charset=utf-8">
<TITLE>301 Moved</TITLE></HEAD><BODY>
<H1>301 Moved</H1>
The document has moved
</BODY></HTML>
`)
		c.Response = resp

		slog.Debug("redirecting to https for " + c.Conn.RemoteAddr().String())
		return true, nil
	}

	return false, nil
}

type BackendHandler struct {
	b *Backend
}

func (b BackendHandler) Handle(c *Context) (bool, error) {
	if strings.HasPrefix(b.b.Path, c.Request.Path) {

		url := "http://" + b.b.Addr + c.Request.Path
		r, err := http.NewRequest(c.Request.Method, url, strings.NewReader(""))
		if err != nil {
			return false, err
		}

		for k, vs := range c.Request.Headers {
			for _, v := range vs {
				r.Header.Add(k, v)
			}
		}

		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			return false, err
		}

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return false, err
		}

		c.Response = StatusCode(resp.StatusCode, b)
		return true, nil
	}

	return false, nil
}

type ServeDocumentRootHandler struct {
	docRoot string
}

func (s ServeDocumentRootHandler) Handle(c *Context) (bool, error) {
	if c.Request.Path == "/" {
		c.Request.Path = "/index.html"
	}

	path := path.Join(s.docRoot, c.Request.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		_, isPathError := err.(*os.PathError)
		if isPathError {
			c.Response = NotFound()
		}
	} else {
		c.Response = Ok(data)
	}

	return true, nil
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
		ListenCh:      make(chan bool, 1),
		ListenTLSCh:   make(chan bool, 1),
		handlers:      make([]Handler, 0),
	}

	if c.CertificateFile != "" || c.CertificateKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.CertificateFile, c.CertificateKeyFile)
		if err != nil {
			return nil, err
		}
		s.Certificate = cert
	}

	if c.RedirectHTTP {
		s.handlers = append(s.handlers, RedirectHTTPHandler{c})
	}

	for _, v := range c.Backends {
		s.handlers = append(s.handlers, BackendHandler{&v})
	}

	if c.DocumentRoot != "" {
		s.handlers = append(s.handlers, ServeDocumentRootHandler{c.DocumentRoot})
	}

	return s, nil
}

func (server *Server) Listen() error {
	address := fmt.Sprintf("%s:%d", server.Hostname, server.ListenPort)
	listen, err := net.Listen("tcp", address)
	server.listener = listen
	if err != nil {
		return err
	}

	slog.Info("listening on http://" + address)
	server.ListenCh <- true

	for {
		conn, err := listen.Accept()
		if err != nil {
			return err
		}

		slog.Debug("accepted connection from " + conn.RemoteAddr().String())
		go server.listenAndHandleRequests(conn, "http")
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

	slog.Info("listening on https://" + address)
	server.ListenTLSCh <- true

	for {
		conn, err := listen.Accept()
		if err != nil {
			return err
		}

		slog.Debug("accepted connection from " + conn.RemoteAddr().String())
		go server.listenAndHandleRequests(conn, "https")
	}
}

func (server Server) Close() error {
	return server.listener.Close()
}

func (server Server) listenAndHandleRequests(conn net.Conn, scheme string) {
	for {
		c := &Context{Conn: conn}

		r, err := ParseRequest(conn, scheme)
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
	for _, h := range server.handlers {
		skip, err := h.Handle(c)
		if err != nil {
			return err
		}

		if skip {
			break
		}
	}

	gzip := false
	hEncoding, hasEncodingHeader := c.Request.Headers[HeaderAcceptEncoding]
	responseGzipped := slices.Contains(c.Response.Headers[HeaderAcceptEncoding], "gzip")
	if hasEncodingHeader && !responseGzipped {
		v := strings.Split(hEncoding[0], ", ")
		if slices.Contains(v, "gzip") {
			gzip = true
		}
	}

	c.Response.Headers["Server"] = []string{"butler/0.1"}

	written, err := c.Conn.Write(c.Response.Bytes(gzip, c.Request.Method == RequestHead))
	if err != nil {
		return errors.New("failed writing response")
	}

	slog.Info(fmt.Sprintf("%s %s (%d bytes)", c.Conn.RemoteAddr(), c.Request, written))
	return nil
}
