export function createState() {
  const params = new URLSearchParams(window.location.search);
  const providerMatch = window.location.pathname.match(/\/provider\/([^/]+)\/?$/);
  const providerName = providerMatch ? decodeURIComponent(providerMatch[1]) : "";
  const providerPrefix = providerMatch
    ? window.location.pathname.slice(0, providerMatch.index)
    : "";

  const state = {
    embedded: params.get("embed") === "1",
    parentBridge: params.get("embed") === "1" && params.get("bridge") === "parent",
    providerName,
    providerPrefix,
    providerApiPrefix: `${providerPrefix}/api`,
    bridgeToken: params.get("bridgeToken") || "",
    parentOrigin: params.get("parentOrigin") || "",
    roomId: params.get("roomId") || "",
    wsProtocol: window.location.protocol === "https:" ? "wss://" : "ws://",
    wsHost: window.location.hostname,
    wsPort: window.location.port ? ":" + window.location.port : "",
    socket: undefined,
    eventsSocket: undefined,
    pendingEvents: [],
    lastEventSequence: 0,
    terminal: undefined,
    fitAddon: undefined,
    activeSessionId: "",
    activeProviderSession: undefined,
    selectingNewSession: false,
    transferHandlersReady: false,
    providerSessions: new Map(),
  };

  state.dom = {
    terminal: document.getElementById("terminal"),
    auth: document.getElementById("auth"),
    provider: document.getElementById("provider-select"),
    tabs: document.getElementById("session-tabs"),
    newSession: document.getElementById("new-session"),
  };
  state.dom.terminal.style.display = "none";
  state.dom.auth.style.display = "none";
  document.body.classList.toggle("embedded", state.embedded);
  document.documentElement.classList.toggle("provider-page", Boolean(state.providerName));

  return state;
}
