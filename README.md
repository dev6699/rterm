[![GoDoc](https://pkg.go.dev/badge/github.com/dev6699/rterm)](https://pkg.go.dev/github.com/dev6699/rterm)
[![Go Report Card](https://goreportcard.com/badge/github.com/dev6699/rterm)](https://goreportcard.com/report/github.com/dev6699/rterm)
[![License](https://img.shields.io/github/license/dev6699/rterm)](LICENSE)

# <p align="center">RTERM</p>

RTERM is a web-based remote control application that allows you to control your terminal remotely.

Inspired by [GoTTY](https://github.com/yudai/gotty)

<p align="center">
    <img src="docs/rterm.gif">
</p>


## Installation

1. Import as package to existing project.
    ```bash
    go get github.com/dev6699/rterm
    ```

    ```go
    import (
        "github.com/dev6699/rterm"
        "github.com/dev6699/rterm/command"
    )

    func main() {
        rterm.SetPrefix("/")
        mux := http.NewServeMux()

        rterm.Register(
            mux,
            rterm.Command{
                Name:        "bash",
                Description: "Bash (Unix shell)",
                Writable:    true,
                AuthCheck:   auth.NewBasic("123456"),
            },
        )

        addr := ":5000"
        server := &http.Server{
            Addr:    addr,
            Handler: mux,
        }
        server.ListenAndServe()
    }
    ```
    Please check [example](cmd/rterm/main.go) for more information.

    <img src="docs/index.png" width="45%">
    <img src="docs/auth.png" width="45%">

2. Prebuilt binary.

    - Grab the latest binary from the [releases](https://github.com/dev6699/rterm/releases) page.

3. From sources:
    ```bash
    # Clone the Repository
    git clone https://github.com/dev6699/rterm.git
    cd rterm

    # Build
    make build
    ```

## Usage
1. Start the binary `./rterm`.
2. Open web browser and navigate to `http://<remote_ip>:5000`.
3. Get control of your terminal!

### Embedded mode

Command pages support `?embed=1` for hosts that embed the rterm UI. In this
mode the page keeps the normal xterm.js terminal and WebSocket connection, and
notifies its parent with `postMessage` events using `{ source: "rterm" }`.
The parent may send `{ type: "write", input }`,
`authenticate`, or `resize` messages to control the session.

For a host that owns the WebSocket connection, use `?embed=1&bridge=parent`.
In bridge mode the rterm page renders the terminal but forwards input,
authentication, and resize messages to the parent; the parent supplies output
events as base64 data with `{ type: "output", data }`.

Embedding is controlled by the Go registration with `AllowEmbed: true` on the
command. Commands without that setting reject `?embed=1` with HTTP 403.

## License
This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
