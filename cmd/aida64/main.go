package main

import (
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"

	plugin "github.com/poizenjam/aida64sd/internal/app/aida64plugin"
)

var port = flag.String("port", "", "The port that should be used to create the WebSocket")
var pluginUUID = flag.String("pluginUUID", "", "A unique identifier string that should be used to register the plugin once the WebSocket is opened")
var registerEvent = flag.String("registerEvent", "", "Registration event")
var info = flag.String("info", "", "A stringified json containing the Stream Deck application information and devices information")

func main() {
	// make sure the font and bridge exe are read relative to the plugin exe
	if err := os.Chdir(filepath.Dir(os.Args[0])); err != nil {
		log.Fatalf("Unable to chdir: %v", err)
	}

	// Stream Deck runs us with no console; keep a small log next to the exe
	// and fall back to discarding if the folder is read-only.
	logPath := filepath.Join(filepath.Dir(os.Args[0]), "aida64.log")
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		log.SetOutput(f)
		log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	} else {
		log.SetOutput(io.Discard)
	}

	flag.Parse()

	p, err := plugin.NewPlugin(*port, *pluginUUID, *registerEvent, *info)
	if err != nil {
		log.Fatal("NewPlugin failed:", err)
	}

	if err := p.RunForever(); err != nil {
		log.Fatal("runForever", err)
	}
}
