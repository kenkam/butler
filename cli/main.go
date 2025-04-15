package main

import (
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/kenkam/butler"
)

type iso9601Writer struct{}

func (writer iso9601Writer) Write(bytes []byte) (int, error) {
	return fmt.Print(time.Now().UTC().Format("2006-01-02T15:04:05.000Z   ") + string(bytes))
}

func main() {
	log.SetFlags(0)
	log.SetOutput(new(iso9601Writer))
	slog.SetLogLoggerLevel(slog.LevelDebug)

	server := butler.NewServer("localhost", 8080, "../../void-cascade")
	server.AddBackend("127.0.0.1:8000", "/")
	log.Fatal(server.Listen())
}
