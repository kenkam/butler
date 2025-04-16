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

	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Host               string    `yaml:"Host"`
	Listen             int       `yaml:"Listen"`
	ListenTLS          int       `yaml:"ListenTLS"`
	RedirectHTTP       bool      `yaml:"RedirectHTTP"`
	Backends           []Backend `yaml:"Backends"`
	CertificateFile    string    `yaml:"CertificateFile"`
	CertificateKeyFile string    `yaml:"CertificateKeyFile"`
	DocumentRoot       string    `yaml:"DocumentRoot"`
}

type Server struct {
	Host         string
	DocumentRoot string

	certificate   tls.Certificate
	httpListener  *listener
	httpsListener *listener
}

type listener struct {
	port     int
	listener net.Listener
	readyCh  chan bool
	handlers []handler
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
type handler interface {
	Handle(context *Context) (bool, error)
}

type RedirectHTTPHandler struct {
	config *Config
}

func (r RedirectHTTPHandler) Handle(c *Context) (bool, error) {
	if c.Request.Scheme == "http" {
		host := strings.Split(c.Request.Host, ":")[0] + ":" + strconv.Itoa(r.config.ListenTLS)
		c.Response = MovedPermanently("https://" + host + c.Request.Path)

		slog.Debug("redirecting to https for " + c.Conn.RemoteAddr().String())
		return true, nil
	}

	return false, nil
}

type BackendHandler struct {
	b *Backend
}

func (b BackendHandler) Handle(c *Context) (bool, error) {
	if strings.HasPrefix(c.Request.Path, b.b.Path) {

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
			c.Response = BadGateway()
			return true, nil
		}

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return false, err
		}

		c.Response = StatusCode(resp.StatusCode, b)
		for k, vs := range resp.Header {
			c.Response.Headers[k] = vs
		}

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
	host := c.Host
	if host == "" {
		host = "0.0.0.0"
	}

	s := &Server{
		Host:         host,
		DocumentRoot: c.DocumentRoot,
	}

	if c.Listen < 0 && c.ListenTLS < 0 {
		return nil, errors.New("either Listen or ListenTLS must be set")
	}

	if c.ListenTLS > -1 && (c.CertificateFile == "" || c.CertificateKeyFile == "") {
		return nil, errors.New("ListenTLS and both CertificateFile and CertificateKeyFile must be set")
	}

	if c.ListenTLS > -1 {
		tl := listener{port: c.ListenTLS, readyCh: make(chan bool, 1), handlers: make([]handler, 0)}
		cert, err := tls.LoadX509KeyPair(c.CertificateFile, c.CertificateKeyFile)
		if err != nil {
			return nil, err
		}
		s.certificate = cert

		for _, v := range c.Backends {
			tl.handlers = append(tl.handlers, BackendHandler{&v})
		}

		if c.DocumentRoot != "" {
			tl.handlers = append(tl.handlers, ServeDocumentRootHandler{c.DocumentRoot})
		}

		s.httpsListener = &tl
	}

	if c.Listen > -1 {
		tl := listener{port: c.Listen, readyCh: make(chan bool, 1), handlers: make([]handler, 0)}

		if c.RedirectHTTP {
			tl.handlers = append(tl.handlers, RedirectHTTPHandler{c})
		}

		for _, v := range c.Backends {
			tl.handlers = append(tl.handlers, BackendHandler{&v})
		}

		if c.DocumentRoot != "" {
			tl.handlers = append(tl.handlers, ServeDocumentRootHandler{c.DocumentRoot})
		}

		s.httpListener = &tl
	}

	return s, nil
}

func (server *Server) Listen() error {
	g := new(errgroup.Group)
	if server.httpListener != nil {
		g.Go(func() error {
			return server.listen(server.httpListener, func(addr string) (net.Listener, error) {
				return net.Listen("tcp", addr)
			}, "http")
		})
	}

	if server.httpsListener != nil {
		g.Go(func() error {
			if server.httpListener != nil {
				<-server.httpListener.readyCh
			}
			return server.listen(server.httpsListener, func(addr string) (net.Listener, error) {
				return tls.Listen("tcp", addr, &tls.Config{
					Certificates: []tls.Certificate{server.certificate},
				})
			}, "https")
		})
	}

	return g.Wait()
}

func (server *Server) listen(listener *listener, createListener func(address string) (net.Listener, error),
	scheme string) error {
	address := fmt.Sprintf("%s:%d", server.Host, listener.port)
	listen, err := createListener(address)
	if err != nil {
		return err
	}

	listener.listener = listen
	slog.Info("listening on " + scheme + "://" + address)
	listener.readyCh <- true

	for {
		conn, err := listen.Accept()
		if err != nil {
			return err
		}

		slog.Debug("accepted connection from " + conn.RemoteAddr().String())
		go listener.listenAndHandleRequests(conn, "http")
	}
}

func (server Server) Close() error {
	if server.httpListener != nil && server.httpListener.listener != nil {
		server.httpListener.listener.Close()
	}

	if server.httpsListener != nil && server.httpsListener.listener != nil {
		server.httpsListener.listener.Close()
	}

	return nil
}

func (listener listener) listenAndHandleRequests(conn net.Conn, scheme string) {
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

		err = listener.handleRequest(c)
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

func (listener listener) handleRequest(c *Context) error {
	for _, h := range listener.handlers {
		skip, err := h.Handle(c)
		if err != nil {
			return err
		}

		if skip {
			break
		}
	}

	// No handlers processed the request
	if c.Response == nil {
		c.Response = NotFound()
	}

	gzip := false
	hEncoding, hasEncodingHeader := c.Request.Headers[HeaderAcceptEncoding]
	responseGzipped := slices.Contains(c.Response.Headers[HeaderAcceptEncoding], "gzip")
	if hasEncodingHeader && !responseGzipped && c.Response.Content != nil {
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
