import test from "node:test";
import assert from "node:assert/strict";
import {
  disposeTerminal,
  resize,
  sendSocket,
  showTerminal,
  writeTerminalData,
} from "../src/terminal.js";
import { stateWithDom, installTerminalFakes } from "./test-utils.js";

test("terminal transport forwards bridge and socket messages", () => {
  const events = [];
  const parent = (type, data) => events.push({ type, data });
  sendSocket({ parentBridge: true, providerName: "", socket: undefined }, "0hello", parent);
  sendSocket({ parentBridge: true, providerName: "", socket: undefined }, "b123456", parent);
  sendSocket(
    { parentBridge: true, providerName: "", socket: undefined },
    '2{"cols":80,"rows":24}',
    parent,
  );
  sendSocket({ parentBridge: true, providerName: "", socket: undefined }, "2not-json", parent);
  assert.deepEqual(events, [
    { type: "input", data: { data: "hello" } },
    { type: "authenticate", data: { code: "123456" } },
    { type: "resize", data: { cols: 80, rows: 24 } },
  ]);
  const sent = [];
  sendSocket(
    {
      parentBridge: false,
      providerName: "",
      socket: { readyState: 1, send: (message) => sent.push(message) },
    },
    "0input",
    parent,
  );
  assert.deepEqual(sent, ["0input"]);
});

test("terminal lifecycle creates xterm, resizes, writes, and disposes", () => {
  const state = stateWithDom();
  const events = [];
  installTerminalFakes();
  state.parentBridge = true;
  showTerminal(state, undefined, true, {
    parentEvent: (type, data) => events.push({ type, data }),
    activateProviderSession: () => {},
  });
  state.terminal.handlers.data("hello");
  state.terminal.handlers.resize({ cols: 80, rows: 24 });
  writeTerminalData(state, "output");
  resize(state);
  disposeTerminal(state);
  assert.equal(state.terminal, undefined);
  assert.equal(
    events.some((event) => event.type === "terminal-ready"),
    true,
  );
  assert.equal(
    events.some((event) => event.type === "terminal-resized"),
    true,
  );
  const existing = { terminal: { marker: true } };
  showTerminal({ ...state, terminal: undefined }, existing, false, {
    activateProviderSession: () => {
      existing.activated = true;
    },
    parentEvent: () => {},
  });
  assert.equal(existing.activated, true);
});
