package provider

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	pathpkg "path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dev6699/rterm/tty"
	"github.com/gorilla/websocket"
)

// Session contains the credentials and target metadata for a provider session.
type Session struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Target   string `json:"target"`
	User     string `json:"user"`
	Token    string `json:"token,omitempty"`
	Handoff  string `json:"handoff,omitempty"`
}

func newToken() string {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		panic(fmt.Sprintf("provider session token: %v", err))
	}
	return fmt.Sprintf("%x", value)
}

// ExecuteRequest contains a shell command to run in a session terminal.
type ExecuteRequest struct {
	Command string `json:"command"`
}

// ExecuteResponse contains command output and its exit status.
type ExecuteResponse struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
}

// SessionRequest identifies the target and user for a new provider session.
type SessionRequest struct {
	Target string `json:"target"`
	User   string `json:"user"`
}

type sessionHandoff struct {
	sessionID string
	token     string
	expiresAt time.Time
}

// Service manages provider sessions, terminal output, transfers, and events.
type Service struct {
	profiles  map[string]Profile
	sessions  map[string]Session
	mu        sync.RWMutex
	output    map[string]string
	terminals map[string]*tty.TTY
	executeMu map[string]*sync.Mutex
	handoffs  map[string]sessionHandoff
	agents    map[string]*sharedAgent
	events    map[*websocket.Conn]*eventClient
}

type eventClient struct {
	sessionID          string
	authorizedSessions map[string]struct{}
	writeMu            sync.Mutex
}

// NewService creates a provider service from config.
func NewService(config Config) *Service {
	profiles := make(map[string]Profile, len(config.Providers))
	for _, profile := range config.Providers {
		profiles[profile.Name] = profile
	}
	return &Service{profiles: profiles, sessions: make(map[string]Session), output: make(map[string]string), terminals: make(map[string]*tty.TTY), executeMu: make(map[string]*sync.Mutex), handoffs: make(map[string]sessionHandoff), agents: make(map[string]*sharedAgent), events: make(map[*websocket.Conn]*eventClient)}
}

// HandleEventsWebSocket upgrades an authorized request and serves session events.
func (s *Service) HandleEventsWebSocket(upgrader *websocket.Upgrader, w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" || !s.AuthorizeSession(r, sessionID) {
		http.Error(w, "unauthorized session", http.StatusUnauthorized)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.mu.Lock()
	client := &eventClient{
		sessionID:          sessionID,
		authorizedSessions: map[string]struct{}{sessionID: {}},
	}
	s.events[conn] = client
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.events, conn)
		s.mu.Unlock()
		client.writeMu.Lock()
		_ = conn.Close()
		client.writeMu.Unlock()
	}()
	client.writeMu.Lock()
	_ = conn.WriteJSON(map[string]any{"type": "selected", "sessionId": sessionID})
	client.writeMu.Unlock()
	for {
		var message struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionId"`
			Token     string `json:"token"`
		}
		if err := conn.ReadJSON(&message); err != nil {
			return
		}
		switch message.Type {
		case "new-session":
			s.broadcastEvent(sessionID, map[string]any{"type": "new-session"})
		case "close":
			if s.authorizeEventSession(client, message.SessionID, message.Token) {
				s.broadcastEvent(sessionID, map[string]any{"type": "closed", "sessionId": message.SessionID})
			}
		case "select":
			s.selectSession(client, message.SessionID, message.Token)
		}
	}
}

func (s *Service) broadcastEvent(sessionID string, message any) {
	s.mu.Lock()
	clients := make([]struct {
		conn   *websocket.Conn
		client *eventClient
	}, 0, len(s.events))
	for conn, client := range s.events {
		if client.sessionID == sessionID {
			clients = append(clients, struct {
				conn   *websocket.Conn
				client *eventClient
			}{conn: conn, client: client})
		}
	}
	s.mu.Unlock()
	for _, client := range clients {
		client.client.writeMu.Lock()
		_ = client.conn.WriteJSON(message)
		client.client.writeMu.Unlock()
	}
}

