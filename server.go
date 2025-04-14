package butler

import (
	"errors"
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
		c := &Context{Conn: conn}

		r, err := ParseRequest(conn)
		if err != nil {
			slog.Debug(fmt.Sprintf("error reading from %s, closing connection...", conn.RemoteAddr()))
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
		return errors.New("failed writing response")
	}

	slog.Info(fmt.Sprintf("%s %s (%d bytes)", c.Conn.RemoteAddr(), c.Request, written))
	return nil
}
