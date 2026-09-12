const MSG_INPUT = '0'
const MSG_OUTPUT = '1'
const MSG_RESIZE_TERMINAL = '2'

const MSG_AUTH = 'a'
const MSG_AUTH_TRY = 'b'
const MSG_AUTH_OK = 'c'
const MSG_AUTH_FAILED = 'd'

const embedded = new URLSearchParams(window.location.search).get('embed') === '1'
const parentBridge = embedded && new URLSearchParams(window.location.search).get('bridge') === 'parent'
document.body.classList.toggle('embedded', embedded)

function parentEvent(type, data = {}) {
    if (!embedded || window.parent === window) return
    window.parent.postMessage({ source: 'rterm', type, ...data }, '*')
}

function sendSocket(message) {
    if (parentBridge) {
        const kind = message.slice(0, 1)
        if (kind === MSG_INPUT) parentEvent('input', { data: message.slice(1) })
        if (kind === MSG_AUTH_TRY) parentEvent('authenticate', { code: message.slice(1) })
        if (kind === MSG_RESIZE_TERMINAL) {
            try {
                parentEvent('resize', JSON.parse(message.slice(1)))
            } catch (_) {
                // Ignore malformed resize data from the terminal client.
            }
        }
        return
    }
    if (socket.readyState === WebSocket.OPEN) socket.send(message)
}

const terminalElement = document.getElementById('terminal')
terminalElement.style.display = 'none'
const authElement = document.getElementById('auth')
authElement.style.display = 'none'

let terminal
let fitAddon
function showTerminal() {
    if (terminal) return
    terminalElement.style.display = 'block'
    terminal = new Terminal({ fontSize: embedded ? 11 : 14, lineHeight: embedded ? 1.1 : 1.2 });
    fitAddon = new FitAddon.FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(terminalElement);
    fitAddon.fit();

    sendSocket(MSG_RESIZE_TERMINAL + JSON.stringify({ cols: terminal.cols, rows: terminal.rows }))
    parentEvent('terminal-ready', { cols: terminal.cols, rows: terminal.rows })

    terminal.onData((data) => {
        sendSocket(MSG_INPUT + data)
    })

    terminal.onResize((data) => {
        sendSocket(MSG_RESIZE_TERMINAL + JSON.stringify(data))
        parentEvent('terminal-resized', data)
    })
}

const wsProtocol = window.location.protocol === 'https:' ? 'wss://' : 'ws://';
const wsHost = window.location.hostname;
const wsPort = window.location.port ? ':' + window.location.port : '';
const wsPath = window.location.pathname + '/ws';
const wsURL = wsProtocol + wsHost + wsPort + wsPath;
const socket = parentBridge ? {
    readyState: WebSocket.OPEN,
    send: () => {},
    addEventListener: () => {},
} : new WebSocket(wsURL);

socket.addEventListener('open', () => parentEvent('connected'))


socket.addEventListener("message", (event) => {
    const message = event.data.slice(0, 1)

    switch (message) {
        case MSG_AUTH:
            authElement.style.display = 'flex'
            document.getElementById('digit1').focus();
            parentEvent('authentication-required')
            break

        case MSG_AUTH_OK:
            authElement.style.display = 'none'
            showTerminal()
            parentEvent('authenticated')
            break

        case MSG_AUTH_FAILED:
            clearDigits()
            parentEvent('authentication-failed')
            break

        case MSG_OUTPUT:
            const data = atob(event.data.slice(1))
            writeTerminalData(data)
            parentEvent('output', { data: event.data.slice(1) })
            break
    }
});

socket.addEventListener("close", () => {
    if (terminal) {
        terminal.dispose()
        terminal = undefined
    }
    terminalElement.style.display = 'block'
    terminalElement.innerText = "Connection closed"
    parentEvent('disconnected')
})

socket.addEventListener("error", () => {
    if (terminal) {
        terminal.dispose()
        terminal = undefined
    }
    terminalElement.style.display = 'block'
    terminalElement.innerText = "Connection error"
    parentEvent('error')
})


function resize() {
    if (!terminal || !fitAddon) return
    fitAddon.fit()
    terminal.scrollToBottom()
}

window.addEventListener('resize', resize)

function writeTerminalData(data) {
    if (!terminal) return
    const followOutput = terminal.buffer.active.viewportY >= terminal.buffer.active.baseY
    terminal.write(data)
    if (followOutput) terminal.scrollToBottom()
}

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
    sendSocket(MSG_AUTH_TRY + code)
}

window.addEventListener('message', (event) => {
    if (!embedded || event.source !== window.parent || !event.data) return
    switch (event.data.type) {
        case 'connected':
            break
        case 'authentication-required':
            if (parentBridge) {
                terminalElement.style.display = 'none'
                authElement.style.display = 'flex'
                document.getElementById('digit1').focus()
            }
            break
        case 'authenticated':
            if (parentBridge) {
                authElement.style.display = 'none'
                showTerminal()
            }
            break
        case 'authentication-failed':
            parentBridge && clearDigits()
            break
        case 'output':
            if (parentBridge && typeof event.data.data === 'string') {
                if (!terminal) showTerminal()
                writeTerminalData(atob(event.data.data))
            }
            break
        case 'reset':
            if (parentBridge) {
                terminal?.dispose()
                terminal = undefined
                fitAddon = undefined
                terminalElement.replaceChildren()
                terminalElement.style.display = 'none'
                authElement.style.display = 'none'
            }
            break
        case 'disconnected':
            if (parentBridge) {
                terminal?.dispose()
                terminal = undefined
                fitAddon = undefined
                terminalElement.replaceChildren()
                terminalElement.style.display = 'none'
                authElement.style.display = 'none'
            }
            break
        case 'write':
            if (typeof event.data.input === 'string') sendSocket(MSG_INPUT + event.data.input)
            break
        case 'authenticate':
            if (typeof event.data.code === 'string') sendSocket(MSG_AUTH_TRY + event.data.code)
            break
        case 'resize':
            if (Number.isInteger(event.data.cols) && Number.isInteger(event.data.rows)) {
                sendSocket(MSG_RESIZE_TERMINAL + JSON.stringify({ cols: event.data.cols, rows: event.data.rows }))
            }
            break
    }
})

parentEvent('loaded')

function clearDigits() {
    for (let i = 1; i <= 6; i++) {
        document.getElementById(`digit${i}`).value = '';
    }
    document.getElementById('digit1').focus();
    document.getElementById('result').textContent = 'Invalid code';
}
