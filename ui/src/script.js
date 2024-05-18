const MSG_INPUT = '0'
const MSG_OUTPUT = '1'
const MSG_RESIZE_TERMINAL = '2'

const terminalElement = document.getElementById('terminal')

const terminal = new Terminal();
const fitAddon = new FitAddon.FitAddon();
terminal.loadAddon(fitAddon);
terminal.open(terminalElement);
fitAddon.fit();

const wsProtocol = window.location.protocol === 'https:' ? 'wss://' : 'ws://';
const wsHost = window.location.hostname;
const wsPort = window.location.port ? ':' + window.location.port : '';
const wsPath = window.location.pathname + '/ws';
const wsURL = wsProtocol + wsHost + wsPort + wsPath;
const socket = new WebSocket(wsURL);

socket.addEventListener("open", () => {
    socket.send(MSG_RESIZE_TERMINAL + JSON.stringify({ cols: terminal.cols, rows: terminal.rows }))
});

socket.addEventListener("message", (event) => {
    const message = event.data.slice(0, 1)
    if (message !== MSG_OUTPUT) {
        return
    }
    const data = atob(event.data.slice(1))
    terminal.write(data)
});

socket.addEventListener("close", () => {
    terminal.dispose()
    terminalElement.innerText = "Connection closed"
})

socket.addEventListener("error", () => {
    terminal.dispose()
    terminalElement.innerText = "Connection error"
})

terminal.onData((data) => {
    socket.send(MSG_INPUT + data)
})

terminal.onResize((data) => {
    socket.send(MSG_RESIZE_TERMINAL + JSON.stringify(data))
})

function resize() {
    fitAddon.fit()
    terminal.scrollToBottom()
}

window.addEventListener('resize', this.resize)