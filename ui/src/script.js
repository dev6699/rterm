const MSG_INPUT = '0'
const MSG_OUTPUT = '1'
const MSG_RESIZE_TERMINAL = '2'

const MSG_AUTH = 'a'
const MSG_AUTH_TRY = 'b'
const MSG_AUTH_OK = 'c'
const MSG_AUTH_FAILED = 'd'

const embedded = new URLSearchParams(window.location.search).get('embed') === '1'
const parentBridge = embedded && new URLSearchParams(window.location.search).get('bridge') === 'parent'
const providerMatch = window.location.pathname.match(/\/provider\/([^/]+)\/?$/)
const providerName = providerMatch ? decodeURIComponent(providerMatch[1]) : ''
const providerPrefix = providerMatch ? window.location.pathname.slice(0, providerMatch.index) : ''
const providerApiPrefix = `${providerPrefix}/api`
const bridgeToken = new URLSearchParams(window.location.search).get('bridgeToken') || ''
const roomId = new URLSearchParams(window.location.search).get('roomId') || ''
document.body.classList.toggle('embedded', embedded)
document.documentElement.classList.toggle('provider-page', Boolean(providerName))

function parentEvent(type, data = {}) {
    if (!embedded || window.parent === window || !bridgeToken)
        return
    window.parent.postMessage(
        { source: 'rterm', type, ...(bridgeToken ? { bridgeToken } : {}), ...data },
        '*',
    )
}


