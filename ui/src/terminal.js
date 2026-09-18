import { MSG_INPUT, MSG_RESIZE_TERMINAL, removeTerminalReports } from "./protocol.js";

export function sendSocket(state, message, parentEvent) {
  if (state.parentBridge && !state.providerName) {
    const kind = message.slice(0, 1);
    if (kind === MSG_INPUT) parentEvent("input", { data: message.slice(1) });
    if (kind === "b") parentEvent("authenticate", { code: message.slice(1) });
    if (kind === MSG_RESIZE_TERMINAL) {
      try {
        parentEvent("resize", JSON.parse(message.slice(1)));
      } catch (_) {
        // Ignore malformed resize data from the terminal client.
      }
    }
    return;
  }
  if (state.socket?.readyState === WebSocket.OPEN) state.socket.send(message);
}

export function showTerminal(state, session, announce, callbacks) {
  if (session?.terminal) {
    callbacks.activateProviderSession(session, announce);
    return;
  }
  if (session) state.socket = session.socket;
  document.body.classList.add("terminal-active");
  document.documentElement.classList.add("terminal-active");
  state.dom.terminal.style.display = "block";
  const container = session ? document.createElement("div") : state.dom.terminal;
  if (session) {
    container.className = "provider-terminal";
    container.style.display = "block";
    session.container = container;
    state.dom.terminal.appendChild(container);
  }
  state.terminal = new Terminal({
    fontSize: state.embedded ? 11 : 14,
    lineHeight: state.embedded ? 1.1 : 1.2,
  });
  state.fitAddon = new FitAddon.FitAddon();
  state.terminal.loadAddon(state.fitAddon);
  state.terminal.open(container);
  state.fitAddon.fit();
  sendSocket(
    state,
    MSG_RESIZE_TERMINAL + JSON.stringify({ cols: state.terminal.cols, rows: state.terminal.rows }),
    callbacks.parentEvent,
  );
  callbacks.parentEvent("terminal-ready", { cols: state.terminal.cols, rows: state.terminal.rows });
  if (session) {
    session.terminal = state.terminal;
    session.fitAddon = state.fitAddon;
  }
  state.terminal.onData((data) => {
    const input = removeTerminalReports(data);
    if (input) sendSocket(state, MSG_INPUT + input, callbacks.parentEvent);
  });
  state.terminal.onResize((data) => {
    sendSocket(state, MSG_RESIZE_TERMINAL + JSON.stringify(data), callbacks.parentEvent);
    callbacks.parentEvent("terminal-resized", data);
  });
  if (session) callbacks.activateProviderSession(session, announce);
}

export function writeTerminalData(state, data, session) {
  const target = session?.terminal || state.terminal;
  if (!target) return;
  const followOutput = target.buffer.active.viewportY >= target.buffer.active.baseY;
  target.write(data, () => {
    if (followOutput) target.scrollToBottom();
  });
}

export function resize(state) {
  if (!state.terminal || !state.fitAddon) return;
  state.fitAddon.fit();
  state.terminal.scrollToBottom();
}

export function disposeTerminal(state) {
  state.terminal?.dispose();
  state.terminal = undefined;
  state.fitAddon = undefined;
}
