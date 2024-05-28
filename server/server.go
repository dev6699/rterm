package server

import (
	"log"
	"net/http"

	"github.com/dev6699/rterm/auth"
	"github.com/dev6699/rterm/tty"
	"github.com/gorilla/websocket"
)

type Command struct {
	Factory   tty.AgentFactory
	AuthCheck auth.AuthCheck
	Writable  bool
}

func HandleWebSocket(wsUpgrader *websocket.Upgrader, cmd Command) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {

		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("server: failed to upgrade websocket; err = %v", err)
			return
		}
		defer conn.Close()

		t := tty.New(WSController{Conn: conn}, cmd.Factory)
		t.WithWrite(cmd.Writable)
		t.WithAuthCheck(cmd.AuthCheck)

		err = t.Run(r.Context())
		if err != nil {
			log.Printf("server: socket connection closed; err = %v", err)
		}
	}

}
