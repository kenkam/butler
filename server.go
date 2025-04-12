package butler

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path"
	"slices"
	"strings"
)

type Server struct {
	Host         string
	Port         int
	DocumentRoot string
}

type Context struct {
	Scanner  *bufio.Scanner
	Conn     net.Conn
	Request  *Request
	Response Response
}

func (server Server) Listen() error {
	address := fmt.Sprintf("%s:%d", server.Host, server.Port)
	listen, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	slog.Info("butler listening on " + address)

	for {
		conn, err := listen.Accept()
		if err != nil {
			return err
		}

		slog.Debug("accepted connection from " + conn.RemoteAddr().String())
		go server.listenAndHandleRequests(conn)
	}
}

func (server Server) listenAndHandleRequests(conn net.Conn) {
	for {
		c := &Context{}

		scanner := bufio.NewScanner(conn)

		c.Scanner = scanner
		c.Conn = conn

		c, err := ParseContext(c)
		if err != nil {
			slog.Debug(fmt.Sprintf("error reading from %s, closing connection...", conn.RemoteAddr()))
			c.Conn.Close()
			return
		}

		slog.Debug(fmt.Sprintf("%s %s", conn.RemoteAddr(), c.Request))

		server.handle(c)
	}
}

func (server Server) handle(c *Context) {
	var response *Response
	if c.Request.Path == "/" {
		c.Request.Path = "/index.html"
	}

	path := path.Join(server.DocumentRoot, c.Request.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		_, isPathError := err.(*os.PathError)
		if isPathError {
			response = NotFound()
		}
	} else {
		response = Ok(data)
	}

	gzip := false
	hEncoding, ok := c.Request.Headers[HeaderAcceptEncoding]
	if ok {
		v := strings.Split(hEncoding[0], ", ")
		if slices.Contains(v, "gzip") {
			gzip = true
		}
	}

	written, err := c.Conn.Write(response.Bytes(gzip, c.Request.Method == RequestHead))
	if err != nil {
		slog.Debug(fmt.Sprintf("failed writing response to %s", c.Conn.RemoteAddr()))
		c.Conn.Close()
	}

	slog.Info(fmt.Sprintf("%s %s (%d bytes)", c.Conn.RemoteAddr(), c.Request, written))
}
