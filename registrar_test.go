package butler

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"testing"
)

func TestRegisteringBackend(t *testing.T) {
	log.SetOutput(io.Discard)

	s, _ := NewServer(&Config{
		Host:            "localhost",
		Listen:          0,
		ListenTLS:       -1,
		Registrar:       true,
		RegistrarListen: 0,
	})

	b, _ := NewServer(&Config{
		Host:         "localhost",
		Listen:       0,
		ListenTLS:    -1,
		DocumentRoot: "./testdata",
	})

	defer s.Close()
	defer b.Close()

	go s.Listen()
	go b.Listen()
	<-s.httpListener.readyCh
	<-s.registrar.registrationServer.httpListener.readyCh
	<-b.httpListener.readyCh

	registrarHost := s.registrar.registrationServer.httpListener.listener.Addr().String()
	payload, err := json.Marshal(&Backend{
		Addr: b.httpListener.listener.Addr().String(),
		Path: "/",
	})
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("PUT", "http://"+registrarHost+"/backends", bytes.NewReader(payload))
	req.Header[HeaderContentType] = []string{"application/json"}
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 but got %v", resp.StatusCode)
	}

	resp, err = http.Get("http://" + s.httpListener.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 but got %v", resp.StatusCode)
	}
}
