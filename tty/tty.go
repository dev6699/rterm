package tty

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
)

type TTY struct {
	controller Controller
	agent      Agent
	mut        sync.Mutex
	bufferSize int
	writable   bool
}

func New(controller Controller, agent Agent, writable bool) *TTY {
	return &TTY{
		controller: controller,
		agent:      agent,
		bufferSize: 1024,
		writable:   writable,
	}
}

func (t *TTY) Run(ctx context.Context) error {
	err := t.initialize()
	if err != nil {
		return err
	}

	errCh := make(chan error, 2)

	go func() {
		buf := make([]byte, t.bufferSize)
		for {
			n, err := t.agent.Read(buf)
			if err != nil {
				errCh <- err
				return
			}

			err = t.handleAgentData(buf[:n])
			if err != nil {
				errCh <- err
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, t.bufferSize)
		for {
			n, err := t.controller.Read(buf)
			if err != nil {
				errCh <- err
				return
			}

			err = t.handleControllerData(buf[:n])
			if err != nil {
				errCh <- err
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		err = ctx.Err()

	case err = <-errCh:
	}

	return err
}

func (t *TTY) initialize() error {
	return nil
}

func (t *TTY) handleAgentData(data []byte) error {
	s := base64.StdEncoding.EncodeToString(data)
	return t.controllerWrite(Output, []byte(s))
}

func (t *TTY) handleControllerData(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("tty: message type missing")
	}

	msg := Message(data[0])
	switch msg {
	case Input:
		if !t.writable || len(data) <= 1 {
			return nil
		}

		_, err := t.agent.Write(data[1:])
		if err != nil {
			return err
		}

	case ResizeTerminal:
		type resize struct {
			Cols int `json:"cols"`
			Rows int `json:"rows"`
		}

		var r resize
		err := json.Unmarshal(data[1:], &r)
		if err != nil {
			return err
		}

		return t.agent.ResizeTerminal(r.Cols, r.Rows)

	default:
		return fmt.Errorf("tty: unkown message type: %c", msg)
	}

	return nil
}

func (t *TTY) controllerWrite(m Message, data []byte) error {
	t.mut.Lock()
	defer t.mut.Unlock()

	_, err := t.controller.Write(append([]byte{byte(m)}, data...))
	if err != nil {
		return err
	}

	return nil
}
