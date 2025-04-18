package butler

import (
	"bytes"
	"io"
	"net/http"
	"strings"
)

type Backend struct {
	Addr string `yaml:"Addr"`
	Path string `yaml:"Path"`
}

func (b Backend) Equals(o Backend) bool {
	return b.Addr == o.Addr && b.Path == o.Path
}

type backendHandler struct {
	b Backend
}

func (b backendHandler) Use(next handleFunc) func(*Context) error {
	return func(c *Context) error {
		if strings.HasPrefix(c.Request.Path, b.b.Path) {

			url := "http://" + b.b.Addr + c.Request.Path
			r, err := http.NewRequest(c.Request.Method, url, bytes.NewReader(c.Request.Body))
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
				c.Response = BadGateway()
				return nil
			}
			defer resp.Body.Close()

			b, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}

			c.Response = StatusCode(resp.StatusCode, b)
			for k, vs := range resp.Header {
				c.Response.Headers[k] = vs
			}

			return nil
		}

		return next(c)
	}
}
