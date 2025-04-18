package butler

import (
	"log/slog"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
)

type handleFunc func(c *Context) error

func notFoundHandleFunc(c *Context) error {
	c.Response = NotFound()
	return nil
}

type handlerBuilder struct {
	handlers []handler
}

func (b *handlerBuilder) Build() handleFunc {
	var f handleFunc = notFoundHandleFunc
	hs := b.handlers[:]
	slices.Reverse(hs)
	for _, h := range hs {
		f = h.Use(f)
	}
	return f
}

func (b *handlerBuilder) Use(handler handler) *handlerBuilder {
	b.handlers = append(b.handlers, handler)
	return b
}

type handler interface {
	Use(next handleFunc) func(*Context) error
}

type redirectHTTPHandler struct {
	config *Config
}

func (r redirectHTTPHandler) Use(next handleFunc) func(*Context) error {
	return func(c *Context) error {
		if c.Request.Scheme == "http" {
			host := strings.Split(c.Request.Host, ":")[0] + ":" + strconv.Itoa(r.config.ListenTLS)
			c.Response = MovedPermanently("https://" + host + c.Request.Path)

			slog.Debug("redirecting to https for " + c.Conn.RemoteAddr().String())
		}

		return next(c)
	}
}

type documentRootHandler struct {
	docRoot string
}

func (s documentRootHandler) Use(next handleFunc) func(*Context) error {
	return func(c *Context) error {
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
			return nil
		}

		return next(c)
	}
}
