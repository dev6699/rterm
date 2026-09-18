import { MSG_AUTH, MSG_AUTH_FAILED, MSG_AUTH_OK, MSG_OUTPUT } from "./protocol.js";
import { parentEvent } from "./bridge.js";
import { clearDigits } from "./auth.js";
import { disposeTerminal, showTerminal, writeTerminalData } from "./terminal.js";
import {
  activateNextProviderSession,
  activateProviderSession,
  attachSessionByToken,
  openNewSession,
  renderTabs,
} from "./sessions.js";

export function setupSockets(state) {
  state.connectEvents = () => connectEvents(state);
  state.attachSocket = (url, session) => {
    const connection = new WebSocket(url);
    if (session) session.socket = connection;
    if (!session || session === state.activeProviderSession) state.socket = connection;
    if (session && state.providerName && !state.eventsSocket) connectEvents(state);
    connection.addEventListener("open", () => {
      if (!session || session === state.activeProviderSession) parentEvent(state, "connected");
    });
    connection.addEventListener("message", (event) => handleSocketMessage(state, event, session));
    connection.addEventListener("close", () => handleSocketClose(state, session, connection));
    connection.addEventListener("error", () => handleSocketError(state, session, connection));
  };
  state.sendRoomEvent = (event) => {
    const message = JSON.stringify(event);
    if (state.eventsSocket?.readyState === WebSocket.OPEN) state.eventsSocket.send(message);
    else if (state.eventsSocket?.readyState === WebSocket.CONNECTING || !state.eventsSocket)
      state.pendingEvents.push(message);
  };
}

export function connectEvents(state) {
  if (!state.roomId) return;
  const path = `${state.providerPrefix}/api/events/ws?roomId=${encodeURIComponent(state.roomId)}`;
  state.eventsSocket = new WebSocket(`${state.wsProtocol}${state.wsHost}${state.wsPort}${path}`);
  state.eventsSocket.addEventListener("open", () => {
    for (const message of state.pendingEvents.splice(0)) state.eventsSocket.send(message);
  });
  state.eventsSocket.addEventListener("message", (event) => {
    try {
      const message = JSON.parse(event.data);
      if (Number.isInteger(message.sequence)) {
        if (message.sequence <= state.lastEventSequence) return;
        state.lastEventSequence = message.sequence;
      }
      if (message.type === "new-session") {
        if (typeof message.sessionId === "string" && typeof message.token === "string") {
          const existing = state.providerSessions.get(message.sessionId);
          if (existing) activateProviderSession(state, existing, false);
          else
            attachSessionByToken(
              state,
              message.sessionId,
              message.token,
              message.target,
              message.user,
              true,
            );
          return;
        }
        if (!state.selectingNewSession) openNewSession(state);
        return;
      }
      if (message.type === "closed" && typeof message.sessionId === "string") {
        parentEvent(state, "disconnected", { sessionId: message.sessionId });
        const closed = state.providerSessions.get(message.sessionId);
        if (closed) {
          closed.closing = true;
          closed.socket?.close();
          closed.terminal?.dispose();
          closed.container?.remove();
          state.providerSessions.delete(closed.id);
          if (closed === state.activeProviderSession) activateNextProviderSession(state, false);
          else renderTabs(state);
        }
        return;
      }
      if (message.type === "selected" && typeof message.sessionId === "string") {
        const selected = state.providerSessions.get(message.sessionId);
        if (selected) activateProviderSession(state, selected, false);
      }
    } catch (_) {
      /* Ignore malformed synchronization messages. */
    }
  });
  state.eventsSocket.addEventListener("close", () => {
    state.eventsSocket = undefined;
  });
}

export function handleSocketMessage(state, event, session) {
  const message = event.data.slice(0, 1);
  if (message === MSG_AUTH) {
    if (session) session.state = "authenticating";
    if (session && session !== state.activeProviderSession) return;
    state.dom.auth.style.display = "flex";
    document.getElementById("digit1").focus();
    parentEvent(state, "authentication-required");
  } else if (message === MSG_AUTH_OK) {
    if (session) session.state = "connected";
    state.dom.auth.style.display = "none";
    showTerminal(state, session, false, state);
    if (!session || session === state.activeProviderSession) parentEvent(state, "authenticated");
  } else if (message === MSG_AUTH_FAILED) {
    clearDigits();
    parentEvent(state, "authentication-failed");
  } else if (message === MSG_OUTPUT) {
    const data = atob(event.data.slice(1));
    writeTerminalData(state, data, session);
    if (!session || session === state.activeProviderSession)
      parentEvent(state, "output", { data: event.data.slice(1) });
  }
}

function cleanupSession(state, session, connection) {
  if (!session) return true;
  if (session.socket !== connection) return false;
  session.socket = undefined;
  session.state = "disconnected";
  session.terminal?.dispose();
  session.terminal = undefined;
  if (session.container) session.container.style.display = "none";
  renderTabs(state);
  return true;
}

export function handleSocketClose(state, session, connection) {
  if (!cleanupSession(state, session, connection)) return;
  if (session?.closing) return;
  if (session) {
    parentEvent(state, "disconnected", { sessionId: session.id });
    if (session !== state.activeProviderSession) return;
  }
  disposeTerminal(state);
  document.body.classList.remove("terminal-active");
  document.documentElement.classList.remove("terminal-active");
  if (state.providerName) {
    state.activeSessionId = "";
    state.dom.terminal.style.display = "none";
    state.dom.provider.style.display = "flex";
    document.getElementById("connect-provider").disabled = false;
  } else {
    state.dom.terminal.style.display = "block";
    state.dom.terminal.innerText = "Connection closed";
    parentEvent(state, "disconnected");
  }
}

export function handleSocketError(state, session, connection) {
  if (!cleanupSession(state, session, connection)) return;
  if (session?.closing || (session && session !== state.activeProviderSession)) return;
  disposeTerminal(state);
  document.body.classList.remove("terminal-active");
  document.documentElement.classList.remove("terminal-active");
  if (state.providerName) {
    state.activeSessionId = "";
    state.dom.terminal.style.display = "none";
    state.dom.provider.style.display = "flex";
    document.getElementById("connect-provider").disabled = false;
  } else {
    state.dom.terminal.style.display = "block";
    state.dom.terminal.innerText = "Connection error";
  }
  parentEvent(state, "error", session ? { sessionId: session.id } : {});
}