function sendSocket(message) {
    if (parentBridge && !providerName) {
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
    if (socket?.readyState === WebSocket.OPEN) socket.send(message)
}

const terminalElement = document.getElementById('terminal')
terminalElement.style.display = 'none'
const authElement = document.getElementById('auth')
authElement.style.display = 'none'
const providerElement = document.getElementById('provider-select')

let terminal
let fitAddon
let activeSessionId = ''
const providerSessions = new Map()
let activeProviderSession
let selectingNewSession = false
let eventsSocket
const pendingEvents = []
let lastEventSequence = 0
let transferHandlersReady = false
const tabsElement = document.getElementById('session-tabs')
const newSessionButton = document.getElementById('new-session')

function showTerminal(session, announce = true) {
    if (session?.terminal) {
        activateProviderSession(session, announce)
        return
    }
    if (session) socket = session.socket
    document.body.classList.add('terminal-active')
    document.documentElement.classList.add('terminal-active')
    terminalElement.style.display = 'block'
    const container = session ? document.createElement('div') : terminalElement
    if (session) {
        container.className = 'provider-terminal'
        container.style.display = 'block'
        session.container = container
        terminalElement.appendChild(container)
    }
    terminal = new Terminal({ fontSize: embedded ? 11 : 14, lineHeight: embedded ? 1.1 : 1.2 })
    fitAddon = new FitAddon.FitAddon()
    terminal.loadAddon(fitAddon)
    terminal.open(container)
    fitAddon.fit()

    sendSocket(MSG_RESIZE_TERMINAL + JSON.stringify({ cols: terminal.cols, rows: terminal.rows }))
    parentEvent('terminal-ready', { cols: terminal.cols, rows: terminal.rows })

    if (session) session.terminal = terminal
    if (session) session.fitAddon = fitAddon
    terminal.onData((data) => {
        const input = removeTerminalReports(data)
        if (input) sendSocket(MSG_INPUT + input)
    })

    terminal.onResize((data) => {
        sendSocket(MSG_RESIZE_TERMINAL + JSON.stringify(data))
        parentEvent('terminal-resized', data)
    })
    if (session) activateProviderSession(session, announce)
}

function renderTabs() {
    if (!providerName || !tabsElement) return
    tabsElement.style.display = providerSessions.size ? 'flex' : 'none'
    if (newSessionButton) newSessionButton.classList.toggle('active', !activeProviderSession)
    for (const button of [...tabsElement.querySelectorAll('.session-tab')]) button.remove()
    const tabs = document.createDocumentFragment()
    for (const session of providerSessions.values()) {
        const tab = document.createElement('div')
        tab.className = 'session-tab'
        tab.classList.toggle('active', session === activeProviderSession)
        const select = document.createElement('button')
        select.className = 'session-tab-select'
        select.textContent = `${session.user}@${session.target} · ${session.id.slice(0, 8)}`
        select.title = session.id
        select.addEventListener('click', () => activateProviderSession(session))
        const close = document.createElement('button')
        close.className = 'session-tab-close'
        close.type = 'button'
        close.textContent = '×'
        close.title = 'Close session'
        close.setAttribute('aria-label', `Close ${session.user}@${session.target}`)
        close.addEventListener('click', () => closeProviderSession(session))
        tab.append(select, close)
        tabs.append(tab)
    }
    if (newSessionButton) tabsElement.insertBefore(tabs, newSessionButton)
    else tabsElement.append(tabs)
}

function closeProviderSession(session) {
    session.closing = true
    const socketToClose = session.socket
    session.socket = undefined
    session.terminal?.dispose()
    session.container?.remove()
    providerSessions.delete(session.id)
    sendRoomEvent({ type: 'close', sessionId: session.id, token: session.token })
    parentEvent('disconnected', { sessionId: session.id })
    if (session !== activeProviderSession) {
        socketToClose?.close()
        renderTabs()
        return
    }
    activeProviderSession = undefined
    activeSessionId = ''
    socket = undefined
    terminal = undefined
    fitAddon = undefined
    activateNextProviderSession(false)
    socketToClose?.close()
}

function activateNextProviderSession(announce = true) {
    const next = [...providerSessions.values()].filter((candidate) => candidate.state !== 'disconnected').at(-1)
    if (next) {
        activateProviderSession(next, announce)
        return
    }
    parentEvent('disconnected')
    openNewSession()
}

function activateProviderSession(session, announce = true) {
    activeProviderSession = session
    activeSessionId = session.id
    socket = session.socket
    terminal = session.terminal
    fitAddon = session.fitAddon
    for (const candidate of providerSessions.values()) {
        if (candidate.container) candidate.container.style.display = candidate === session ? 'block' : 'none'
    }
    providerElement.style.display = session.state === 'connected' ? 'none' : 'flex'
    terminalElement.style.display = session.state === 'connected' ? 'block' : 'none'
    renderTabs()
    if (session.state === 'connected') {
        document.body.classList.add('terminal-active')
        document.documentElement.classList.add('terminal-active')
        setupTransfers()
        if (announce) {
            sendRoomEvent({ type: 'select', sessionId: session.id, token: session.token })
        }
        parentEvent('authenticated')
    }
    resize()
}

function openNewSession(announce = false) {
    if (selectingNewSession) return
    selectingNewSession = true
    if (announce) sendRoomEvent({ type: 'new-session' })
    activeProviderSession = undefined
    activeSessionId = ''
    socket = undefined
    terminal = undefined
    fitAddon = undefined
    for (const session of providerSessions.values()) {
        if (session.container) session.container.style.display = 'none'
    }
    terminalElement.style.display = 'none'
    hideTransfers()
    providerElement.style.display = 'flex'
    const connectButton = document.getElementById('connect-provider')
    const errorElement = document.getElementById('provider-error')
    if (connectButton) connectButton.disabled = false
    if (errorElement) errorElement.textContent = ''
    renderTabs()
}

function syncProviderSessions(sessions, activeSessionId) {
    if (!Array.isArray(sessions)) return
    const allowed = new Set(
        sessions
            .filter((session) => session && typeof session.sessionId === 'string')
            .map((session) => session.sessionId),
    )
    for (const session of [...providerSessions.values()]) {
        if (allowed.has(session.id)) continue
        session.socket?.close()
        session.terminal?.dispose()
        session.container?.remove()
        providerSessions.delete(session.id)
    }
    renderTabs()
    if (selectingNewSession) return
    const active = typeof activeSessionId === 'string' ? providerSessions.get(activeSessionId) : undefined
    if (active && active !== activeProviderSession) activateProviderSession(active, false)
}

function attachSessionByToken(sessionId, token, target, user, activate) {
    if (!providerName || !sessionId || !token || providerSessions.has(sessionId)) return
    const providerSession = {
        id: sessionId,
        token,
        target: target || sessionId,
        user: user || '',
        state: 'connecting',
        socket: undefined,
        terminal: undefined,
        fitAddon: undefined,
        container: undefined,
    }
    providerSessions.set(sessionId, providerSession)
    const sessionPath = `${providerApiPrefix}/sessions/${encodeURIComponent(sessionId)}/ws`
    attachSocket(
        `${wsProtocol}${wsHost}${wsPort}${sessionPath}?token=${encodeURIComponent(token)}`,
        providerSession,
    )
    if (activate) {
        selectingNewSession = false
        activeProviderSession = providerSession
        activeSessionId = sessionId
        showTerminal(providerSession, false)
        providerElement.style.display = 'none'
        setupTransfers()
    }
    renderTabs()
}

const wsProtocol = window.location.protocol === 'https:' ? 'wss://' : 'ws://'
const wsHost = window.location.hostname
const wsPort = window.location.port ? ':' + window.location.port : ''
const wsPath = window.location.pathname + '/ws'
const wsURL = wsProtocol + wsHost + wsPort + wsPath
let socket = parentBridge
    ? {
          readyState: WebSocket.OPEN,
          send: () => {},
          addEventListener: () => {},
      }
    : undefined

function attachSocket(url, session) {
    const connection = new WebSocket(url)
    if (session) session.socket = connection
    if (!session || session === activeProviderSession) socket = connection
    if (session && providerName && !eventsSocket) connectEvents(session)
    connection.addEventListener('open', () => {
        if (!session || session === activeProviderSession) parentEvent('connected')
    })
    connection.addEventListener('message', (event) => handleSocketMessage(event, session))
    connection.addEventListener('close', () => handleSocketClose(session, connection))
    connection.addEventListener('error', () => handleSocketError(session, connection))
}

function sendRoomEvent(event) {
    const message = JSON.stringify(event)
    if (eventsSocket?.readyState === WebSocket.OPEN) {
        eventsSocket.send(message)
    } else if (eventsSocket?.readyState === WebSocket.CONNECTING || !eventsSocket) {
        pendingEvents.push(message)
    }
}

function connectEvents(session) {
    console.log('[rterm-frame] connect-events', { sessionId: session.id })
    if (!roomId) return
    const eventsPath = `${providerPrefix}/api/events/ws?roomId=${encodeURIComponent(roomId)}`
    eventsSocket = new WebSocket(`${wsProtocol}${wsHost}${wsPort}${eventsPath}`)
    eventsSocket.addEventListener('open', () => {
        for (const message of pendingEvents.splice(0)) eventsSocket.send(message)
    })
    eventsSocket.addEventListener('message', (event) => {
        try {
            const message = JSON.parse(event.data)
            if (Number.isInteger(message.sequence)) {
                if (message.sequence <= lastEventSequence) return
                lastEventSequence = message.sequence
            }
            console.log('[rterm-frame] event', {
                type: message.type,
                sessionId: typeof message.sessionId === 'string' ? message.sessionId : undefined,
                sequence: Number.isInteger(message.sequence) ? message.sequence : undefined,
            })
            if (message.type === 'new-session') {
                if (typeof message.sessionId === 'string' && typeof message.token === 'string') {
                    const existing = providerSessions.get(message.sessionId)
                    if (existing) activateProviderSession(existing, false)
                    else attachSessionByToken(message.sessionId, message.token, message.target, message.user, true)
                    return
                }
                // The initiating view already opened the form locally. Other attached
                // views should follow the new-session selection.
                if (!selectingNewSession) openNewSession()
                return
            }
            if (message.type === 'closed' && typeof message.sessionId === 'string') {
                parentEvent('disconnected', { sessionId: message.sessionId })
                const closed = providerSessions.get(message.sessionId)
                if (closed) {
                    closed.closing = true
                    closed.socket?.close()
                    closed.terminal?.dispose()
                    closed.container?.remove()
                    providerSessions.delete(closed.id)
                    if (closed === activeProviderSession) activateNextProviderSession(false)
                    else renderTabs()
                }
                return
            }
            if (message.type !== 'selected' || typeof message.sessionId !== 'string') return
            const selected = providerSessions.get(message.sessionId)
            console.log('[rterm-frame] selected lookup', {
                sessionId: message.sessionId,
                found: Boolean(selected),
                knownSessionIds: [...providerSessions.keys()],
                selectingNewSession,
            })
            if (selected) activateProviderSession(selected, false)
        } catch (_) {
            // Ignore malformed synchronization messages.
        }
    })
    eventsSocket.addEventListener('close', () => {
        eventsSocket = undefined
    })
}

function handleSocketMessage(event, session) {
    const message = event.data.slice(0, 1)

    switch (message) {
        case MSG_AUTH:
            if (session) session.state = 'authenticating'
            if (session && session !== activeProviderSession) break
            authElement.style.display = 'flex'
            document.getElementById('digit1').focus()
            parentEvent('authentication-required')
            break

        case MSG_AUTH_OK:
            if (session) session.state = 'connected'
            authElement.style.display = 'none'
            // Authentication is an automatic socket lifecycle event; it must
            // not re-announce a background session as a user selection.
            showTerminal(session, false)
            if (!session || session === activeProviderSession) parentEvent('authenticated')
            break

        case MSG_AUTH_FAILED:
            clearDigits()
            parentEvent('authentication-failed')
            break

        case MSG_OUTPUT:
            const data = atob(event.data.slice(1))
            writeTerminalData(data, session)
            if (!session || session === activeProviderSession) parentEvent('output', { data: event.data.slice(1) })
            break
    }
}

function handleSocketClose(session, connection) {
    if (session) {
        if (session.socket !== connection) return
        session.socket = undefined
        session.state = 'disconnected'
        if (session.terminal) session.terminal.dispose()
        session.terminal = undefined
        if (session.container) session.container.style.display = 'none'
        renderTabs()
        if (session.closing) return
        parentEvent('disconnected', { sessionId: session.id })
        if (session !== activeProviderSession) return
    } else if (terminal) {
        terminal.dispose()
        terminal = undefined
    }
    document.body.classList.remove('terminal-active')
    document.documentElement.classList.remove('terminal-active')
    if (providerName) {
        hideTransfers()
        activeSessionId = ''
        terminalElement.style.display = 'none'
        providerElement.style.display = 'flex'
        document.getElementById('connect-provider').disabled = false
    } else {
        terminalElement.style.display = 'block'
        terminalElement.innerText = 'Connection closed'
    }
    if (!session) parentEvent('disconnected')
}

// xterm.js answers terminal status queries with reports. When the remote PTY
// has echo enabled, those answers can be echoed back as visible text instead
// of being consumed by the program that issued the query. They are not useful
// as interactive input in the web terminal, so keep them out of the PTY.
function removeTerminalReports(data) {
    return data.replace(/\x1b\](?:10|11|12);rgb:[0-9a-f]+\/[0-9a-f]+\/[0-9a-f]+(?:\x07|\x1b\\)|\x1b\[\??\d+;\d+R/gi, '')
}

function handleSocketError(session, connection) {
    if (session) {
        if (session.socket !== connection) return
        session.socket = undefined
        session.state = 'disconnected'
        if (session.terminal) session.terminal.dispose()
        session.terminal = undefined
        if (session.container) session.container.style.display = 'none'
        renderTabs()
        if (session.closing) return
        if (session !== activeProviderSession) return
    }
    if (terminal) {
        terminal.dispose()
        terminal = undefined
    }
    document.body.classList.remove('terminal-active')
    document.documentElement.classList.remove('terminal-active')
    if (providerName) {
        hideTransfers()
        activeSessionId = ''
        terminalElement.style.display = 'none'
        providerElement.style.display = 'flex'
        document.getElementById('connect-provider').disabled = false
    } else {
        terminalElement.style.display = 'block'
        terminalElement.innerText = 'Connection error'
    }
    parentEvent('error', session ? { sessionId: session.id } : {})
}

function hideTransfers() {
    const panel = document.getElementById('transfer-panel')
    const toggle = document.getElementById('transfer-toggle')
    if (panel) panel.style.display = 'none'
    if (toggle) {
        toggle.style.display = 'none'
        toggle.setAttribute('aria-expanded', 'false')
    }
}

async function setupProvider() {
    providerElement.style.display = 'flex'
    const targetSelect = document.getElementById('target-select')
    const userSelect = document.getElementById('user-select')
    const connectButton = document.getElementById('connect-provider')
    const errorElement = document.getElementById('provider-error')
    let targets = []

    function updateUsers() {
        userSelect.replaceChildren()
        const target = targets.find((item) => item.id === targetSelect.value)
        for (const user of target?.users || []) {
            const option = document.createElement('option')
            option.value = user
            option.textContent = user
            userSelect.appendChild(option)
        }
        connectButton.disabled = !target || userSelect.options.length === 0
    }

    function renderTargets() {
        const visible = targets
        const selected = targetSelect.value
        targetSelect.replaceChildren()
        for (const target of visible) {
            const option = document.createElement('option')
            option.value = target.id
            option.textContent = target.label || target.id
            targetSelect.appendChild(option)
        }
        if (visible.some((target) => target.id === selected)) targetSelect.value = selected
        updateUsers()
    }

    try {
        connectButton.disabled = true
        const response = await fetch(`${providerApiPrefix}/providers/${encodeURIComponent(providerName)}/targets`)
        if (!response.ok) throw new Error(await response.text())
        const discovery = await response.json()
        targets = discovery.targets || []
        renderTargets()
        targetSelect.addEventListener('change', updateUsers)
        connectButton.addEventListener('click', async () => {
            try {
                selectingNewSession = false
                connectButton.disabled = true
                errorElement.textContent = ''
                const create = await fetch(
                    `${providerApiPrefix}/providers/${encodeURIComponent(providerName)}/sessions${roomId ? `?roomId=${encodeURIComponent(roomId)}` : ''}`,
                    {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ target: targetSelect.value, user: userSelect.value }),
                    },
                )
                if (!create.ok) throw new Error(await create.text())
                const session = await create.json()
                const providerSession = {
                    id: session.id,
                    token: session.token,
                    target: session.target,
                    user: session.user,
                    state: 'connecting',
                    socket: undefined,
                    terminal: undefined,
                    fitAddon: undefined,
                    container: undefined,
                }
                providerSessions.set(providerSession.id, providerSession)
                activeProviderSession = providerSession
                activeSessionId = providerSession.id
                renderTabs()
                sendRoomEvent({
                    type: 'new-session',
                    sessionId: session.id,
                    token: session.token,
                    target: session.target,
                    user: session.user,
                })
                parentEvent('connecting')
                providerElement.style.display = 'none'
                setupTransfers()
                const sessionPath = `${providerApiPrefix}/sessions/${encodeURIComponent(session.id)}/ws`
                attachSocket(
                    `${wsProtocol}${wsHost}${wsPort}${sessionPath}?token=${encodeURIComponent(session.token)}`,
                    providerSession,
                )
            } catch (error) {
                connectButton.disabled = false
                errorElement.textContent = error instanceof Error ? error.message : String(error)
            }
        })
    } catch (error) {
        errorElement.textContent = error instanceof Error ? error.message : String(error)
    }
}