func (s *Service) selectSession(client *eventClient, sessionID, token string) {
	if !s.authorizeEventSession(client, sessionID, token) {
		return
	}
	s.broadcastEvent(client.sessionID, map[string]any{"type": "selected", "sessionId": sessionID})
}

func (s *Service) authorizeEventSession(client *eventClient, sessionID, token string) bool {
	if _, ok := client.authorizedSessions[sessionID]; ok {
		return true
	}
	if !s.authorizeSessionToken(sessionID, token) {
		return false
	}
	client.authorizedSessions[sessionID] = struct{}{}
	return true
}

type sharedAgent struct {
	mu          sync.Mutex
	agent       tty.Agent
	subscribers map[*sharedSubscription]struct{}
	onOutput    func([]byte)
	onEmpty     func()
}

// SharedAgent returns a terminal connection sharing the agent for sessionID.
func (s *Service) SharedAgent(sessionID string, factory func() (tty.Agent, error), onOutput func([]byte)) (tty.Agent, error) {
	s.mu.Lock()
	shared := s.agents[sessionID]
	created := false
	if shared == nil {
		agent, err := factory()
		if err != nil {
			s.mu.Unlock()
			return nil, err
		}
		shared = &sharedAgent{agent: agent, subscribers: make(map[*sharedSubscription]struct{}), onOutput: onOutput}
		shared.onEmpty = func() {
			s.mu.Lock()
			if s.agents[sessionID] == shared {
				delete(s.agents, sessionID)
			}
			s.mu.Unlock()
		}
		s.agents[sessionID] = shared
		created = true
	}
	connection := shared.subscribe()
	s.mu.Unlock()
	if created {
		go shared.readLoop()
	}
	return connection, nil
}

type sharedAgentConnection struct {
	shared       *sharedAgent
	subscription *sharedSubscription
	pending      []byte
	closeOnce    sync.Once
}

type sharedSubscription struct {
	output chan []byte
	done   chan struct{}
}

func (c *sharedAgentConnection) Read(data []byte) (int, error) {
	var chunk []byte
	if len(c.pending) > 0 {
		chunk = c.pending
		c.pending = nil
	} else {
		select {
		case chunk = <-c.subscription.output:
		case <-c.subscription.done:
			return 0, io.EOF
		}
	}
	n := copy(data, chunk)
	if n < len(chunk) {
		c.pending = append(c.pending, chunk[n:]...)
	}
	return n, nil
}

func (c *sharedAgentConnection) Write(data []byte) (int, error) {
	c.shared.mu.Lock()
	defer c.shared.mu.Unlock()
	return c.shared.agent.Write(data)
}

func (c *sharedAgentConnection) ResizeTerminal(columns int, rows int) error {
	c.shared.mu.Lock()
	defer c.shared.mu.Unlock()
	return c.shared.agent.ResizeTerminal(columns, rows)
}

func (c *sharedAgentConnection) Close() error {
	c.closeOnce.Do(func() {
		c.shared.removeSubscriber(c.subscription)
	})
	return nil
}

func (s *sharedAgent) subscribe() *sharedAgentConnection {
	subscription := &sharedSubscription{output: make(chan []byte, 32), done: make(chan struct{})}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribers[subscription] = struct{}{}
	return &sharedAgentConnection{shared: s, subscription: subscription}
}

