package provider

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dev6699/rterm/tty"
)

type closeTrackingAgent struct {
	reads     chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func (a *closeTrackingAgent) Read(data []byte) (int, error) {
	chunk, ok := <-a.reads
	if !ok {
		return 0, io.EOF
	}
	return copy(data, chunk), nil
}

func (a *closeTrackingAgent) Write(data []byte) (int, error) { return len(data), nil }

func (a *closeTrackingAgent) ResizeTerminal(columns, rows int) error { return nil }

func (a *closeTrackingAgent) Close() error {
	a.closeOnce.Do(func() {
		close(a.closed)
		close(a.reads)
	})
	return nil
}

func TestSharedAgentEvictsStalledSubscriber(t *testing.T) {
	agent := &closeTrackingAgent{reads: make(chan []byte), closed: make(chan struct{})}
	shared := &sharedAgent{agent: agent, subscribers: make(map[*sharedSubscription]struct{})}
	stalled := shared.subscribe()
	active := shared.subscribe()
	for index := 0; index < cap(stalled.subscription.output); index++ {
		stalled.subscription.output <- []byte("buffered")
	}

	go shared.readLoop()
	agent.reads <- []byte("delivered")
	select {
	case chunk := <-active.subscription.output:
		if string(chunk) != "delivered" {
			t.Fatalf("got %q, want delivered", chunk)
		}
	case <-time.After(time.Second):
		t.Fatal("active subscriber did not receive output")
	}
	select {
	case <-stalled.subscription.done:
	case <-time.After(time.Second):
		t.Fatal("stalled subscriber was not evicted")
	}
	close(agent.reads)
}

func TestSharedAgentCloseStopsFinalAgentAndRemovesCache(t *testing.T) {
	agent := &closeTrackingAgent{reads: make(chan []byte), closed: make(chan struct{})}
	service := NewService(Config{})
	connection, err := service.SharedAgent("session", func() (tty.Agent, error) { return agent, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.(io.Closer).Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-agent.closed:
	case <-time.After(time.Second):
		t.Fatal("underlying agent was not closed")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if _, ok := service.agents["session"]; ok {
		t.Fatal("shared agent remained cached after final subscriber closed")
	}
}

func TestSharedAgentConnectionCloseUnblocksRead(t *testing.T) {
	shared := &sharedAgent{subscribers: make(map[*sharedSubscription]struct{})}
	connection := shared.subscribe()
	result := make(chan error, 1)
	go func() {
		_, err := connection.Read(make([]byte, 1))
		result <- err
	}()

	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != io.EOF {
		t.Fatalf("got %v, want io.EOF", err)
	}
	shared.mu.Lock()
	defer shared.mu.Unlock()
	if len(shared.subscribers) != 0 {
		t.Fatalf("subscriber was not removed: %d remain", len(shared.subscribers))
	}
}

func TestExpandArgsUsesArgumentValuesWithoutShellParsing(t *testing.T) {
	args, err := expandArgs([]string{"ssh", "{user}@{target}", "echo; touch /tmp/nope"}, map[string]string{
		"user":   "root",
		"target": "node name",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := args[1], "root@node name"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := args[2], "echo; touch /tmp/nope"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestValidateRejectsIncompleteProfile(t *testing.T) {
	err := (Config{Providers: []Profile{{Name: "test", Discover: DiscoveryCommand{Command: Command{Program: "discover"}}, Users: []string{"root"}, Connect: Command{Program: "ssh"}, Upload: Command{Program: "scp"}, Download: Command{Program: "scp"}}}}).Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDiscoverTargetsMapsRawJSONAndPredefinedUsers(t *testing.T) {
	directory := t.TempDir()
	program := filepath.Join(directory, "discover")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf '%s' '[{\"spec\":{\"hostname\":\"node-1\"}}]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	profile := Profile{
		Name:     "test",
		Discover: DiscoveryCommand{Command: Command{Program: program}, TargetPath: "spec.hostname"},
		Users:    []string{"root", "ubuntu"},
	}
	discovery, err := profile.DiscoverTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Targets) != 1 || discovery.Targets[0].ID != "node-1" {
		t.Fatalf("unexpected discovery: %+v", discovery)
	}
	if got := discovery.Targets[0].Users; len(got) != 2 || got[1] != "ubuntu" {
		t.Fatalf("unexpected users: %+v", got)
	}
}

func TestResolveUploadPathPreservesOrAddsMatchingExtension(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		filename string
		want     string
	}{
		{name: "folder", path: "/tmp", filename: "archive.tar", want: "/tmp/archive.tar"},
		{name: "trailing slash folder", path: "/tmp/", filename: "archive.tar", want: "/tmp/archive.tar"},
		{name: "matching extension rename", path: "/tmp/renamed.tar", filename: "source.tar", want: "/tmp/renamed.tar"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveUploadPath(test.path, test.filename)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveUploadPathRejectsMismatchedExtension(t *testing.T) {
	if _, err := resolveUploadPath("/tmp/archive.zip", "archive.tar"); err == nil {
		t.Fatal("expected extension mismatch")
	}
}
