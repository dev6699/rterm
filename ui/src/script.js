const MSG_INPUT = '0'
const MSG_OUTPUT = '1'
const MSG_RESIZE_TERMINAL = '2'

const MSG_AUTH = 'a'
const MSG_AUTH_TRY = 'b'
const MSG_AUTH_OK = 'c'
const MSG_AUTH_FAILED = 'd'

const terminalElement = document.getElementById('terminal')
terminalElement.style.display = 'none'
const authElement = document.getElementById('auth')
authElement.style.display = 'none'

let terminal
function showTerminal() {
    terminalElement.style.display = 'block'
    terminal = new Terminal();
    const fitAddon = new FitAddon.FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(terminalElement);
    fitAddon.fit();

    socket.send(MSG_RESIZE_TERMINAL + JSON.stringify({ cols: terminal.cols, rows: terminal.rows }))

    terminal.onData((data) => {
        socket.send(MSG_INPUT + data)
    })

    terminal.onResize((data) => {
        socket.send(MSG_RESIZE_TERMINAL + JSON.stringify(data))
    })
}

const wsProtocol = window.location.protocol === 'https:' ? 'wss://' : 'ws://';
const wsHost = window.location.hostname;
const wsPort = window.location.port ? ':' + window.location.port : '';
const wsPath = window.location.pathname + '/ws';
const wsURL = wsProtocol + wsHost + wsPort + wsPath;
const socket = new WebSocket(wsURL);


socket.addEventListener("message", (event) => {
    const message = event.data.slice(0, 1)

    switch (message) {
        case MSG_AUTH:
            authElement.style.display = 'flex'
            document.getElementById('digit1').focus();
            break

        case MSG_AUTH_OK:
            authElement.style.display = 'none'
            showTerminal()
            break

        case MSG_AUTH_FAILED:
            clearDigits()
            break

        case MSG_OUTPUT:
            const data = atob(event.data.slice(1))
            terminal.write(data)
            break
    }
});

socket.addEventListener("close", () => {
    terminal.dispose()
    terminalElement.innerText = "Connection closed"
})

socket.addEventListener("error", () => {
    terminal.dispose()
    terminalElement.innerText = "Connection error"
})


function resize() {
    fitAddon.fit()
    terminal.scrollToBottom()
}

window.addEventListener('resize', this.resize)

function moveFocus(currentDigit) {
    const currentInput = document.getElementById(`digit${currentDigit}`);
    if (currentInput.value.length === 1) {
        if (currentDigit < 6) {
            document.getElementById('result').textContent = '';
            document.getElementById(`digit${currentDigit + 1}`).focus();
        } else {
            submitCode()
        }
    }
}

function submitCode() {
    const digits = [];
    for (let i = 1; i <= 6; i++) {
        const digitInput = document.getElementById(`digit${i}`);
        digits.push(digitInput.value);
    }
    const code = digits.join('');
    socket.send(MSG_AUTH_TRY + code)
}

function clearDigits() {
    for (let i = 1; i <= 6; i++) {
        document.getElementById(`digit${i}`).value = '';
    }
    document.getElementById('digit1').focus();
    document.getElementById('result').textContent = 'Invalid code';
}