if (newSessionButton) newSessionButton.addEventListener('click', () => openNewSession(true))

function setupTransfers() {
    const panel = document.getElementById('transfer-panel')
    const toggle = document.getElementById('transfer-toggle')
    const close = document.getElementById('transfer-close')
    const uploadFile = document.getElementById('upload-file')
    const uploadPath = document.getElementById('upload-path')
    const uploadButton = document.getElementById('upload-button')
    const downloadPath = document.getElementById('download-path')
    const downloadButton = document.getElementById('download-button')
    const statusElement = document.getElementById('transfer-status')
    toggle.style.display = 'block'
    if (transferHandlersReady) return
    transferHandlersReady = true
    toggle.addEventListener('click', () => {
        panel.style.display = 'block'
        toggle.style.display = 'none'
        toggle.setAttribute('aria-expanded', 'true')
    })
    close.addEventListener('click', () => {
        panel.style.display = 'none'
        toggle.style.display = 'block'
        toggle.setAttribute('aria-expanded', 'false')
    })

    uploadPath.addEventListener('keydown', (event) => {
        if (event.key === 'Enter') {
            event.preventDefault()
            uploadButton.click()
        }
    })

    downloadPath.addEventListener('keydown', (event) => {
        if (event.key === 'Enter') {
            event.preventDefault()
            downloadButton.click()
        }
    })

    uploadButton.addEventListener('click', async () => {
        const file = uploadFile.files[0]
        if (!file || !uploadPath.value.trim()) {
            statusElement.textContent = 'Choose a file and enter a remote destination.'
            return
        }
        try {
            uploadButton.disabled = true
            statusElement.textContent = `Uploading ${file.name}…`
            const uploadQuery = new URLSearchParams({
                path: uploadPath.value.trim(),
                filename: file.name,
            })
            const response = await fetch(
                `${providerApiPrefix}/sessions/${encodeURIComponent(activeSessionId)}/upload?${uploadQuery}`,
                {
                    method: 'POST',
                    headers: { Authorization: `Bearer ${providerSessions.get(activeSessionId).token}` },
                    body: file,
                },
            )
            if (!response.ok) throw new Error(await response.text())
            const result = await response.json()
            statusElement.textContent = `Upload complete (${result.bytes || file.size} bytes).`
        } catch (error) {
            statusElement.textContent = `Upload failed: ${error instanceof Error ? error.message : String(error)}`
        } finally {
            uploadButton.disabled = false
        }
    })

    downloadButton.addEventListener('click', async () => {
        const remotePath = downloadPath.value.trim()
        if (!remotePath) {
            statusElement.textContent = 'Enter a remote source path.'
            return
        }
        try {
            downloadButton.disabled = true
            statusElement.textContent = 'Downloading…'
            const response = await fetch(
                `${providerApiPrefix}/sessions/${encodeURIComponent(activeSessionId)}/download?path=${encodeURIComponent(remotePath)}`,
                {
                    headers: { Authorization: `Bearer ${providerSessions.get(activeSessionId).token}` },
                },
            )
            if (!response.ok) throw new Error(await response.text())
            const blob = await response.blob()
            const link = document.createElement('a')
            link.href = URL.createObjectURL(blob)
            link.download = remotePath.split('/').pop() || 'download'
            link.style.display = 'none'
            document.body.appendChild(link)
            link.click()
            link.remove()
            URL.revokeObjectURL(link.href)
            statusElement.textContent = `Download complete (${blob.size} bytes).`
        } catch (error) {
            statusElement.textContent = `Download failed: ${error instanceof Error ? error.message : String(error)}`
        } finally {
            downloadButton.disabled = false
        }
    })
}

