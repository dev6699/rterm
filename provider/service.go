package provider

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	RoomID   string `json:"-"`
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

// Service manages provider sessions, terminal output, transfers, and events.
type Service struct {
	profiles     map[string]Profile
	sessions     map[string]Session
	mu           sync.RWMutex
	output       map[string]string
	terminals    map[string]*tty.TTY
	executeMu    map[string]*sync.Mutex
	agents       map[string]*sharedAgent
	events       map[*websocket.Conn]*eventClient
	eventSeq     uint64
	activeRooms  map[string]string
	lastActivity map[string]time.Time
}

const sessionTTL = 30 * time.Minute

type eventClient struct {
	roomID             string
	authorizedSessions map[string]struct{}
	writeMu            sync.Mutex
	initializing       bool
	pending            []any
}

// NewService creates a provider service from config.
func NewService(config Config) *Service {
	profiles := make(map[string]Profile, len(config.Providers))
	for _, profile := range config.Providers {
		profiles[profile.Name] = profile
	}
	service := &Service{profiles: profiles, sessions: make(map[string]Session), output: make(map[string]string), terminals: make(map[string]*tty.TTY), executeMu: make(map[string]*sync.Mutex), agents: make(map[string]*sharedAgent), events: make(map[*websocket.Conn]*eventClient), activeRooms: make(map[string]string), lastActivity: make(map[string]time.Time)}
	go service.expireSessions()
	return service
}

func (s *Service) expireSessions() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for now := range ticker.C {
		expired := make([]struct {
			id, room string
			terminal *tty.TTY
			agent    *sharedAgent
		}, 0)
		s.mu.Lock()
		for id, lastActivity := range s.lastActivity {
			if now.Sub(lastActivity) < sessionTTL {
				continue
			}
			session, ok := s.sessions[id]
			if !ok {
				delete(s.lastActivity, id)
				continue
			}
			delete(s.sessions, id)
			delete(s.output, id)
			terminal := s.terminals[id]
			delete(s.terminals, id)
			delete(s.executeMu, id)
			agent := s.agents[id]
			delete(s.agents, id)
			delete(s.lastActivity, id)
			if s.activeRooms[session.RoomID] == id {
				delete(s.activeRooms, session.RoomID)
			}
			expired = append(expired, struct {
				id, room string
				terminal *tty.TTY
				agent    *sharedAgent
			}{id, session.RoomID, terminal, agent})
		}
		for _, item := range expired {
			if s.activeRooms[item.room] == "" {
				for id, session := range s.sessions {
					if session.RoomID == item.room {
						s.activeRooms[item.room] = id
						break
					}
				}
			}
		}
		s.mu.Unlock()
		for _, item := range expired {
			log.Printf("provider events: session expired room=%q target=%q", item.room, item.id)
			if item.terminal != nil {
				_ = item.terminal.Close()
			}
			if item.agent != nil {
				item.agent.close()
			}
			if item.room == "" {
				continue
			}
			s.broadcastEvent(item.room, map[string]any{"type": "closed", "sessionId": item.id})
			if activeID := s.activeRoom(item.room); activeID != "" {
				s.broadcastEvent(item.room, map[string]any{"type": "selected", "sessionId": activeID})
			}
		}
	}
}

func (s *Service) activeRoom(roomID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeRooms[roomID]
}

