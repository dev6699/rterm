package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"
	"strings"

	"github.com/dev6699/rterm"
	"github.com/dev6699/rterm/command"
)

func main() {
	err := run()
	if err != nil {
		log.Fatalf("program exited; err = %v", err)
	}
}

func run() error {
	rterm.SetPrefix("/")
	mux := http.NewServeMux()

	rterm.Register(
		mux,
		rterm.Command{
			Factory: func() (*command.Command, error) {
				return command.New("bash", nil)
			},
			Name:        "bash",
			Description: "Bash (Unix shell)",
			Writable:    true,
		},
		rterm.Command{
			Factory: func() (*command.Command, error) {
				return command.New("htop", nil)
			},
			Name:        "htop",
			Description: "Interactive system monitor process viewer and process manager",
			Writable:    false,
		},
		rterm.Command{
			Factory: func() (*command.Command, error) {
				return command.New("nvidia-smi", strings.Split("--query-gpu=utilization.gpu --format=csv -l 1", " "))
			},
			Name:        "nvidia-smi",
			Description: "Monitors and outputs the GPU utilization percentage every second",
			Writable:    false,
		},
	)

	addr := ":5000"
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	log.Println("⚠️ CAUTION USE AT YOUR OWN RISK!!! ⚠️")
	log.Printf("Server listening on http://0.0.0.0%s", addr)
	return server.ListenAndServe()
}
