package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dev6699/rterm/tty"
	"github.com/gorilla/websocket"
)

type testAgent struct {
	mu       sync.Mutex
	reads    chan []byte
	writes   [][]byte
	columns  int
	rows     int
	readErr  error
	writeErr error
	onWrite  func([]byte)
}

func (a *testAgent) Read(data []byte) (int, error) {
	if a.readErr != nil {
		return 0, a.readErr
	}
	chunk, ok := <-a.reads
	if !ok {
		return 0, io.EOF
	}
	return copy(data, chunk), nil
}

func (a *testAgent) Write(data []byte) (int, error) {
	if a.writeErr != nil {
		return 0, a.writeErr
	}
	a.mu.Lock()
	a.writes = append(a.writes, append([]byte(nil), data...))
	a.mu.Unlock()
	if a.onWrite != nil {
		a.onWrite(data)
	}
	return len(data), nil
}

func (a *testAgent) ResizeTerminal(columns, rows int) error {
	a.columns, a.rows = columns, rows
	return nil
}

func testService(t *testing.T) (*Service, Session) {
	t.Helper()
	directory := t.TempDir()
	discover := filepath.Join(directory, "discover")
	if err := os.WriteFile(discover, []byte("#!/bin/sh\nprintf '%s' '[{\"id\":\"node-1\"}]'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	profile := Profile{
		Name:     "test",
		Discover: DiscoveryCommand{Command: Command{Program: discover}, TargetPath: "id"},
		Users:    []string{"root"},
		Connect:  Command{Program: "ssh", Args: []string{"{user}@{target}"}},
		Upload:   Command{Program: "true"},
		Download: Command{Program: "true"},
		MaxBytes: 1024,
	}
	service := NewService(Config{Providers: []Profile{profile}})
	session, err := service.CreateSession(context.Background(), "test", SessionRequest{Target: "node-1", User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	return service, session
}

func request(service *Service, method, path, token string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	service.Handler().ServeHTTP(recorder, req)
	return recorder
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder, value any) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(value); err != nil {
		t.Fatal(err)
	}
}

func TestServiceSessionAccessorsAndOutput(t *testing.T) {
	service, session := testService(t)
	if got := service.Profiles(); len(got) != 1 || got[0] != "test" {
		t.Fatalf("unexpected profiles: %#v", got)
	}
	if _, ok := service.Profile("missing"); ok {
		t.Fatal("missing profile was found")
	}
	if got, ok := service.Session(session.ID); !ok || got.Token != session.Token {
		t.Fatal("session lookup failed")
	}
	if _, ok := service.Session("missing"); ok {
		t.Fatal("missing session was found")
	}
	if !service.AttachTerminal(session.ID, nil) || service.AttachTerminal("missing", nil) {
		t.Fatal("unexpected terminal attachment result")
	}
	service.AppendOutput("missing", []byte("ignored"))
	service.AppendOutput(session.ID, []byte("one\ntwo\nthree"))
	if output, truncated := service.ReadOutput(session.ID, 2); output != "two\nthree" || !truncated {
		t.Fatalf("unexpected limited output %q, %v", output, truncated)
	}
	if output, truncated := service.ReadOutput(session.ID, 0); output != "one\ntwo\nthree" || truncated {
		t.Fatalf("unexpected full output %q, %v", output, truncated)
	}
	if output, ok := service.ReadOutput("missing", 1); ok || output != "" {
		t.Fatal("missing output was found")
	}
	service.AppendOutput(session.ID, bytes.Repeat([]byte("x"), 256*1024+10))
	if output, _ := service.ReadOutput(session.ID, 1); len(output) != 256*1024 {
		t.Fatalf("output was not capped: %d bytes", len(output))
	}
}

func TestServiceAuthorizationAndConnectArgs(t *testing.T) {
	service, session := testService(t)
	for _, test := range []struct {
		name  string
		path  string
		token string
		want  bool
	}{
		{"bearer", "/api/sessions/" + session.ID + "/read", session.Token, true},
		{"query websocket", "/api/sessions/" + session.ID + "/ws?token=" + url.QueryEscape(session.Token), session.Token, true},
		{"wrong token", "/api/sessions/" + session.ID + "/read", "wrong", false},
		{"missing token", "/api/sessions/" + session.ID + "/read", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.path, nil)
			if !strings.Contains(test.name, "query") {
				req.Header.Set("Authorization", "Bearer "+test.token)
			}
			if got := service.AuthorizeSession(req, session.ID); got != test.want {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
	program, args, err := service.ConnectArgs(session.ID)
	if err != nil || program != "ssh" || len(args) != 1 || args[0] != "root@node-1" {
		t.Fatalf("unexpected connect args: %q %#v %v", program, args, err)
	}
	if _, _, err := service.ConnectArgs("missing"); err == nil {
		t.Fatal("missing session did not fail")
	}
	service.sessions[session.ID] = Session{ID: session.ID, Provider: "missing"}
	if _, _, err := service.ConnectArgs(session.ID); err == nil {
		t.Fatal("missing provider did not fail")
	}
}

func TestCreateSessionErrors(t *testing.T) {
	service, _ := testService(t)
	for name, request := range map[string]SessionRequest{
		"unknown target": {Target: "missing", User: "root"},
		"unknown user":   {Target: "node-1", User: "admin"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.CreateSession(context.Background(), "test", request); err == nil {
				t.Fatal("expected create error")
			}
		})
	}
	if _, err := service.CreateSession(context.Background(), "missing", SessionRequest{}); err == nil {
		t.Fatal("unknown provider did not fail")
	}
	profile := service.profiles["test"]
	profile.Discover.Program = "sh"
	profile.Discover.Args = []string{"-c", "exit 1"}
	service.profiles["test"] = profile
	if _, err := service.CreateSession(context.Background(), "test", SessionRequest{}); err == nil {
		t.Fatal("discovery failure did not propagate")
	}
}

func TestSharedAgentFanoutAndOperations(t *testing.T) {
	agent := &testAgent{reads: make(chan []byte, 1)}
	service := NewService(Config{})
	var output []byte
	first, err := service.SharedAgent("session", func() (tty.Agent, error) { return agent, nil }, func(data []byte) { output = append(output, data...) })
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.SharedAgent("session", func() (tty.Agent, error) { t.Fatal("factory called twice"); return nil, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	agent.reads <- []byte("output")
	buffer := make([]byte, 20)
	if n, err := first.Read(buffer); err != nil || string(buffer[:n]) != "output" {
		t.Fatalf("first read: %d %v %q", n, err, buffer[:n])
	}
	if n, err := second.Read(buffer); err != nil || string(buffer[:n]) != "output" {
		t.Fatalf("second read: %d %v %q", n, err, buffer[:n])
	}
	deadline := time.Now().Add(time.Second)
	for len(output) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if string(output) != "output" {
		t.Fatalf("callback output: %q", output)
	}
	if _, err := first.Write([]byte("input")); err != nil {
		t.Fatal(err)
	}
	if err := first.ResizeTerminal(80, 24); err != nil || agent.columns != 80 || agent.rows != 24 {
		t.Fatalf("resize failed: %v", err)
	}
	if err := first.(io.Closer).Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.(io.Closer).Close(); err != nil {
		t.Fatal(err)
	}
	if len(agent.writes) != 1 || string(agent.writes[0]) != "input" {
		t.Fatalf("writes: %#v", agent.writes)
	}

	failed := errors.New("factory failed")
	if _, err := service.SharedAgent("failed", func() (tty.Agent, error) { return nil, failed }, nil); !errors.Is(err, failed) {
		t.Fatalf("factory error: %v", err)
	}
}

func TestHandlerRoutesAndHandoffs(t *testing.T) {
	service, session := testService(t)
	if recorder := request(service, http.MethodGet, "/api/providers", "", nil); recorder.Code != http.StatusOK {
		t.Fatalf("providers status: %d", recorder.Code)
	}
	if recorder := request(service, http.MethodGet, "/api/providers/test/targets", "", nil); recorder.Code != http.StatusOK {
		t.Fatalf("targets status: %d", recorder.Code)
	}
	if recorder := request(service, http.MethodGet, "/api/providers/missing/targets", "", nil); recorder.Code != http.StatusNotFound {
		t.Fatalf("missing targets status: %d", recorder.Code)
	}
	if recorder := request(service, http.MethodGet, "/api/providers/test/targets", "", strings.NewReader("bad")); recorder.Code != http.StatusOK {
		t.Fatalf("targets request status: %d", recorder.Code)
	}
	if recorder := request(service, http.MethodPost, "/api/providers/test/sessions", "", strings.NewReader("bad")); recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid session status: %d", recorder.Code)
	}
	body := strings.NewReader(`{"target":"node-1","user":"root"}`)
	created := request(service, http.MethodPost, "/api/providers/test/sessions", "", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status: %d", created.Code)
	}
	var createdSession Session
	decodeBody(t, created, &createdSession)
	req := httptest.NewRequest(http.MethodPost, "/api/providers/test/sessions", strings.NewReader(`{"target":"node-1","user":"root"}`))
	req.Header.Set("X-Rterm-Handoff", "1")
	recorder := httptest.NewRecorder()
	service.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("handoff create status: %d", recorder.Code)
	}
	decodeBody(t, recorder, &createdSession)
	if createdSession.Handoff == "" {
		t.Fatal("handoff missing")
	}
	handoff := request(service, http.MethodPost, "/api/sessions/"+createdSession.ID+"/handoff", "", strings.NewReader(`{"handoff":"`+createdSession.Handoff+`"}`))
	if handoff.Code != http.StatusOK {
		t.Fatalf("handoff status: %d", handoff.Code)
	}
	if reused := request(service, http.MethodPost, "/api/sessions/"+createdSession.ID+"/handoff", "", strings.NewReader(`{"handoff":"`+createdSession.Handoff+`"}`)); reused.Code != http.StatusUnauthorized {
		t.Fatalf("reused handoff status: %d", reused.Code)
	}
	for _, test := range []struct {
		method string
		path   string
		code   int
	}{
		{http.MethodPost, "/api/sessions/" + session.ID + "/handoff", http.StatusBadRequest},
		{http.MethodPost, "/api/sessions/missing/handoff", http.StatusBadRequest},
		{http.MethodGet, "/api/sessions/" + session.ID + "/read", http.StatusUnauthorized},
		{http.MethodPost, "/api/sessions/" + session.ID + "/share", http.StatusUnauthorized},
		{http.MethodPost, "/api/sessions/" + session.ID + "/execute", http.StatusUnauthorized},
		{http.MethodPost, "/api/sessions/" + session.ID + "/upload", http.StatusUnauthorized},
		{http.MethodGet, "/api/sessions/" + session.ID + "/download", http.StatusUnauthorized},
	} {
		if recorder := request(service, test.method, test.path, "wrong", nil); recorder.Code != test.code {
			t.Errorf("%s: got %d, want %d", test.path, recorder.Code, test.code)
		}
	}
	if recorder := request(service, http.MethodGet, "/api/sessions/"+session.ID+"/read?maxLines=bad", session.Token, nil); recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid read status: %d", recorder.Code)
	}
	if recorder := request(service, http.MethodGet, "/unknown", "", nil); recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown route status: %d", recorder.Code)
	}
}

func TestHandlerReadShareExecuteAndTransfer(t *testing.T) {
	service, session := testService(t)
	service.AppendOutput(session.ID, []byte("line 1\nline 2"))
	read := request(service, http.MethodGet, "/api/sessions/"+session.ID+"/read?maxLines=1", session.Token, nil)
	if read.Code != http.StatusOK {
		t.Fatalf("read status: %d", read.Code)
	}
	share := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/share", session.Token, nil)
	if share.Code != http.StatusOK {
		t.Fatalf("share status: %d", share.Code)
	}
	var shareBody map[string]string
	decodeBody(t, share, &shareBody)
	if shareBody["handoff"] == "" {
		t.Fatal("share handoff missing")
	}
	if execute := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/execute", session.Token, strings.NewReader(`{"command":"echo hi"}`)); execute.Code != http.StatusConflict {
		t.Fatalf("unattached execute status: %d", execute.Code)
	}
	for _, body := range []string{"bad", `{"command":"   "}`} {
		if execute := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/execute", session.Token, strings.NewReader(body)); execute.Code != http.StatusBadRequest {
			t.Fatalf("invalid execute status: %d", execute.Code)
		}
	}
	if execute := request(service, http.MethodPost, "/api/sessions/missing/execute", session.Token, strings.NewReader(`{"command":"x"}`)); execute.Code != http.StatusUnauthorized {
		t.Fatalf("missing execute auth status: %d", execute.Code)
	}
	missingExecute := httptest.NewRecorder()
	service.execute(missingExecute, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"command":"x"}`)), "missing")
	if missingExecute.Code != http.StatusNotFound {
		t.Fatalf("direct missing execute status: %d", missingExecute.Code)
	}

	upload := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/upload?path=/tmp/file.txt&filename=source.txt", session.Token, strings.NewReader("contents"))
	if upload.Code != http.StatusOK {
		t.Fatalf("upload status: %d body=%s", upload.Code, upload.Body.String())
	}
	if tooLarge := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/upload?path=/tmp/file.txt&filename=source.txt", session.Token, strings.NewReader(strings.Repeat("x", 1025))); tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large upload status: %d", tooLarge.Code)
	}
	if failedBody := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/upload?path=/tmp/file.txt&filename=source.txt", session.Token, failingReader{}); failedBody.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("failed upload body status: %d", failedBody.Code)
	}
	if badPath := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/upload?path=/tmp/file.zip&filename=source.txt", session.Token, strings.NewReader("x")); badPath.Code != http.StatusBadRequest {
		t.Fatalf("bad upload path status: %d", badPath.Code)
	}
	if missingPath := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/upload", session.Token, strings.NewReader("x")); missingPath.Code != http.StatusBadRequest {
		t.Fatalf("missing upload path status: %d", missingPath.Code)
	}
	if missingName := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/upload?path=/tmp", session.Token, strings.NewReader("x")); missingName.Code != http.StatusBadRequest {
		t.Fatalf("missing filename status: %d", missingName.Code)
	}
	if missingTransferPath := request(service, http.MethodGet, "/api/sessions/"+session.ID+"/download", session.Token, nil); missingTransferPath.Code != http.StatusBadRequest {
		t.Fatalf("missing download path status: %d", missingTransferPath.Code)
	}
}

func TestTransferAndExecuteSuccessAndFailures(t *testing.T) {
	service, session := testService(t)
	oldRunner := execCommandContext
	defer func() { execCommandContext = oldRunner }()
	execCommandContext = func(context.Context, string, ...string) commandRunner { return fakeRunner{output: []byte("ok")} }
	profile := service.profiles["test"]
	profile.Download = Command{Program: "sh", Args: []string{"-c", "printf ok > '{localPath}'"}}
	service.profiles["test"] = profile
	execCommandContext = oldRunner
	if recorder := request(service, http.MethodGet, "/api/sessions/"+session.ID+"/download?path=/tmp/file", session.Token, nil); recorder.Code != http.StatusOK || recorder.Body.String() != "ok" {
		t.Fatalf("download: %d %q", recorder.Code, recorder.Body.String())
	}
	profile.MaxBytes = 0
	service.profiles["test"] = profile
	if recorder := request(service, http.MethodGet, "/api/sessions/"+session.ID+"/download?path=/tmp/file", session.Token, nil); recorder.Code != http.StatusOK {
		t.Fatalf("default-size download: %d %q", recorder.Code, recorder.Body.String())
	}
	oldProfile := service.profiles["test"]
	oldProfile.Upload = Command{Program: "upload"}
	service.profiles["test"] = oldProfile
	execCommandContext = func(context.Context, string, ...string) commandRunner { return fakeRunner{output: []byte("ok")} }
	if recorder := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/upload?path=/tmp/file&filename=data", session.Token, strings.NewReader("data")); recorder.Code != http.StatusOK {
		t.Fatalf("upload: %d %q", recorder.Code, recorder.Body.String())
	}
	execCommandContext = func(context.Context, string, ...string) commandRunner {
		return fakeRunner{err: errors.New("failed"), output: []byte(" command failed ")}
	}
	if recorder := request(service, http.MethodGet, "/api/sessions/"+session.ID+"/download?path=/tmp/file", session.Token, nil); recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "command failed") {
		t.Fatalf("failed transfer: %d %q", recorder.Code, recorder.Body.String())
	}
	execCommandContext = func(context.Context, string, ...string) commandRunner { return fakeRunner{err: errors.New("failed")} }
	if recorder := request(service, http.MethodGet, "/api/sessions/"+session.ID+"/download?path=/tmp/file", session.Token, nil); recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "failed") {
		t.Fatalf("empty failed transfer: %d %q", recorder.Code, recorder.Body.String())
	}

	agent := &testAgent{reads: make(chan []byte)}
	shared := &sharedAgent{agent: agent, subscribers: make(map[*sharedSubscription]struct{})}
	connection := shared.subscribe()
	service.agents[session.ID] = shared
	controller := &blockingController{stop: make(chan struct{})}
	agent.onWrite = func(data []byte) {
		text := string(data)
		start := strings.Index(text, "rterm-done;") + len("rterm-done;")
		end := strings.Index(text[start:], ";%s")
		service.AppendOutput(session.ID, []byte("command output\n\x1b]9;rterm-done;"+text[start:start+end]+";0\x07"))
	}
	ttyValue := tty.New(controller, func() (tty.Agent, error) { return connection, nil }, 1024, 1024)
	ttyValue.WithWrite(true)
	service.AttachTerminal(session.ID, ttyValue)
	go ttyValue.Run(context.Background())
	time.Sleep(10 * time.Millisecond)
	if recorder := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/execute", session.Token, strings.NewReader(`{"command":"echo hi"}`)); recorder.Code != http.StatusOK {
		t.Fatalf("execute status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	agent.onWrite = func([]byte) {}
	canceledRequest := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID+"/execute", strings.NewReader(`{"command":"echo hi"}`))
	canceledContext, cancel := context.WithCancel(canceledRequest.Context())
	cancel()
	canceledRequest = canceledRequest.WithContext(canceledContext)
	canceledRequest.Header.Set("Authorization", "Bearer "+session.Token)
	canceledResponse := httptest.NewRecorder()
	service.Handler().ServeHTTP(canceledResponse, canceledRequest)
	if canceledResponse.Code != http.StatusRequestTimeout {
		t.Fatalf("canceled execute status: %d", canceledResponse.Code)
	}
	agent.writeErr = errors.New("write failed")
	if writeFailure := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/execute", session.Token, strings.NewReader(`{"command":"echo hi"}`)); writeFailure.Code != http.StatusConflict {
		t.Fatalf("write failure execute status: %d", writeFailure.Code)
	}
	close(controller.stop)
	close(agent.reads)
}

type fakeRunner struct {
	output []byte
	err    error
}

func (r fakeRunner) CombinedOutput() ([]byte, error) { return r.output, r.err }

type blockingController struct{ stop chan struct{} }

func (c *blockingController) Read([]byte) (int, error)       { <-c.stop; return 0, io.EOF }
func (c *blockingController) Write(data []byte) (int, error) { return len(data), nil }

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestHandleEventsWebSocketAndSelection(t *testing.T) {
	service, session := testService(t)
	otherSession, err := service.CreateSession(context.Background(), "test", SessionRequest{Target: "node-1", User: "root"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.HandleEventsWebSocket(&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}, w, r)
	}))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/?session=" + url.QueryEscape(session.ID) + "&token=" + url.QueryEscape(session.Token)
	header := http.Header{}
	header.Set("Authorization", "Bearer "+session.Token)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var event map[string]any
	if err := conn.ReadJSON(&event); err != nil || event["type"] != "selected" {
		t.Fatalf("initial event: %#v %v", event, err)
	}
	if err := conn.WriteJSON(map[string]string{"type": "new-session"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&event); err != nil || event["type"] != "new-session" {
		t.Fatalf("new event: %#v %v", event, err)
	}
	if err := conn.WriteJSON(map[string]string{"type": "close", "sessionId": "missing"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]string{"type": "close", "sessionId": otherSession.ID}); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&event); err != nil || event["type"] != "closed" || event["sessionId"] != otherSession.ID {
		t.Fatalf("closed event: %#v %v", event, err)
	}
	if err := conn.WriteJSON(map[string]string{"type": "select", "sessionId": session.ID}); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&event); err != nil || event["type"] != "selected" {
		t.Fatalf("selected event: %#v %v", event, err)
	}
	if _, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/?session=missing&token=x", nil); err == nil {
		t.Fatal("unauthorized websocket connected")
	}
}

func TestStreamFileAndHelpers(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := streamFile(recorder, file, 4); err != nil || recorder.Body.String() != "data" || recorder.Header().Get("Content-Length") != "4" {
		t.Fatalf("stream: %v %q %#v", err, recorder.Body.String(), recorder.Header())
	}
	oversized := httptest.NewRecorder()
	if err := streamFile(oversized, file, 3); err != nil || oversized.Code != http.StatusBadGateway {
		t.Fatalf("unexpected oversized stream result: %v, status %d", err, oversized.Code)
	}
	if err := streamFile(httptest.NewRecorder(), filepath.Join(t.TempDir(), "missing"), 10); err == nil {
		t.Fatal("missing stream succeeded")
	}
	if fileSize(file) != 4 || fileSize(filepath.Join(t.TempDir(), "missing")) != 0 {
		t.Fatal("unexpected file size")
	}
	if !contains([]string{"a", "b"}, "b") || contains([]string{"a"}, "b") {
		t.Fatal("contains failed")
	}
	if got := outputSinceForTest(); got != "" {
		t.Fatal(got)
	}
}

func outputSinceForTest() string {
	service := NewService(Config{})
	return service.outputSince("missing", 0)
}

func TestSharedAgentReadVariantsAndReadLoopShutdown(t *testing.T) {
	shared := &sharedAgent{subscribers: make(map[*sharedSubscription]struct{})}
	connection := shared.subscribe()
	connection.subscription.output <- []byte("abc")
	buffer := make([]byte, 2)
	if n, err := connection.Read(buffer); err != nil || n != 2 || string(buffer) != "ab" {
		t.Fatalf("partial read: %d %v %q", n, err, buffer)
	}
	if n, err := connection.Read(buffer); err != nil || n != 1 || string(buffer[:1]) != "c" {
		t.Fatalf("pending read: %d %v %q", n, err, buffer)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}

	agent := &testAgent{readErr: io.EOF, reads: make(chan []byte)}
	service := NewService(Config{})
	value, err := service.SharedAgent("eof", func() (tty.Agent, error) { return agent, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := value.Read(make([]byte, 1)); err == io.EOF {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("read loop did not close subscribers")
}

func TestServiceDirectErrorBranches(t *testing.T) {
	service, session := testService(t)
	service.selectSession(session.ID, "missing")
	if got := service.outputSince(session.ID, 999999); got != service.output[session.ID] {
		t.Fatal("outputSince did not handle an oversized offset")
	}
	missingRead := httptest.NewRecorder()
	service.read(missingRead, httptest.NewRequest(http.MethodGet, "/", nil), "missing")
	if missingRead.Code != http.StatusNotFound {
		t.Fatalf("missing read status: %d", missingRead.Code)
	}
	missingTransfer := httptest.NewRecorder()
	service.transfer(missingTransfer, httptest.NewRequest(http.MethodGet, "/", nil), "missing", false)
	if missingTransfer.Code != http.StatusNotFound {
		t.Fatalf("missing transfer status: %d", missingTransfer.Code)
	}
	service.sessions[session.ID] = Session{ID: session.ID, Provider: "gone"}
	providerMissing := httptest.NewRecorder()
	service.transfer(providerMissing, httptest.NewRequest(http.MethodGet, "/?path=x", nil), session.ID, false)
	if providerMissing.Code != http.StatusBadGateway {
		t.Fatalf("missing provider status: %d", providerMissing.Code)
	}
	service.sessions[session.ID] = session
	profile := service.profiles["test"]
	profile.Download = Command{Program: "true", Args: []string{"{unknown}"}}
	service.profiles["test"] = profile
	badArgs := httptest.NewRecorder()
	service.transfer(badArgs, httptest.NewRequest(http.MethodGet, "/?path=x", nil), session.ID, false)
	if badArgs.Code != http.StatusInternalServerError {
		t.Fatalf("unresolved transfer status: %d", badArgs.Code)
	}

	invalidHandoff := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/handoff", "", strings.NewReader(`{"handoff":"bad"}`))
	if invalidHandoff.Code != http.StatusUnauthorized {
		t.Fatalf("invalid handoff status: %d", invalidHandoff.Code)
	}
	expired := newToken()
	service.handoffs[expired] = sessionHandoff{sessionID: session.ID, token: session.Token, expiresAt: time.Now().Add(-time.Second)}
	expiredHandoff := request(service, http.MethodPost, "/api/sessions/"+session.ID+"/handoff", "", strings.NewReader(`{"handoff":"`+expired+`"}`))
	if expiredHandoff.Code != http.StatusUnauthorized {
		t.Fatalf("expired handoff status: %d", expiredHandoff.Code)
	}
}

func TestHandlerDiscoveryAndSessionErrors(t *testing.T) {
	service, _ := testService(t)
	profile := service.profiles["test"]
	profile.Discover.Program = "sh"
	profile.Discover.Args = []string{"-c", "exit 1"}
	service.profiles["test"] = profile
	if recorder := request(service, http.MethodGet, "/api/providers/test/targets", "", nil); recorder.Code != http.StatusBadGateway {
		t.Fatalf("discovery error status: %d", recorder.Code)
	}
	if recorder := request(service, http.MethodPost, "/api/providers/test/sessions", "", strings.NewReader(`{"target":"x","user":"root"}`)); recorder.Code != http.StatusBadRequest {
		t.Fatalf("session discovery error status: %d", recorder.Code)
	}
	if recorder := request(service, http.MethodGet, "/api/providers", "", nil); recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatal("providers response is not JSON")
	}
}

func TestEventsUpgradeFailure(t *testing.T) {
	service, session := testService(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/?session="+session.ID, nil)
	request.Header.Set("Authorization", "Bearer "+session.Token)
	service.HandleEventsWebSocket(&websocket.Upgrader{}, response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("upgrade failure status: %d", response.Code)
	}
}
