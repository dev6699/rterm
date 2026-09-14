package rterm

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dev6699/rterm/auth"
	"github.com/dev6699/rterm/command"
	"github.com/dev6699/rterm/provider"
	"github.com/dev6699/rterm/server"
	"github.com/dev6699/rterm/tty"
	"github.com/dev6699/rterm/ui"
	"github.com/gorilla/websocket"
)

var (
	defaultPrefix = "/rterm"
	wsUpgrader    = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	assets             fs.FS
	registeredCommands []Command
)

func init() {
	var err error
	assets, err = ui.Assets()
	if err != nil {
		log.Fatalf("rterm: failed to load assets; err = %v", err)
	}
}

// SetPrefix to override default url prefix
func SetPrefix(prefix string) {
	// Check if the prefix starts with "/"
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}

	// Check if the prefix ends with "/"
	prefix = strings.TrimSuffix(prefix, "/")

	defaultPrefix = prefix
}

// SetWSUpgrader to override default websocket upgrader
func SetWSUpgrader(u websocket.Upgrader) {
	wsUpgrader = u
}

type Command struct {
	// Name of the command, will be used as the url to execute the command
	Name string
	// Args of the the command
	Args []string
	// Description of the command
	Description string
	// Writable indicate whether server should process inputs from clients
	Writable bool
	// AuthCheck acts as pre-verification step before starts agent process
	AuthCheck auth.AuthCheck
	// AllowEmbed controls whether the command page may be embedded by a parent page.
	AllowEmbed bool
}