func (s *sharedAgent) readLoop() {
	buffer := make([]byte, 32*1024)
	for {
		n, err := s.agent.Read(buffer)
		if n > 0 {
			chunk := append([]byte(nil), buffer[:n]...)
			s.mu.Lock()
			onOutput := s.onOutput
			subscribers := make([]*sharedSubscription, 0, len(s.subscribers))
			for subscriber := range s.subscribers {
				subscribers = append(subscribers, subscriber)
			}
			s.mu.Unlock()
			if onOutput != nil {
				onOutput(chunk)
			}
			for _, subscriber := range subscribers {
				select {
				case subscriber.output <- chunk:
				case <-subscriber.done:
				default:
					// Evict stalled subscribers so they cannot block other clients.
					s.removeSubscriber(subscriber)
				}
			}
		}
		if err != nil {
			s.mu.Lock()
			for subscriber := range s.subscribers {
				close(subscriber.done)
			}
			s.subscribers = make(map[*sharedSubscription]struct{})
			s.mu.Unlock()
			if s.onEmpty != nil {
				s.onEmpty()
			}
			return
		}
	}
}

func (s *sharedAgent) removeSubscriber(subscriber *sharedSubscription) {
	closeAgent := false
	s.mu.Lock()
	if _, ok := s.subscribers[subscriber]; ok {
		delete(s.subscribers, subscriber)
		close(subscriber.done)
		closeAgent = len(s.subscribers) == 0
	}
	s.mu.Unlock()
	if closeAgent {
		if closer, ok := s.agent.(io.Closer); ok {
			_ = closer.Close()
		}
		if s.onEmpty != nil {
			s.onEmpty()
		}
	}
}

// Profiles returns configured provider names in sorted order.
func (s *Service) Profiles() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]string, 0, len(s.profiles))
	for name := range s.profiles {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// Profile returns a configured provider by name.
func (s *Service) Profile(name string) (Profile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, ok := s.profiles[name]
	return profile, ok
}

// Session returns a managed session by ID.
func (s *Service) Session(id string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	return session, ok
}

// CreateSession discovers and validates a target before creating its session.
func (s *Service) CreateSession(ctx context.Context, profileName string, request SessionRequest) (Session, error) {
	profile, ok := s.Profile(profileName)
	if !ok {
		return Session{}, fmt.Errorf("unknown provider %q", profileName)
	}
	discovery, err := profile.DiscoverTargets(ctx)
	if err != nil {
		return Session{}, err
	}
	target, ok := profile.Target(request.Target, discovery)
	if !ok {
		return Session{}, fmt.Errorf("target %q is not available", request.Target)
	}
	if !contains(target.Users, request.User) {
		return Session{}, fmt.Errorf("user %q is not available for target %q", request.User, request.Target)
	}
	session := Session{ID: newID(), Token: newToken(), Provider: profileName, Target: target.ID, User: request.User}
	s.mu.Lock()
	s.sessions[session.ID] = session
	s.output[session.ID] = ""
	s.executeMu[session.ID] = &sync.Mutex{}
	s.mu.Unlock()
	return session, nil
}

// AuthorizeSession verifies a bearer token for a session request.
func (s *Service) AuthorizeSession(r *http.Request, sessionID string) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == r.Header.Get("Authorization") && strings.HasSuffix(r.URL.Path, "/ws") {
		token = r.URL.Query().Get("token")
	}
	return s.authorizeSessionToken(sessionID, token)
}

func (s *Service) authorizeSessionToken(sessionID, token string) bool {
	session, ok := s.Session(sessionID)
	return ok && token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(session.Token)) == 1
}

// AttachTerminal associates a running TTY with an existing session.
func (s *Service) AttachTerminal(sessionID string, terminal *tty.TTY) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sessionID]; !ok {
		return false
	}
	s.terminals[sessionID] = terminal
	return true
}

// AppendOutput stores terminal output for an existing session.
func (s *Service) AppendOutput(sessionID string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sessionID]; !ok {
		return
	}
	s.output[sessionID] += string(data)
	if len(s.output[sessionID]) > 256*1024 {
		s.output[sessionID] = s.output[sessionID][len(s.output[sessionID])-256*1024:]
	}
}

