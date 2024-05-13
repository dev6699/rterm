package server

import "github.com/gorilla/websocket"

type WSController struct {
	*websocket.Conn
}

func (w WSController) Write(p []byte) (n int, err error) {
	writer, err := w.Conn.NextWriter(websocket.TextMessage)
	if err != nil {
		return 0, err
	}
	defer writer.Close()
	return writer.Write(p)
}

func (w WSController) Read(p []byte) (n int, err error) {
	for {
		msgType, reader, err := w.Conn.NextReader()
		if err != nil {
			return 0, err
		}

		if msgType != websocket.TextMessage {
			continue
		}

		return reader.Read(p)
	}
}