// HandleEventsWebSocket upgrades an authorized request and serves session events.
func (s *Service) HandleEventsWebSocket(upgrader *websocket.Upgrader, w http.ResponseWriter, r *http.Request) {
	roomID := r.URL.Query().Get("roomId")
	if roomID == "" {
		log.Printf("provider events: websocket rejected room=%q", roomID)
		http.Error(w, "unauthorized session", http.StatusUnauthorized)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.mu.Lock()
	client := &eventClient{
		roomID:             roomID,
		authorizedSessions: make(map[string]struct{}),
		initializing:       true,
	}
	initialEvents := make([]map[string]any, 0)
	ids := make([]string, 0)
	for id, session := range s.sessions {
		if session.RoomID == roomID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		session := s.sessions[id]
		s.eventSeq++
		initialEvents = append(initialEvents, map[string]any{
			"type": "new-session", "sessionId": session.ID, "token": session.Token,
			"target": session.Target, "user": session.User, "sequence": s.eventSeq,
		})
	}
	activeID := s.activeRooms[roomID]
	if activeID == "" && len(ids) > 0 {
		activeID = ids[len(ids)-1]
		s.activeRooms[roomID] = activeID
	}
	if activeID != "" {
		s.eventSeq++
		initialEvents = append(initialEvents, map[string]any{"type": "selected", "sessionId": activeID, "sequence": s.eventSeq})
	}
	s.events[conn] = client
	s.mu.Unlock()
	log.Printf("provider events: websocket authorized room=%q", roomID)
	for _, event := range initialEvents {
		client.writeMu.Lock()
		_ = conn.WriteJSON(event)
		client.writeMu.Unlock()
	}
	// Broadcasts that occurred while the initial snapshot was being written are
	// queued by broadcastEvent. Acquire the write lock before making the client
	// ready so a later broadcast cannot overtake the queued events.
	client.writeMu.Lock()
	s.mu.Lock()
	client.initializing = false
	pendingEvents := client.pending
	client.pending = nil
	s.mu.Unlock()
	for _, event := range pendingEvents {
		_ = conn.WriteJSON(event)
	}
	client.writeMu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.events, conn)
		s.mu.Unlock()
		client.writeMu.Lock()
		_ = conn.Close()
		client.writeMu.Unlock()
	}()
	for {
		var message struct {
			Type      string `json:"type"`
			SessionID string `json:"sessionId"`
			Token     string `json:"token"`
			Target    string `json:"target"`
			User      string `json:"user"`
		}
		if err := conn.ReadJSON(&message); err != nil {
			return
		}
		switch message.Type {
		case "new-session":
			log.Printf("provider events: new-session room=%q", roomID)
			event := map[string]any{"type": "new-session"}
			if message.SessionID != "" && s.authorizeEventSession(client, message.SessionID, message.Token) {
				event["sessionId"] = message.SessionID
				event["token"] = message.Token
				event["target"] = message.Target
				event["user"] = message.User
			}
			s.broadcastEvent(roomID, event)
			if message.SessionID != "" {
				s.selectSession(client, message.SessionID, message.Token)
			}
		case "close":
			allowed := s.authorizeEventSession(client, message.SessionID, message.Token)
			log.Printf("provider events: close room=%q target=%q tokenProvided=%t allowed=%t", roomID, message.SessionID, message.Token != "", allowed)
			if allowed {
				s.mu.Lock()
				terminal := s.terminals[message.SessionID]
				agent := s.agents[message.SessionID]
				delete(s.sessions, message.SessionID)
				delete(s.output, message.SessionID)
				delete(s.terminals, message.SessionID)
				delete(s.agents, message.SessionID)
				delete(s.executeMu, message.SessionID)
				delete(s.lastActivity, message.SessionID)
				if s.activeRooms[roomID] == message.SessionID {
					delete(s.activeRooms, roomID)
					for id, session := range s.sessions {
						if id != message.SessionID && session.RoomID == roomID {
							s.activeRooms[roomID] = id
							break
						}
					}
				}
				fallback := s.activeRooms[roomID]
				s.mu.Unlock()
				if terminal != nil {
					_ = terminal.Close()
				}
				if agent != nil {
					agent.close()
				}
				s.broadcastEvent(roomID, map[string]any{"type": "closed", "sessionId": message.SessionID})
				if fallback != "" {
					s.broadcastEvent(roomID, map[string]any{"type": "selected", "sessionId": fallback})
				}
			}
		case "select":
			log.Printf("provider events: select room=%q target=%q tokenProvided=%t", roomID, message.SessionID, message.Token != "")
			s.selectSession(client, message.SessionID, message.Token)
		}
	}
}