// ReadOutput returns recent output and whether older lines were truncated.
func (s *Service) ReadOutput(sessionID string, maxLines int) (string, bool) {
	s.mu.RLock()
	output, ok := s.output[sessionID]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}
	lines := strings.Split(output, "\n")
	if maxLines < 1 || maxLines > 200 {
		maxLines = 200
	}
	truncated := len(lines) > maxLines
	if truncated {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n"), truncated
}

// ConnectArgs expands the configured connection command for a session.
func (s *Service) ConnectArgs(sessionID string) (string, []string, error) {
	session, ok := s.Session(sessionID)
	if !ok {
		return "", nil, fmt.Errorf("unknown session %q", sessionID)
	}
	profile, ok := s.Profile(session.Provider)
	if !ok {
		return "", nil, fmt.Errorf("unknown provider %q", session.Provider)
	}
	args, err := expandArgs(profile.Connect.Args, map[string]string{
		"target": session.Target,
		"user":   session.User,
	})
	return profile.Connect.Program, args, err
}

// Handler returns the HTTP handler for provider and session APIs.
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 1 && parts[0] == "providers" && r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]any{"providers": s.Profiles()})
			return
		}
		if len(parts) == 3 && parts[0] == "providers" && parts[2] == "targets" && r.Method == http.MethodGet {
			profile, ok := s.Profile(parts[1])
			if !ok {
				http.NotFound(w, r)
				return
			}
			discovery, err := profile.DiscoverTargets(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			writeJSON(w, http.StatusOK, discovery)
			return
		}
		if len(parts) == 3 && parts[0] == "providers" && parts[2] == "sessions" && r.Method == http.MethodPost {
			var request SessionRequest
			if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&request); err != nil {
				http.Error(w, "invalid session request", http.StatusBadRequest)
				return
			}
			session, err := s.CreateSession(r.Context(), parts[1], request)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if r.Header.Get("X-Rterm-Handoff") == "1" {
				handoff := newToken()
				s.mu.Lock()
				s.handoffs[handoff] = sessionHandoff{
					sessionID: session.ID,
					token:     session.Token,
					expiresAt: time.Now().Add(time.Minute),
				}
				s.mu.Unlock()
				session.Handoff = handoff
			}
			writeJSON(w, http.StatusCreated, session)
			return
		}
		if len(parts) == 3 && parts[0] == "sessions" && parts[2] == "handoff" && r.Method == http.MethodPost {
			var request struct {
				Handoff string `json:"handoff"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 8*1024)).Decode(&request); err != nil {
				http.Error(w, "invalid handoff", http.StatusBadRequest)
				return
			}
			s.mu.Lock()
			handoff, ok := s.handoffs[request.Handoff]
			if ok && handoff.sessionID == parts[1] && time.Now().Before(handoff.expiresAt) {
				delete(s.handoffs, request.Handoff)
			}
			s.mu.Unlock()
			if !ok || handoff.sessionID != parts[1] || !time.Now().Before(handoff.expiresAt) {
				http.Error(w, "invalid handoff", http.StatusUnauthorized)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"token": handoff.token})
			return
		}
		if len(parts) == 3 && parts[0] == "sessions" && parts[2] == "share" && r.Method == http.MethodPost {
			if !s.authorize(w, r, parts[1]) {
				return
			}
			session, ok := s.Session(parts[1])
			if !ok {
				http.NotFound(w, r)
				return
			}
			handoff := newToken()
			s.mu.Lock()
			s.handoffs[handoff] = sessionHandoff{
				sessionID: session.ID,
				token:     session.Token,
				expiresAt: time.Now().Add(time.Minute),
			}
			s.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]string{"handoff": handoff})
			return
		}
		if len(parts) == 3 && parts[0] == "sessions" && parts[2] == "upload" && r.Method == http.MethodPost {
			if !s.authorize(w, r, parts[1]) {
				return
			}
			s.transfer(w, r, parts[1], true)
			return
		}
		if len(parts) == 3 && parts[0] == "sessions" && parts[2] == "execute" && r.Method == http.MethodPost {
			if !s.authorize(w, r, parts[1]) {
				return
			}
			s.execute(w, r, parts[1])
			return
		}
		if len(parts) == 3 && parts[0] == "sessions" && parts[2] == "read" && r.Method == http.MethodGet {
			if !s.authorize(w, r, parts[1]) {
				return
			}
			s.read(w, r, parts[1])
			return
		}
		if len(parts) == 3 && parts[0] == "sessions" && parts[2] == "download" && r.Method == http.MethodGet {
			if !s.authorize(w, r, parts[1]) {
				return
			}
			s.transfer(w, r, parts[1], false)
			return
		}
		http.NotFound(w, r)
	})
}

func (s *Service) authorize(w http.ResponseWriter, r *http.Request, sessionID string) bool {
	if s.AuthorizeSession(r, sessionID) {
		return true
	}
	http.Error(w, "unauthorized session", http.StatusUnauthorized)
	return false
}

func (s *Service) execute(w http.ResponseWriter, r *http.Request, sessionID string) {
	if _, ok := s.Session(sessionID); !ok {
		http.NotFound(w, r)
		return
	}
	var request ExecuteRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&request); err != nil || strings.TrimSpace(request.Command) == "" {
		http.Error(w, "command is required", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	terminal := s.terminals[sessionID]
	mutex := s.executeMu[sessionID]
	s.mu.RUnlock()
	if terminal == nil || mutex == nil {
		http.Error(w, "terminal is not connected", http.StatusConflict)
		return
	}
	mutex.Lock()
	defer mutex.Unlock()
	s.mu.RLock()
	start := len(s.output[sessionID])
	s.mu.RUnlock()
	marker := newID()
	wrapper := fmt.Sprintf("{ %s\n}; status=$?; printf '\\033]9;rterm-done;%s;%%s\\007' \"$status\"\n", request.Command, marker)
	if err := terminal.WriteInput([]byte(wrapper)); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	deadline := time.NewTimer(120 * time.Second)
	defer deadline.Stop()
	for {
		output := s.outputSince(sessionID, start)
		if index := strings.Index(output, "\x1b]9;rterm-done;"+marker+";"); index >= 0 {
			end := strings.Index(output[index:], "\x07")
			if end >= 0 {
				markerEnd := index + end
				statusText := output[index+len("\x1b]9;rterm-done;"+marker+";") : markerEnd]
				status, _ := strconv.Atoi(statusText)
				writeJSON(w, http.StatusOK, ExecuteResponse{Output: output[:index], ExitCode: status})
				return
			}
		}
		select {
		case <-r.Context().Done():
			http.Error(w, r.Context().Err().Error(), http.StatusRequestTimeout)
			return
		case <-deadline.C:
			http.Error(w, "command did not finish within 120 seconds", http.StatusGatewayTimeout)
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (s *Service) outputSince(sessionID string, start int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	output := s.output[sessionID]
	if start > len(output) {
		return output
	}
	return output[start:]
}

func (s *Service) read(w http.ResponseWriter, r *http.Request, sessionID string) {
	if _, ok := s.Session(sessionID); !ok {
		http.NotFound(w, r)
		return
	}
	maxLines := 200
	if value := r.URL.Query().Get("maxLines"); value != "" {
		if _, err := fmt.Sscanf(value, "%d", &maxLines); err != nil {
			http.Error(w, "invalid maxLines", http.StatusBadRequest)
			return
		}
	}
	output, truncated := s.ReadOutput(sessionID, maxLines)
	writeJSON(w, http.StatusOK, map[string]any{"output": output, "truncated": truncated})
}

func (s *Service) transfer(w http.ResponseWriter, r *http.Request, sessionID string, upload bool) {
	session, ok := s.Session(sessionID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	profile, ok := s.Profile(session.Provider)
	if !ok {
		http.Error(w, "provider is unavailable", http.StatusBadGateway)
		return
	}
	remotePath := r.URL.Query().Get("path")
	if remotePath == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	if upload {
		resolved, resolveErr := resolveUploadPath(remotePath, r.URL.Query().Get("filename"))
		if resolveErr != nil {
			http.Error(w, resolveErr.Error(), http.StatusBadRequest)
			return
		}
		remotePath = resolved
	}
	maxBytes := profile.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 4 << 30
	}
	if upload && r.ContentLength > maxBytes {
		http.Error(w, "transfer exceeds configured limit", http.StatusRequestEntityTooLarge)
		return
	}
	temporary, err := os.CreateTemp("", "rterm-transfer-*")
	if err != nil {
		http.Error(w, "unable to create transfer file", http.StatusInternalServerError)
		return
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if upload {
		limited := io.LimitReader(r.Body, maxBytes+1)
		written, copyErr := io.Copy(temporary, limited)
		if copyErr != nil || written > maxBytes {
			http.Error(w, "unable to receive transfer", http.StatusRequestEntityTooLarge)
			return
		}
		if _, err := temporary.Seek(0, 0); err != nil {
			http.Error(w, "unable to prepare transfer", http.StatusInternalServerError)
			return
		}
	}
	if err := temporary.Close(); err != nil {
		http.Error(w, "unable to prepare transfer", http.StatusInternalServerError)
		return
	}
	command := profile.Download
	if upload {
		command = profile.Upload
	}
	args, err := expandArgs(command.Args, map[string]string{
		"target":     session.Target,
		"user":       session.User,
		"remotePath": remotePath,
		"localPath":  temporaryPath,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Hour)
	defer cancel()
	process := execCommandContext(ctx, command.Program, args...)
	output, err := process.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		http.Error(w, message, http.StatusBadGateway)
		return
	}
	if upload {
		writeJSON(w, http.StatusOK, map[string]any{"status": "completed", "bytes": fileSize(temporaryPath)})
		return
	}
	if err := streamFile(w, temporaryPath, maxBytes); err != nil {
		return
	}
}

func resolveUploadPath(remotePath, filename string) (string, error) {
	base := pathpkg.Base(filename)
	if base == "." || base == "/" || base == "" {
		return "", fmt.Errorf("filename is required for uploads")
	}
	sourceExt := pathpkg.Ext(base)
	destinationBase := pathpkg.Base(strings.TrimRight(remotePath, "/"))
	destinationExt := pathpkg.Ext(destinationBase)
	if strings.HasSuffix(remotePath, "/") || destinationExt == "" {
		return strings.TrimRight(remotePath, "/") + "/" + base, nil
	}
	if !strings.EqualFold(destinationExt, sourceExt) {
		return "", fmt.Errorf("destination extension %q must match upload extension %q", destinationExt, sourceExt)
	}
	return remotePath, nil
}

var execCommandContext = func(ctx context.Context, program string, args ...string) commandRunner {
	return &processRunner{command: exec.CommandContext(ctx, program, args...)}
}

type commandRunner interface {
	CombinedOutput() ([]byte, error)
}

type processRunner struct{ command *exec.Cmd }

func (p *processRunner) CombinedOutput() ([]byte, error) { return p.command.CombinedOutput() }

func streamFile(w http.ResponseWriter, path string, maxBytes int64) error {
	file, err := os.Open(path)
	if err != nil {
		http.Error(w, "transfer output is unavailable", http.StatusBadGateway)
		return err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || info.Size() > maxBytes {
		http.Error(w, "transfer exceeds configured limit", http.StatusBadGateway)
		return err
	} else {
		w.Header().Set("Content-Length", fmt.Sprint(info.Size()))
	}
	_, err = io.Copy(w, file)
	return err
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newID() string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("transfer-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", data)
}
