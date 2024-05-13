package main

import (
	"log"

	"github.com/dev6699/rterm/command"
	"github.com/dev6699/rterm/server"
	"github.com/dev6699/rterm/ui"
)

func main() {
	err := run()
	if err != nil {
		log.Fatalf("program exited; err = %v", err)
	}
}

func run() error {
	assets, err := ui.Assets()
	if err != nil {
		return err
	}

	srv, err := server.New(
		assets,
		func() (*command.Command, error) {
			return command.New("bash", nil)
		},
	)
	if err != nil {
		return err
	}

	addr := ":5000"
	log.Println("⚠️ CAUTION USE AT YOUR OWN RISK!!! ⚠️")
	log.Printf("Server listening on http://0.0.0.0%s", addr)

	return srv.Run(addr)
}
