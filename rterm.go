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
	"path/filepath"
	"sort"
	"strings"

	"github.com/dev6699/rterm/auth"
	"github.com/dev6699/rterm/command"
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
}

// Register binds all command handlers to the http mux.
// GET <prefix>/ ->  commands listing index page.
// GET <prefix>/{command} -> command page.
// GET <prefix>/{command}/ws -> websocket for command inputs handling.
func Register(mux *http.ServeMux, commands ...Command) {
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
		if r.URL.Path == defaultPrefix {
			if r.Method == http.MethodGet {
				index(w, r)
				return
			}
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		if strings.HasPrefix(r.URL.Path, defaultPrefix+"/") {

			commandPath := strings.TrimPrefix(r.URL.Path, defaultPrefix+"/")

			if strings.HasSuffix(commandPath, "/ws") {
				if r.Method == http.MethodGet {
					c := strings.TrimSuffix(commandPath, "/ws")
					cmd, ok := commandsMap[c]
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
				ext := filepath.Ext(r.URL.String())
				stripPrefix := r.URL.String()
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
