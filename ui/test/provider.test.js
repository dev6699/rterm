import test from "node:test";
import assert from "node:assert/strict";
import { setupProvider } from "../src/provider.js";
import { stateWithDom } from "./test-utils.js";

test("provider discovery populates users and creates a session", async () => {
  const state = stateWithDom("", "/provider/ssh");
  const calls = [];
  state.sendRoomEvent = (event) => calls.push(event);
  state.attachSocket = (url, session) => calls.push({ url, session });
  globalThis.fetch = async (url, options) =>
    options
      ? {
          ok: true,
          json: async () => ({ id: "session-1", token: "token", target: "host", user: "root" }),
        }
      : { ok: true, json: async () => ({ targets: [{ id: "host", users: ["root"] }] }) };
  await setupProvider(state);
  document.getElementById("target-select").value = "host";
  document.getElementById("target-select").dispatch("change");
  document.getElementById("connect-provider").click();
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(state.providerSessions.has("session-1"), true);
  assert.equal(calls[0].type, "new-session");
});

test("provider discovery and creation errors remain visible", async () => {
  const state = stateWithDom("", "/provider/ssh");
  globalThis.fetch = async () => ({ ok: false, text: async () => "discovery failed" });
  await setupProvider(state);
  assert.equal(document.getElementById("provider-error").textContent, "discovery failed");
  const retryState = stateWithDom("", "/provider/ssh");
  globalThis.fetch = async (url, options) =>
    options
      ? { ok: false, text: async () => "creation failed" }
      : { ok: true, json: async () => ({ targets: [{ id: "host", users: ["root"] }] }) };
  await setupProvider(retryState);
  document.getElementById("target-select").value = "host";
  document.getElementById("target-select").dispatch("change");
  document.getElementById("connect-provider").click();
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(document.getElementById("provider-error").textContent, "creation failed");
  assert.equal(document.getElementById("connect-provider").disabled, false);
});
