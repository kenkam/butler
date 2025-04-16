package butler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"
)

type Backend struct {
	Addr string `yaml:"Addr"`
	Path string `yaml:"Path"`
}

func (b Backend) Equals(o Backend) bool {
	return b.Addr == o.Addr && b.Path == o.Path
}

type registrar struct {
	port         int
	server       *Server
	registerCh   chan Backend
	unregisterCh chan Backend
	healthChecks []healthCheck
}

type healthCheck struct {
	b       Backend
	healthy bool
}

type putHandler struct {
	r *registrar
}

func newRegistrar(port int, server *Server) *registrar {
	return &registrar{port, server, make(chan Backend), make(chan Backend), make([]healthCheck, 0)}
}

func (p *putHandler) Handle(c *Context) (bool, error) {
	if c.Request.Method != "PUT" || c.Request.Path != "/backends" {
		c.Response = NotFound()
		return true, nil
	}

	contentType := ""
	hContentType, ok := c.Request.Headers[HeaderContentType]
	if ok || len(hContentType) > 0 {
		contentType = hContentType[0]
	}

	// Only support application/json for now
	if contentType != "text/json" && contentType != "application/json" {
		c.Response = UnsupportedMediaType()
		return true, nil
	}

	b := Backend{}
	err := json.Unmarshal(c.Request.Body, &b)
	if err != nil {
		c.Response = BadRequest()
		return true, nil
	}

	p.r.registerCh <- b
	c.Response = StatusCode(http.StatusNoContent, nil)
	return true, nil
}

func (r *registrar) Listen() error {
	s, err := NewServer(&Config{
		Listen:    r.port,
		ListenTLS: -1,
	})
	if err != nil {
		return err
	}

	s.httpListener.handlers = append(s.httpListener.handlers, &putHandler{r})
	go r.processMessages()

	return s.Listen()
}

func (r *registrar) processMessages() {
	register := func(b Backend) {
		for _, h := range r.healthChecks {
			if h.b.Equals(b) {
				return
			}
		}

		h := healthCheck{b, false}
		go healthCheckLoop(h, r)
		r.healthChecks = append(r.healthChecks, h)
	}

	unregister := func(b Backend) {
		r.healthChecks = slices.DeleteFunc(r.healthChecks, func(h healthCheck) bool {
			return h.b.Equals(b)
		})

		r.server.removeBackend(b)
		slog.Debug(fmt.Sprintf("unregistering %v from backends", b))
	}

	for {
		select {
		case b := <-r.registerCh:
			register(b)
		case b := <-r.unregisterCh:
			unregister(b)
		}
	}
}

func healthCheckLoop(h healthCheck, r *registrar) {
	for {
		previouslyHealthy := h.healthy

		resp, err := http.Get("http://" + h.b.Addr + "/health")
		if err != nil {
			slog.Debug(fmt.Sprintf("%v is unhealthy: %v", h.b, err))
			r.unregisterCh <- h.b
			return
		}

		if resp.StatusCode < 200 && resp.StatusCode >= 300 {
			slog.Debug(fmt.Sprintf("%v is unhealthy: status code: %v", h.b, resp.StatusCode))
			r.unregisterCh <- h.b
			return
		} else if !previouslyHealthy {
			r.server.addBackend(h.b)
			h.healthy = true
		}

		<-time.After(5 * time.Second)
	}
}
