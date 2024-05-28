package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"
	"strings"

	"github.com/dev6699/rterm"
	"github.com/dev6699/rterm/auth"
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
			Name:        "bash",
			Description: "Bash (Unix shell)",
			Writable:    true,
			AuthCheck:   auth.NewTOTP("F4ECH5IH72ECOFFN4INKHXA5AVKTS256"),
		},
		rterm.Command{
			Name:        "sh",
			Description: "Shell",
			Writable:    true,
			AuthCheck:   auth.NewBasic("123456"),
		},
		rterm.Command{
			Name:        "htop",
			Description: "Interactive system monitor process viewer and process manager",
		},
		rterm.Command{
			Name:        "nvidia-smi",
			Args:        strings.Split("--query-gpu=utilization.gpu --format=csv -l 1", " "),
			Description: "Monitors and outputs the GPU utilization percentage every second",
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
