import test from "node:test";
import assert from "node:assert/strict";
import { setupAuth } from "../src/auth.js";
import { stateWithDom } from "./test-utils.js";

test("auth advances fields, submits, and clears invalid codes", () => {
  const state = stateWithDom();
  const sent = [];
  const auth = setupAuth(state, () => {});
  state.socket = { readyState: WebSocket.OPEN, send: (message) => sent.push(message) };
  for (let i = 1; i <= 6; i++) {
    document.getElementById(`digit${i}`).value = String(i);
    document.getElementById(`digit${i}`).dispatch("input");
  }
  assert.deepEqual(sent, ["b123456"]);
  auth.clearDigits();
  assert.equal(document.getElementById("digit1").value, "");
  assert.equal(document.getElementById("result").textContent, "Invalid code");
  assert.equal(document.getElementById("digit1").focused, true);
});
