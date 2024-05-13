package server

import (
	"io/fs"
	"log"
	"net/http"

	"github.com/dev6699/rterm/command"
	"github.com/dev6699/rterm/tty"
	"github.com/gorilla/websocket"
)

type CommandFactory = func() (*command.Command, error)

type Server struct {
	wsUpgrader *websocket.Upgrader
	cmdFac     CommandFactory
}

func New(assets fs.FS, cmdFac CommandFactory) (*Server, error) {
	s := &Server{
		wsUpgrader: &websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		cmdFac: cmdFac,
	}

	http.Handle("/", http.FileServer(http.FS(assets)))
	http.HandleFunc("/ws", s.handleWebSocket)

	return s, nil
}

func (s *Server) Run(addr string) error {
	return http.ListenAndServe(addr, nil)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("server: failed to upgrade websocket; err = %v", err)
		return
	}
	defer conn.Close()

	cmd, err := s.cmdFac()
	if err != nil {
		log.Printf("server: failed to start command; err = %v", err)
		return
	}

	t := tty.New(WSController{Conn: conn}, cmd)
	err = t.Run(r.Context())
	if err != nil {
		log.Printf("server: socket connection closed; err = %v", err)
	}
}
