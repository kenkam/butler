package butler

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path"
	"strings"
)

const (
	bufferSize = 24
)

type Server struct {
	Host         string
	Port         int
	DocumentRoot string
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
		var read int
		var requestBuilder strings.Builder
		totalRead := 0

		for {
			buffer := [bufferSize]byte{}
			var err error
			read, err = conn.Read(buffer[:])
			if err != nil {
				slog.Debug(fmt.Sprintf("error reading from %s, closing connection...", conn.RemoteAddr()))
				conn.Close()
				return
			}

			if read != bufferSize {
				break
			}

			// TODO read headers first
			written, err := requestBuilder.WriteString(string(buffer[:read]))
			if err != nil {
				slog.Error("failed to write request")
				conn.Close()
			}

			totalRead += written
		}

		request := Parse(requestBuilder.String())
		slog.Debug(fmt.Sprintf("%s %s", conn.RemoteAddr(), request.String()))

		server.handle(request, conn)
	}
}

func (server Server) handle(request *Request, conn net.Conn) {
	var response *Response
	if request.path == "/" {
		request.path = "/index.html"
	}

	path := path.Join(server.DocumentRoot, request.path)
	data, err := os.ReadFile(path)
	if err != nil {
		_, isPathError := err.(*os.PathError)
		if isPathError {
			response = NotFound()
		}
	} else {
		response = Ok(data)
	}

	written, err := conn.Write(response.ToBytes())
	if err != nil {
		slog.Debug(fmt.Sprintf("failed writing response to %s", conn.RemoteAddr()))
		conn.Close()
	}

	slog.Info(fmt.Sprintf("%s %s (%d bytes)", conn.RemoteAddr(), request, written))
}
