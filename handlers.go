package butler

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
)

// If return value is true, should skip all other handlers
type handler interface {
	Handle(context *Context) (bool, error)
}

type redirectHTTPHandler struct {
	config *Config
}

func (r redirectHTTPHandler) Handle(c *Context) (bool, error) {
	if c.Request.Scheme == "http" {
		host := strings.Split(c.Request.Host, ":")[0] + ":" + strconv.Itoa(r.config.ListenTLS)
		c.Response = MovedPermanently("https://" + host + c.Request.Path)

		slog.Debug("redirecting to https for " + c.Conn.RemoteAddr().String())
		return true, nil
	}

	return false, nil
}

type backendHandler struct {
	b Backend
}

func (b backendHandler) Handle(c *Context) (bool, error) {
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

type documentRootHandler struct {
	docRoot string
}

func (s documentRootHandler) Handle(c *Context) (bool, error) {
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
