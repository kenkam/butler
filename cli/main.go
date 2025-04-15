package main

import (
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/alecthomas/kong"
	"github.com/kenkam/butler"
)

type ServeCmd struct {
	Config string `arg:""`
}

var CLI struct {
	Serve ServeCmd `cmd:"" help:"Start server."`
}

type iso9601Writer struct{}

func (writer iso9601Writer) Write(bytes []byte) (int, error) {
	return fmt.Print(time.Now().UTC().Format("2006-01-02T15:04:05.000Z   ") + string(bytes))
}

func (c *ServeCmd) Run() error {
	server, err := butler.NewServerYaml(c.Config)
	if err != nil {
		log.Fatal(err)
	}

	server.AddBackend("127.0.0.1:8000", "/")
	log.Fatal(server.ListenTLS())
	return nil
}

func main() {
	log.SetFlags(0)
	log.SetOutput(new(iso9601Writer))
	slog.SetLogLoggerLevel(slog.LevelDebug)

	ctx := kong.Parse(&CLI)
	ctx.Run()
}
