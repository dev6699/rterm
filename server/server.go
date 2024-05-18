package server

import (
	"log"
	"net/http"

	"github.com/dev6699/rterm/command"
	"github.com/dev6699/rterm/tty"
	"github.com/gorilla/websocket"
)

type CommandFactory = func() (*command.Command, error)

func HandleWebSocket(wsUpgrader *websocket.Upgrader, cmdFac CommandFactory, writable bool) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("server: failed to upgrade websocket; err = %v", err)
			return
		}
		defer conn.Close()

		cmd, err := cmdFac()
		if err != nil {
			log.Printf("server: failed to start command; err = %v", err)
			return
		}

		t := tty.New(WSController{Conn: conn}, cmd, writable)
		err = t.Run(r.Context())
		if err != nil {
			log.Printf("server: socket connection closed; err = %v", err)
		}
	}

}
