package tty_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/dev6699/rterm/tty"
)

type pipe struct {
	*io.PipeReader
	*io.PipeWriter
}

func (p pipe) Read(d []byte) (n int, err error) {
	if p.PipeReader != nil {
		return p.PipeReader.Read(d)
	}

	select {}
}

func (p pipe) Write(d []byte) (n int, err error) {
	if p.PipeWriter != nil {
		return p.PipeWriter.Write(d)
	}
	select {}
}

func (p pipe) ResizeTerminal(columns int, row int) error {
	return nil
}

func Test_AgentWrite(t *testing.T) {
	agentReader, agentWriter := io.Pipe()
	agent := pipe{
		PipeReader: agentReader,
	}

	controllerReader, controllerWriter := io.Pipe()
	controller := pipe{
		PipeWriter: controllerWriter,
	}

	dt := tty.New(controller, agent)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func(t *testing.T) {
		defer wg.Done()
		dt.Run(ctx)
	}(t)

	message := []byte("foobar")
	_, err := agentWriter.Write(message)
	if err != nil {
		t.Fatalf("agentWriter.Write(); err  = %v", err)
	}

	buf := make([]byte, 1024)
	n, err := controllerReader.Read(buf)
	if err != nil {
		t.Fatalf("controllerReader.Read(); err = %v", err)
	}

	if tty.Message(buf[0]) != tty.Output {
		t.Fatalf("got message type = %c; want = %c", tty.Message(buf[0]), tty.Output)
	}

	decoded := make([]byte, 1024)
	n, err = base64.StdEncoding.Decode(decoded, buf[1:n])
	if err != nil {
		t.Fatalf("base64.StdEncoding.Decode(); err = %v", err)
	}
	if !bytes.Equal(decoded[:n], message) {
		t.Fatalf("got message = %s; want = %s", decoded[:n], message)
	}

	cancel()
	wg.Wait()
}

func Test_ControllerWrite(t *testing.T) {
	agentReader, agentWriter := io.Pipe()
	agent := pipe{
		PipeReader: agentReader,
		PipeWriter: agentWriter,
	}

	controllerReader, controllerWriter := io.Pipe()
	controller := pipe{
		PipeReader: controllerReader,
		PipeWriter: controllerWriter,
	}

	dt := tty.New(controller, agent)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func(t *testing.T) {
		defer wg.Done()
		dt.Run(ctx)
	}(t)

	message := []byte(fmt.Sprintf("%chello\n", tty.Input))
	_, err := controllerWriter.Write(message)
	if err != nil {
		t.Fatalf("controllerWriter.Write(); err  = %v", err)
	}

	buf := make([]byte, 1024)
	n, err := agentReader.Read(buf)
	if err != nil {
		t.Fatalf("agentReader.Read(); err = %v", err)
	}

	if !bytes.Equal(buf[:n], message[1:]) {
		t.Fatalf("got message = %s; want = %s", buf[:n], message[1:])
	}

	cancel()
	wg.Wait()
}
