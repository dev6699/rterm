import test from "node:test";
import assert from "node:assert/strict";
import {
  activateNextProviderSession,
  activateProviderSession,
  attachSessionByToken,
  closeProviderSession,
  openNewSession,
  renderTabs,
  syncProviderSessions,
} from "../src/sessions.js";
import { stateWithDom, installTerminalFakes } from "./test-utils.js";

test("session lifecycle renders tabs, activates, syncs, and opens a new session", () => {
  const state = stateWithDom("", "/provider/ssh");
  state.resize = () => {};
  state.sendRoomEvent = (event) => {
    state.lastRoomEvent = event;
  };
  state.providerSessions.set("one", {
    id: "one",
    user: "root",
    target: "host",
    state: "connected",
    token: "secret",
    terminal: undefined,
    container: undefined,
  });
  renderTabs(state);
  activateProviderSession(state, state.providerSessions.get("one"), true);
  assert.equal(state.activeSessionId, "one");
  assert.deepEqual(state.lastRoomEvent, { type: "select", sessionId: "one", token: "secret" });
  syncProviderSessions(state, [], "");
  assert.equal(state.providerSessions.size, 0);
  openNewSession(state, true);
  assert.equal(state.selectingNewSession, true);
  assert.deepEqual(state.lastRoomEvent, { type: "new-session" });
  const clickable = stateWithDom("", "/provider/ssh");
  clickable.resize = () => {};
  clickable.sendRoomEvent = () => {};
  const session = {
    id: "click",
    user: "root",
    target: "host",
    state: "connected",
    token: "token",
    terminal: undefined,
    container: undefined,
  };
  clickable.providerSessions.set(session.id, session);
  renderTabs(clickable);
  const tab = clickable.dom.tabs.children[0].children[0];
  tab.children[0].click();
  tab.children[1].click();
});

test("sessions attach token sessions, close tabs, and select fallbacks", () => {
  const state = stateWithDom("", "/provider/ssh");
  state.resize = () => {};
  state.attachSocket = (url, session) => {
    session.socket = { readyState: 1, send() {} };
  };
  state.activateProviderSession = () => {};
  attachSessionByToken(state, "id", "token", "target", "user", false);
  assert.equal(state.providerSessions.get("id").target, "target");
  state.dom.newSession = null;
  state.parentEvent = () => {};
  state.providerSessions.set("tab", {
    id: "tab",
    user: "root",
    target: "host",
    state: "connected",
    token: "token",
  });
  renderTabs(state);
  installTerminalFakes();
  attachSessionByToken(state, "active", "token", "host", "root", true);
  assert.equal(state.activeSessionId, "active");
  const firstSocket = {
    close() {
      this.closed = true;
    },
  };
  const first = {
    id: "first",
    token: "one",
    state: "connected",
    socket: firstSocket,
    terminal: { dispose() {} },
    container: { remove() {}, style: {} },
  };
  const second = {
    id: "second",
    token: "two",
    state: "connected",
    socket: { close() {} },
    terminal: undefined,
    container: undefined,
  };
  state.providerSessions.set(first.id, first);
  state.providerSessions.set(second.id, second);
  state.activeProviderSession = second;
  state.sendRoomEvent = (event) => {
    state.lastRoomEvent = event;
  };
  closeProviderSession(state, first);
  assert.equal(firstSocket.closed, true);
  state.providerSessions.clear();
  state.providerSessions.set(second.id, second);
  closeProviderSession(state, second);
  assert.equal(state.selectingNewSession, true);
  state.selectingNewSession = false;
  state.providerSessions.clear();
  activateNextProviderSession(state, false);
  assert.equal(state.selectingNewSession, true);
});