if (providerName) setupProvider()
else if (!parentBridge) attachSocket(wsURL)

function resize() {
    if (!terminal || !fitAddon) return
    fitAddon.fit()
    terminal.scrollToBottom()
}

window.addEventListener('resize', resize)

function writeTerminalData(data, session) {
    const target = session?.terminal || terminal
    if (!target) return
    const followOutput = target.buffer.active.viewportY >= target.buffer.active.baseY
    target.write(data, () => {
        if (followOutput) target.scrollToBottom()
    })
}

function moveFocus(currentDigit) {
    const currentInput = document.getElementById(`digit${currentDigit}`)
    if (currentInput.value.length === 1) {
        if (currentDigit < 6) {
            document.getElementById('result').textContent = ''
            document.getElementById(`digit${currentDigit + 1}`).focus()
        } else {
            submitCode()
        }
    }
}

function submitCode() {
    const digits = []
    for (let i = 1; i <= 6; i++) {
        const digitInput = document.getElementById(`digit${i}`)
        digits.push(digitInput.value)
    }
    const code = digits.join('')
    sendSocket(MSG_AUTH_TRY + code)
}

function handleSessionsRequest(request) {
    const requestId = typeof request.requestId === 'string' ? request.requestId : ''
    if (!requestId) return
    const respond = (ok, result, error) => parentEvent('sessions-response', {
        requestId,
        ok,
        connected: [...providerSessions.values()].some((candidate) => candidate.state === 'connected'),
        result,
        error,
    })
    respond(true, [...providerSessions.values()].map((candidate) => ({
            sessionId: candidate.id,
            token: candidate.token,
            provider: providerName,
            target: candidate.target,
            user: candidate.user,
    })))
}

window.addEventListener('message', (event) => {
    if (!embedded || event.source !== window.parent || !event.data || event.data.bridgeToken !== bridgeToken)
        return
    switch (event.data.type) {
        case 'sessions-request':
            handleSessionsRequest(event.data)
            break
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
        case 'select-session':
            if (providerName && typeof event.data.sessionId === 'string') {
                const session = providerSessions.get(event.data.sessionId)
                if (session) activateProviderSession(session)
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
if (providerName && roomId) connectEvents({ id: '', token: '' })

function clearDigits() {
    for (let i = 1; i <= 6; i++) {
        document.getElementById(`digit${i}`).value = ''
    }
    document.getElementById('digit1').focus()
    document.getElementById('result').textContent = 'Invalid code'
}
