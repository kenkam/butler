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
	port               int
	backingServer      *Server
	registrationServer *Server
	registerCh         chan healthCheck
	unregisterCh       chan healthCheck
	healthChecks       []healthCheck
}

type healthCheck struct {
	b Backend
}

type putHandler struct {
	r *registrar
}

func newRegistrar(port int, server *Server) (*registrar, error) {
	s, err := NewServer(&Config{
		Host:      "localhost",
		Listen:    port,
		ListenTLS: -1,
	})
	if err != nil {
		return nil, err
	}

	return &registrar{port, server, s, make(chan healthCheck), make(chan healthCheck), make([]healthCheck, 0)}, nil
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

	h := healthCheck{b}
	healthy, err := checkHealth(h, p.r)
	if !healthy || err != nil {
		c.Response = BadRequest()
		return true, nil
	}

	p.r.registerCh <- h
	p.r.backingServer.addBackend(h.b)
	c.Response = StatusCode(http.StatusNoContent, nil)
	return true, nil
}

func (r *registrar) Listen() error {
	r.registrationServer.httpListener.handlers = append(r.registrationServer.httpListener.handlers, &putHandler{r})
	go r.processMessages()
	return r.registrationServer.Listen()
}

func (r *registrar) processMessages() {
	for {
		select {
		case nh := <-r.registerCh:
			for _, h := range r.healthChecks {
				if h.b.Equals(nh.b) {
					break
				}
			}

			go healthCheckLoop(nh, r)
			r.healthChecks = append(r.healthChecks, nh)

		case b := <-r.unregisterCh:
			r.healthChecks = slices.DeleteFunc(r.healthChecks, func(h healthCheck) bool {
				return h.b.Equals(b.b)
			})

			r.backingServer.removeBackend(b.b)
			slog.Debug(fmt.Sprintf("unregistering %v from backends", b))
		}
	}
}

func healthCheckLoop(h healthCheck, r *registrar) {
	for {
		<-time.After(5 * time.Second)

		healthy, err := checkHealth(h, r)
		if !healthy || err != nil {
			r.unregisterCh <- h
			return
		}
	}
}

func checkHealth(h healthCheck, r *registrar) (bool, error) {
	resp, err := http.Get("http://" + h.b.Addr + "/health")
	if err != nil {
		slog.Debug(fmt.Sprintf("%v is unhealthy: %v", h.b, err))
		r.unregisterCh <- h
		return false, err
	}

	if resp.StatusCode < 200 && resp.StatusCode >= 300 {
		slog.Debug(fmt.Sprintf("%v is unhealthy: status code: %v", h.b, resp.StatusCode))
		return false, nil
	}

	return true, nil
}
