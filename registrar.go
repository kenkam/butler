package butler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"
)

type registrarBackendHandler struct {
	r *registrar
}

func (r registrarBackendHandler) Use(next handleFunc) func(*Context) error {
	return func(c *Context) error {
		r.r.mu.Lock()
		defer r.r.mu.Unlock()
		for _, b := range r.r.backends {
			bh := backendHandler{b}
			next = bh.Use(next)
		}

		return next(c)
	}
}

type registrar struct {
	port               int
	registrationServer *Server
	registerCh         chan Backend
	unregisterCh       chan Backend
	backends           []Backend
	mu                 sync.Mutex
}

func newRegistrar(port int) (*registrar, error) {
	s, err := NewServer(&Config{
		Host:      "localhost",
		Listen:    port,
		ListenTLS: -1,
	})
	if err != nil {
		return nil, err
	}

	return &registrar{port, s, make(chan Backend), make(chan Backend), make([]Backend, 0), sync.Mutex{}}, nil
}

func handlePut(r *registrar) func(*Context) error {
	return func(c *Context) error {
		if c.Request.Method != "PUT" || c.Request.Path != "/backends" {
			c.Response = NotFound()
			return nil
		}

		contentType := ""
		hContentType, ok := c.Request.Headers[HeaderContentType]
		if ok || len(hContentType) > 0 {
			contentType = hContentType[0]
		}

		// Only support application/json for now
		if contentType != "text/json" && contentType != "application/json" {
			c.Response = UnsupportedMediaType()
			return nil
		}

		b := Backend{}
		err := json.Unmarshal(c.Request.Body, &b)
		if err != nil {
			c.Response = BadRequest()
			return nil
		}

		healthy, err := checkHealth(b, r)
		if !healthy || err != nil {
			c.Response = BadRequest()
			return nil
		}

		r.registerCh <- b
		c.Response = StatusCode(http.StatusNoContent, nil)
		return nil
	}
}

func (r *registrar) Listen() error {
	r.registrationServer.httpListener.handleFunc = handlePut(r)
	go r.processMessages()
	return r.registrationServer.Listen()
}

func (r *registrar) Close() error {
	if r.registrationServer != nil {
		return r.registrationServer.Close()
	}

	return nil
}

func (r *registrar) processMessages() {
	for {
		select {
		case nb := <-r.registerCh:
			for _, b := range r.backends {
				if nb.Equals(b) {
					break
				}
			}

			go healthCheckLoop(nb, r)

			r.mu.Lock()
			r.backends = append(r.backends, nb)
			r.mu.Unlock()

		case b := <-r.unregisterCh:
			r.mu.Lock()
			r.backends = slices.DeleteFunc(r.backends, func(eb Backend) bool {
				return eb.Equals(b)
			})
			r.mu.Unlock()

			slog.Debug(fmt.Sprintf("unregistering %v from backends", b))
		}
	}
}

func healthCheckLoop(b Backend, r *registrar) {
	for {
		<-time.After(5 * time.Second)

		healthy, err := checkHealth(b, r)
		if !healthy || err != nil {
			r.unregisterCh <- b
			return
		}
	}
}

func checkHealth(b Backend, r *registrar) (bool, error) {
	resp, err := http.Get("http://" + b.Addr + "/health")
	if err != nil {
		slog.Debug(fmt.Sprintf("%v is unhealthy: %v", b, err))
		r.unregisterCh <- b
		return false, err
	}

	if resp.StatusCode < 200 && resp.StatusCode >= 300 {
		slog.Debug(fmt.Sprintf("%v is unhealthy: status code: %v", b, resp.StatusCode))
		return false, nil
	}

	return true, nil
}