// RegisterWithConfig registers static commands and provider profiles loaded from
// a JSON file. A missing config file is treated as an empty provider set.
func RegisterWithConfig(mux *http.ServeMux, configPath string, commands ...Command) error {
	config, err := provider.Load(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return register(mux, commands, provider.NewService(config))
}

// Register binds all command handlers to the http mux.
// GET <prefix>/ ->  commands listing index page.
// GET <prefix>/{command} -> command page.
// GET <prefix>/{command}/ws -> websocket for command inputs handling.
func Register(mux *http.ServeMux, commands ...Command) {
	if err := register(mux, commands, nil); err != nil {
		log.Printf("rterm: failed to register commands; err = %v", err)
	}
}

func register(mux *http.ServeMux, commands []Command, providers *provider.Service) error {
	commandsMap := map[string]Command{}
	for _, cmd := range commands {
		commandsMap[cmd.Name] = cmd
		registeredCommands = append(registeredCommands, cmd)
		log.Printf("server: command[%s] -> %s", cmd.Name, defaultPrefix+"/"+cmd.Name)
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		providerPrefix := strings.TrimRight(defaultPrefix, "/") + "/provider"
		if defaultPrefix == "/" {
			providerPrefix = "/provider"
		}
		if providers != nil && (r.URL.Path == providerPrefix || r.URL.Path == providerPrefix+"/") {
			if r.Method != http.MethodGet {
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}
			providerIndex(w, providerPrefix, providers.Profiles())
			return
		}
		apiPrefix := defaultPrefix + "/api/"
		if defaultPrefix == "/" {
			apiPrefix = "/api/"
		}
		if providers != nil && strings.HasPrefix(r.URL.Path, apiPrefix) {
			if strings.HasSuffix(r.URL.Path, "/api/events/ws") {
				providers.HandleEventsWebSocket(&wsUpgrader, w, r)
				return
			}
			apiPath := strings.TrimPrefix(r.URL.Path, defaultPrefix)
			if defaultPrefix == "/" {
				apiPath = r.URL.Path
			}
			if strings.HasSuffix(apiPath, "/ws") {
				parts := strings.Split(strings.Trim(apiPath, "/"), "/")
				if len(parts) == 4 && parts[0] == "api" && parts[1] == "sessions" && parts[2] != "" && parts[3] == "ws" {
					if !providers.AuthorizeSession(r, parts[2]) {
						http.Error(w, "unauthorized session", http.StatusUnauthorized)
						return
					}
					program, args, err := providers.ConnectArgs(parts[2])
					if err != nil {
						http.Error(w, err.Error(), http.StatusNotFound)
						return
					}
					server.HandleWebSocket(&wsUpgrader, server.Command{
						Factory: func() (tty.Agent, error) {
							return providers.SharedAgent(parts[2], func() (tty.Agent, error) { return command.New(program, args) }, func(data []byte) { providers.AppendOutput(parts[2], data) })
						},
						Writable: true,
						OnTTY:    func(terminal *tty.TTY) { providers.AttachTerminal(parts[2], terminal) },
					})(w, r)
					return
				}
			}
			r2 := r.Clone(r.Context())
			r2.URL.Path = strings.TrimPrefix(r.URL.Path, defaultPrefix)
			providers.Handler().ServeHTTP(w, r2)
			return
		}
		if r.URL.Path == defaultPrefix {
			if r.Method == http.MethodGet {
				index(w, r)
				return
			}
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		hasRoute := strings.HasPrefix(r.URL.Path, defaultPrefix+"/")
		if defaultPrefix == "/" {
			hasRoute = strings.HasPrefix(r.URL.Path, "/")
		}

		if hasRoute {
			commandPath := strings.TrimPrefix(r.URL.Path, defaultPrefix+"/")
			commandName := strings.TrimSuffix(commandPath, "/ws")
			commandName = strings.TrimSuffix(commandName, "/")
			cmd, isCommand := commandsMap[commandName]
			if isCommand && r.URL.Query().Get("embed") == "1" && !cmd.AllowEmbed {
				http.Error(w, "Embedding is disabled for this command", http.StatusForbidden)
				return
			}

			if strings.HasSuffix(commandPath, "/ws") {
				if r.Method == http.MethodGet {
					cmd, ok := commandsMap[commandName]
					if !ok {
						http.NotFound(w, r)
						return
					}
					server.HandleWebSocket(&wsUpgrader, server.Command{
						Factory: func() (tty.Agent, error) {
							return command.New(cmd.Name, cmd.Args)
						},
						Writable:  cmd.Writable,
						AuthCheck: cmd.AuthCheck,
					})(w, r)
					return
				}
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}

			if r.Method == http.MethodGet {
				if filepath.Ext(r.URL.Path) == "" {
					serveIndex(w)
					return
				}
				ext := filepath.Ext(r.URL.Path)
				stripPrefix := r.URL.Path
				if ext != "" {
					stripPrefix = defaultPrefix
				}
				http.StripPrefix(stripPrefix, http.FileServer(http.FS(assets))).ServeHTTP(w, r)
				return
			}
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		http.NotFound(w, r)
	})
	return nil
}

func providerIndex(w http.ResponseWriter, prefix string, providers []string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b bytes.Buffer
	fmt.Fprintf(&b, `<!DOCTYPE html><html><head><meta charset='utf-8'><meta name='viewport' content='width=device-width, initial-scale=1'><title>Providers</title><style>
*{box-sizing:border-box}html,body{width:100%%;min-height:100%%}body{margin:0;padding:24px;display:flex;align-items:center;justify-content:center;color:#fff;background:#111;font:14px system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.card{width:min(100%%,520px);padding:28px;border:1px solid #333;border-radius:12px;background:#181818;box-shadow:0 18px 50px rgba(0,0,0,.35)}h1{margin:0 0 6px;font-size:1.35rem}p{margin:0 0 18px;color:#aaa}.providers{display:flex;flex-direction:column;gap:8px;margin:0;padding:0;list-style:none}.providers a{display:block;padding:11px 12px;border:1px solid #444;border-radius:6px;color:#fff;background:#0d0d0d;text-decoration:none}.providers a:hover,.providers a:focus-visible{border-color:#4f8cff;background:#202b43;outline:none}.empty{color:#aaa}
</style></head><body><main class='card'><h1>Remote providers</h1><p>Select a provider to connect.</p><ul class='providers'>`)
	if len(providers) == 0 {
		b.WriteString("<li class='empty'>No providers configured.</li>")
	}
	for _, name := range providers {
		link := &url.URL{Path: prefix + "/" + name}
		fmt.Fprintf(&b, "<li><a href='%s'>%s</a></li>", link.String(), html.EscapeString(name))
	}
	b.WriteString("</ul></main></body></html>")
	_, _ = w.Write(b.Bytes())
}

// index responds with an HTML page listing the available commands.
func index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	err := indexTmplExecute(w)
	if err != nil {
		log.Printf("rterm: failed to serve index; err = %v", err)
	}
}

func serveIndex(w http.ResponseWriter) {
	file, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	prefix := strings.TrimRight(defaultPrefix, "/")
	page := strings.ReplaceAll(string(file), "__RTERM_PREFIX__", prefix)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, page)
}

func indexTmplExecute(w io.Writer) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, `<html>
<head>
<title>%s</title>
<style>
.profile-name{
	display:inline-block;
	width:6rem;
}
</style>
</head>
<body>
%s
<br>
<br>
Types of commands available:
<table>
<thead><td>Command</td></thead>
`, defaultPrefix, defaultPrefix)

	for _, command := range registeredCommands {
		link := &url.URL{Path: defaultPrefix + "/" + command.Name}
		fmt.Fprintf(&b, "<tr><td><a href='%s'>%s</a></td></tr>\n", link, html.EscapeString(command.Name))
	}

	b.WriteString(`</table>
<br>
<p>
Command Descriptions:
<ul>
`)
	for _, command := range registeredCommands {
		fmt.Fprintf(&b, "<li><div class=profile-name>%s: </div> %s</li>\n", html.EscapeString(command.Name), html.EscapeString(command.Description))
	}
	b.WriteString(`</ul>
</p>
</body>
</html>`)

	_, err := w.Write(b.Bytes())
	return err
}
