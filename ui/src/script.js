import { acceptsParentMessage, parentEvent } from "./bridge.js";
import { setupAuth } from "./auth.js";
import { MSG_AUTH_TRY, MSG_INPUT, MSG_RESIZE_TERMINAL } from "./protocol.js";
import { setupProvider } from "./provider.js";
import { createState } from "./state.js";
import { activateProviderSession, openNewSession } from "./sessions.js";
import { setupSockets } from "./sockets.js";
import { resize, sendSocket, showTerminal, writeTerminalData } from "./terminal.js";

const state = createState();
state.resize = () => resize(state);
state.activateProviderSession = (session, announce = true) =>
  activateProviderSession(state, session, announce);
state.parentEvent = (type, data) => parentEvent(state, type, data);
state.sendSocket = (message) => sendSocket(state, message, state.parentEvent);
setupSockets(state);
const auth = setupAuth(state, state.parentEvent);

if (state.dom.newSession)
  state.dom.newSession.addEventListener("click", () => openNewSession(state, true));
window.addEventListener("resize", state.resize);

window.addEventListener("message", (event) => {
  if (!acceptsParentMessage(state, event)) return;
  const data = event.data;
  switch (data.type) {
    case "sessions-request":
      if (typeof data.requestId === "string" && data.requestId)
        parentEvent(state, "sessions-response", {
          requestId: data.requestId,
          ok: true,
          connected: [...state.providerSessions.values()].some(
            (session) => session.state === "connected",
          ),
          result: [...state.providerSessions.values()].map((session) => ({
            sessionId: session.id,
            token: session.token,
            provider: state.providerName,
            target: session.target,
            user: session.user,
          })),
        });
      break;
    case "authentication-required":
      if (state.parentBridge) {
        state.dom.terminal.style.display = "none";
        state.dom.auth.style.display = "flex";
        document.getElementById("digit1").focus();
      }
      break;
    case "authenticated":
      if (state.parentBridge) {
        state.dom.auth.style.display = "none";
        showTerminal(state, undefined, true, state);
      }
      break;
    case "authentication-failed":
      if (state.parentBridge) auth.clearDigits();
      break;
    case "output":
      if (state.parentBridge && typeof data.data === "string") {
        if (!state.terminal) showTerminal(state, undefined, true, state);
        writeTerminalData(state, atob(data.data));
      }
      break;
    case "reset":
    case "disconnected":
      if (state.parentBridge) {
        state.terminal?.dispose();
        state.terminal = undefined;
        state.fitAddon = undefined;
        state.dom.terminal.replaceChildren();
        state.dom.terminal.style.display = "none";
        state.dom.auth.style.display = "none";
      }
      break;
    case "select-session": {
      if (state.providerName && typeof data.sessionId === "string") {
        const session = state.providerSessions.get(data.sessionId);
        if (session) activateProviderSession(state, session);
      }
      break;
    }
    case "write":
      if (typeof data.input === "string")
        sendSocket(state, MSG_INPUT + data.input, state.parentEvent);
      break;
    case "authenticate":
      if (typeof data.code === "string")
        sendSocket(state, MSG_AUTH_TRY + data.code, state.parentEvent);
      break;
    case "resize":
      if (Number.isInteger(data.cols) && Number.isInteger(data.rows))
        sendSocket(
          state,
          MSG_RESIZE_TERMINAL + JSON.stringify({ cols: data.cols, rows: data.rows }),
          state.parentEvent,
        );
      break;
  }
});

if (state.providerName) setupProvider(state);
else if (!state.parentBridge)
  state.attachSocket(
    `${state.wsProtocol}${state.wsHost}${state.wsPort}${window.location.pathname}/ws`,
  );
parentEvent(state, "loaded");
if (state.providerName && state.roomId) state.connectEvents();
