package tty

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/dev6699/rterm/auth"
)

type AgentFactory = func() (Agent, error)

type TTY struct {
	controller   Controller
	agentFactory AgentFactory
	// agent will be nil unless authCheck has passed
	agent Agent

	// mutex to ensure no concurrent write to controller
	mut                  sync.Mutex
	agentMut             sync.RWMutex
	agentBufferSize      int
	controllerBufferSize int
	writable             bool
	authCheck            auth.AuthCheck
	onOutput             func([]byte)
}

func New(controller Controller, agentFactory AgentFactory, agentBufferSize int, controllerBufferSize int) *TTY {
	return &TTY{
		controller:           controller,
		agentFactory:         agentFactory,
		agentBufferSize:      agentBufferSize,
		controllerBufferSize: controllerBufferSize,
	}
}

func (t *TTY) WithWrite(b bool) {
	t.writable = b
}

func (t *TTY) WithAuthCheck(c auth.AuthCheck) {
	t.authCheck = c
}

// WriteInput writes bytes to the agent attached to this terminal.
func (t *TTY) WriteInput(data []byte) error {
	if !t.writable || len(data) == 0 {
		return nil
	}
	agent := t.currentAgent()
	if agent == nil {
		return fmt.Errorf("tty: terminal is not connected")
	}
	_, err := agent.Write(data)
	return err
}

func (t *TTY) WithOutput(callback func([]byte)) {
	t.onOutput = callback
}

func (t *TTY) Run(ctx context.Context) error {
	err := t.initialize()
	if err != nil {
		return err
	}

	errCh := make(chan error, 2)

	go func() {
		buf := make([]byte, t.agentBufferSize)
		for {
			agent := t.currentAgent()
			if agent == nil {
				continue
			}

			n, err := agent.Read(buf)
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
		buf := make([]byte, t.controllerBufferSize)
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
	if t.authCheck != nil {
		return t.controllerWrite(Auth, nil)
	}

	err := t.createAgent()
	if err != nil {
		return err
	}
	return t.controllerWrite(AuthOK, nil)
}

func (t *TTY) createAgent() error {
	t.agentMut.Lock()
	defer t.agentMut.Unlock()
	if t.agent != nil {
		return nil
	}

	var err error
	t.agent, err = t.agentFactory()
	if err != nil {
		return err
	}

	return nil
}

func (t *TTY) currentAgent() Agent {
	t.agentMut.RLock()
	defer t.agentMut.RUnlock()
	return t.agent
}

func (t *TTY) setAgent(agent Agent) {
	t.agentMut.Lock()
	defer t.agentMut.Unlock()
	t.agent = agent
}

func (t *TTY) handleAgentData(data []byte) error {
	if t.onOutput != nil {
		t.onOutput(append([]byte(nil), data...))
	}
	s := base64.StdEncoding.EncodeToString(data)
	return t.controllerWrite(Output, []byte(s))
}

func (t *TTY) handleControllerData(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("tty: message type missing")
	}

	msg := Message(data[0])
	switch msg {

	case AuthTry:
		code := data[1:]
		pass, err := t.authCheck.Verify(string(code))
		if err != nil {
			return err
		}

		if pass {
			agent, err := t.agentFactory()
			if err != nil {
				return err
			}
			t.setAgent(agent)
			t.controllerWrite(AuthOK, nil)
		} else {
			t.controllerWrite(AuthFailed, nil)
		}

	case Input:
		if !t.writable || len(data) <= 1 {
			return nil
		}

		agent := t.currentAgent()
		if agent == nil {
			return nil
		}
		_, err := agent.Write(data[1:])
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

		agent := t.currentAgent()
		if agent == nil {
			return nil
		}
		return agent.ResizeTerminal(r.Cols, r.Rows)

	default:
		return fmt.Errorf("tty: unknown message type: %c", msg)
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
