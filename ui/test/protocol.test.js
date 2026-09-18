import test from "node:test";
import assert from "node:assert/strict";
import { removeTerminalReports, terminalWebSocketUrl } from "../src/protocol.js";

test("removes xterm color and cursor reports but preserves normal input", () => {
  assert.equal(
    removeTerminalReports("echo\x1b]10;rgb:0000/0000/0000\x07\x1b[12;34R\r\n"),
    "echo\r\n",
  );
});

test("removes ST-terminated color reports", () => {
  assert.equal(removeTerminalReports("a\x1b]11;rgb:ffff/0000/abcd\x1b\\b"), "ab");
});

test("builds the terminal WebSocket URL", () => {
  assert.equal(
    terminalWebSocketUrl({ wsProtocol: "wss://", wsHost: "host", wsPort: ":443" }, "/ws"),
    "wss://host:443/ws",
  );
});
