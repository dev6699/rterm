import test from "node:test";
import assert from "node:assert/strict";
import { handleSocketClose, handleSocketError, setupSockets } from "../src/sockets.js";
import { writeTerminalData } from "../src/terminal.js";
import { stateWithDom, installTerminalFakes, authIds } from "./test-utils.js";
import { installDom } from "./fakes.js";

test("socket setup ignores stale connections and routes authentication/output", () => {
  installDom(authIds);
  const state = stateWithDom();
  const connections = [];
  globalThis.WebSocket = class FakeSocket {
    static OPEN = 1;
    static CONNECTING = 0;
    constructor() {
      this.readyState = 1;
      this.listeners = {};
      connections.push(this);
    }
    addEventListener(type, handler) {
      (this.listeners[type] ||= []).push(handler);
    }
    emit(type, data = {}) {
      for (const handler of this.listeners[type] || []) handler(data);
    }
    send() {}
    close() {}
  };
  state.resize = () => {};
  state.parentEvent = () => {};
  state.activateProviderSession = () => {};
  setupSockets(state);
  state.activeProviderSession = { id: "active" };
  state.attachSocket("/first", state.activeProviderSession);
  const first = connections[0];
  state.attachSocket("/second", state.activeProviderSession);
  const second = connections[1];
  first.emit("close");
  second.emit("open");
  installTerminalFakes();
  second.emit("message", { data: "a" });
  assert.equal(document.getElementById("auth").style.display, "flex");
  second.emit("message", { data: "c" });
  second.emit("message", { data: "1b3V" });
  second.emit("message", { data: "d" });
  assert.doesNotThrow(() => writeTerminalData(state, "output"));
});

test("room socket flushes queued events and handles session events", () => {
  const state = stateWithDom("?roomId=room", "/provider/ssh");
  const connections = [];
  globalThis.WebSocket = class FakeSocket {
    static OPEN = 1;
    static CONNECTING = 0;
    constructor() {
      this.readyState = 0;
      this.listeners = {};
      connections.push(this);
    }
    addEventListener(type, handler) {
      (this.listeners[type] ||= []).push(handler);
    }
    emit(type, data = {}) {
      for (const handler of this.listeners[type] || []) handler(data);
    }
    send(message) {
      this.lastMessage = message;
    }
    close() {
      this.closed = true;
    }
  };
  state.parentEvent = () => {};
  state.resize = () => {};
  state.activateProviderSession = () => {};
  setupSockets(state);
  state.sendRoomEvent({ type: "new-session" });
  state.connectEvents();
  const room = connections[0];
  room.emit("open");
  assert.equal(room.lastMessage, '{"type":"new-session"}');
  installTerminalFakes();
  room.emit("message", { data: JSON.stringify({ sequence: 1, type: "new-session" }) });
  state.selectingNewSession = false;
  room.emit("message", {
    data: JSON.stringify({
      sequence: 2,
      type: "new-session",
      sessionId: "new",
      token: "token",
      target: "host",
      user: "root",
    }),
  });
  state.providerSessions.set("existing", {
    id: "existing",
    state: "connected",
    socket: { close() {} },
    terminal: undefined,
    container: undefined,
  });
  room.emit("message", {
    data: JSON.stringify({ sequence: 3, type: "selected", sessionId: "existing" }),
  });
  state.activeProviderSession = state.providerSessions.get("new");
  room.emit("message", {
    data: JSON.stringify({ sequence: 4, type: "closed", sessionId: "existing" }),
  });
  room.emit("message", { data: "{bad" });
  room.emit("close");
  assert.equal(state.eventsSocket, undefined);
});

test("socket close and error handlers clean active, background, and standalone sessions", () => {
  const state = stateWithDom("", "/provider/ssh");
  state.activeProviderSession = { id: "active" };
  state.resize = () => {};
  const active = {
    id: "active",
    state: "connected",
    socket: {},
    terminal: { dispose() {} },
    container: { style: {} },
  };
  state.activeProviderSession = active;
  state.socket = active.socket;
  handleSocketClose(state, active, {});
  handleSocketError(state, active, active.socket);
  const stale = { id: "stale", socket: {}, state: "connected" };
  handleSocketClose(state, stale, {});
  handleSocketError(state, stale, {});
  state.terminal = { dispose() {} };
  handleSocketClose(state, undefined, {});
  state.terminal = { dispose() {} };
  handleSocketError(state, undefined, {});
  const standalone = stateWithDom();
  standalone.terminal = { dispose() {} };
  handleSocketClose(standalone, undefined, {});
  standalone.terminal = { dispose() {} };
  handleSocketError(standalone, undefined, {});
  assert.equal(standalone.dom.terminal.innerText, "Connection error");
});
