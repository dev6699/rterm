import { parentEvent } from "./bridge.js";
import { hideTransfers, setupTransfers } from "./transfers.js";
import { showTerminal } from "./terminal.js";

export function renderTabs(state) {
  if (!state.providerName || !state.dom.tabs) return;
  state.dom.tabs.style.display = state.providerSessions.size ? "flex" : "none";
  if (state.dom.newSession)
    state.dom.newSession.classList.toggle("active", !state.activeProviderSession);
  for (const button of [...state.dom.tabs.querySelectorAll(".session-tab")]) button.remove();
  const tabs = document.createDocumentFragment();
  for (const session of state.providerSessions.values()) {
    const tab = document.createElement("div");
    tab.className = "session-tab";
    tab.classList.toggle("active", session === state.activeProviderSession);
    const select = document.createElement("button");
    select.className = "session-tab-select";
    select.textContent = `${session.user}@${session.target} · ${session.id.slice(0, 8)}`;
    select.title = session.id;
    select.addEventListener("click", () => activateProviderSession(state, session));
    const close = document.createElement("button");
    close.className = "session-tab-close";
    close.type = "button";
    close.textContent = "×";
    close.title = "Close session";
    close.setAttribute("aria-label", `Close ${session.user}@${session.target}`);
    close.addEventListener("click", () => closeProviderSession(state, session));
    tab.append(select, close);
    tabs.append(tab);
  }
  if (state.dom.newSession) state.dom.tabs.insertBefore(tabs, state.dom.newSession);
  else state.dom.tabs.append(tabs);
}

export { showTerminal };

export function activateProviderSession(state, session, announce = true) {
  state.activeProviderSession = session;
  state.activeSessionId = session.id;
  state.socket = session.socket;
  state.terminal = session.terminal;
  state.fitAddon = session.fitAddon;
  for (const candidate of state.providerSessions.values())
    if (candidate.container)
      candidate.container.style.display = candidate === session ? "block" : "none";
  state.dom.provider.style.display = session.state === "connected" ? "none" : "flex";
  state.dom.terminal.style.display = session.state === "connected" ? "block" : "none";
  renderTabs(state);
  if (session.state === "connected") {
    document.body.classList.add("terminal-active");
    document.documentElement.classList.add("terminal-active");
    setupTransfers(state);
    if (announce)
      state.sendRoomEvent({ type: "select", sessionId: session.id, token: session.token });
    parentEvent(state, "authenticated");
  }
  state.resize();
}

export function openNewSession(state, announce = false) {
  if (state.selectingNewSession) return;
  state.selectingNewSession = true;
  if (announce) state.sendRoomEvent({ type: "new-session" });
  state.activeProviderSession = undefined;
  state.activeSessionId = "";
  state.socket = undefined;
  state.terminal = undefined;
  state.fitAddon = undefined;
  for (const session of state.providerSessions.values())
    if (session.container) session.container.style.display = "none";
  state.dom.terminal.style.display = "none";
  hideTransfers();
  state.dom.provider.style.display = "flex";
  const connect = document.getElementById("connect-provider");
  const error = document.getElementById("provider-error");
  if (connect) connect.disabled = false;
  if (error) error.textContent = "";
  renderTabs(state);
}

export function closeProviderSession(state, session) {
  session.closing = true;
  const socket = session.socket;
  session.socket = undefined;
  session.terminal?.dispose();
  session.container?.remove();
  state.providerSessions.delete(session.id);
  state.sendRoomEvent({ type: "close", sessionId: session.id, token: session.token });
  parentEvent(state, "disconnected", { sessionId: session.id });
  if (session !== state.activeProviderSession) {
    socket?.close();
    renderTabs(state);
    return;
  }
  state.activeProviderSession = undefined;
  state.activeSessionId = "";
  state.socket = undefined;
  state.terminal = undefined;
  state.fitAddon = undefined;
  activateNextProviderSession(state, false);
  socket?.close();
}

export function activateNextProviderSession(state, announce = true) {
  const next = [...state.providerSessions.values()]
    .filter((candidate) => candidate.state !== "disconnected")
    .at(-1);
  if (next) activateProviderSession(state, next, announce);
  else {
    parentEvent(state, "disconnected");
    openNewSession(state);
  }
}

export function syncProviderSessions(state, sessions, activeSessionId) {
  if (!Array.isArray(sessions)) return;
  const allowed = new Set(
    sessions
      .filter((session) => session && typeof session.sessionId === "string")
      .map((session) => session.sessionId),
  );
  for (const session of [...state.providerSessions.values()])
    if (!allowed.has(session.id)) {
      session.socket?.close();
      session.terminal?.dispose();
      session.container?.remove();
      state.providerSessions.delete(session.id);
    }
  renderTabs(state);
  if (state.selectingNewSession) return;
  const active =
    typeof activeSessionId === "string" ? state.providerSessions.get(activeSessionId) : undefined;
  if (active && active !== state.activeProviderSession)
    activateProviderSession(state, active, false);
}

export function attachSessionByToken(state, sessionId, token, target, user, activate) {
  if (!state.providerName || !sessionId || !token || state.providerSessions.has(sessionId)) return;
  const session = {
    id: sessionId,
    token,
    target: target || sessionId,
    user: user || "",
    state: "connecting",
    socket: undefined,
    terminal: undefined,
    fitAddon: undefined,
    container: undefined,
  };
  state.providerSessions.set(sessionId, session);
  state.attachSocket(
    `${state.providerApiPrefix}/sessions/${encodeURIComponent(sessionId)}/ws?token=${encodeURIComponent(token)}`,
    session,
  );
  if (activate) {
    state.selectingNewSession = false;
    state.activeProviderSession = session;
    state.activeSessionId = sessionId;
    showTerminal(state, session, false, state);
    state.dom.provider.style.display = "none";
    setupTransfers(state);
  }
  renderTabs(state);
}