func (s *Service) broadcastEvent(roomID string, message any) {
	s.mu.Lock()
	s.eventSeq++
	if event, ok := message.(map[string]any); ok {
		event["sequence"] = s.eventSeq
	}
	clients := make([]struct {
		conn   *websocket.Conn
		client *eventClient
	}, 0, len(s.events))
	for conn, client := range s.events {
		if client.roomID == roomID {
			if client.initializing {
				client.pending = append(client.pending, message)
				continue
			}
			clients = append(clients, struct {
				conn   *websocket.Conn
				client *eventClient
			}{conn: conn, client: client})
		}
	}
	s.mu.Unlock()
	log.Printf("provider events: broadcast room=%q recipients=%d message=%T", roomID, len(clients), message)
	for _, client := range clients {
		client.client.writeMu.Lock()
		_ = client.conn.WriteJSON(message)
		client.client.writeMu.Unlock()
	}
}

func (s *Service) selectSession(client *eventClient, sessionID, token string) {
	allowed := s.authorizeEventSession(client, sessionID, token)
	log.Printf("provider events: select room=%q target=%q tokenProvided=%t allowed=%t", client.roomID, sessionID, token != "", allowed)
	if !allowed {
		return
	}
	s.mu.Lock()
	s.activeRooms[client.roomID] = sessionID
	s.lastActivity[sessionID] = time.Now()
	s.mu.Unlock()
	s.broadcastEvent(client.roomID, map[string]any{"type": "selected", "sessionId": sessionID})
}

func (s *Service) authorizeEventSession(client *eventClient, sessionID, token string) bool {
	if session, ok := s.Session(sessionID); !ok || session.RoomID != client.roomID {
		log.Printf("provider events: room=%q target=%q denied", client.roomID, sessionID)
		return false
	}
	if _, ok := client.authorizedSessions[sessionID]; ok {
		log.Printf("provider events: room=%q target=%q already authorized", client.roomID, sessionID)
		return true
	}
	if !s.authorizeSessionToken(sessionID, token) {
		log.Printf("provider events: room=%q target=%q token denied", client.roomID, sessionID)
		return false
	}
	client.authorizedSessions[sessionID] = struct{}{}
	log.Printf("provider events: room=%q target=%q scope authorized", client.roomID, sessionID)
	return true
}

type sharedAgent struct {
	mu          sync.Mutex
	agent       tty.Agent
	subscribers map[*sharedSubscription]struct{}
	onOutput    func([]byte)
	onEmpty     func()
	closed      bool
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
			s.shutdown(false)
			return
		}
	}
}

func (s *sharedAgent) removeSubscriber(subscriber *sharedSubscription) {
	closeAgent := false
	s.mu.Lock()
	if !s.closed {
		if _, ok := s.subscribers[subscriber]; ok {
			delete(s.subscribers, subscriber)
			close(subscriber.done)
			closeAgent = len(s.subscribers) == 0
		}
	}
	s.mu.Unlock()
	if closeAgent {
		s.close()
	}
}

func (s *sharedAgent) close() {
	s.shutdown(true)
}

func (s *sharedAgent) shutdown(closeAgent bool) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	for subscriber := range s.subscribers {
		close(subscriber.done)
	}
	s.subscribers = make(map[*sharedSubscription]struct{})
	agent := s.agent
	onEmpty := s.onEmpty
	s.mu.Unlock()
	if closeAgent {
		if closer, ok := agent.(io.Closer); ok {
			_ = closer.Close()
		}
	}
	if onEmpty != nil {
		onEmpty()
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
	s.lastActivity[session.ID] = time.Now()
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
	s.lastActivity[sessionID] = time.Now()
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
			if roomID := r.URL.Query().Get("roomId"); roomID != "" {
				s.mu.Lock()
				created := s.sessions[session.ID]
				created.RoomID = roomID
				s.sessions[session.ID] = created
				s.mu.Unlock()
				session.RoomID = roomID
			}
			writeJSON(w, http.StatusCreated, session)
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
