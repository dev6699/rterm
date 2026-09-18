import test from "node:test";
import assert from "node:assert/strict";
import { setupTransfers } from "../src/transfers.js";
import { stateWithDom } from "./test-utils.js";

test("transfers perform upload and download requests", async () => {
  const state = stateWithDom("", "/provider/ssh");
  state.activeSessionId = "session";
  state.providerSessions.set("session", { token: "token" });
  const requests = [];
  document.getElementById("upload-file").files = [{ name: "a.txt", size: 3 }];
  document.getElementById("upload-path").value = "/tmp";
  globalThis.fetch = async (url, options) => {
    requests.push({ url, options });
    return { ok: true, json: async () => ({ bytes: 3 }), blob: async () => new Blob(["abc"]) };
  };
  setupTransfers(state);
  document.getElementById("upload-button").click();
  await new Promise((resolve) => setImmediate(resolve));
  document.getElementById("download-path").value = "/tmp/a.txt";
  document.getElementById("download-button").click();
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(requests.length, 2);
  assert.match(requests[0].url, /upload/);
  assert.match(requests[1].url, /download/);
});

test("transfers validate inputs, report failures, and toggle the panel", async () => {
  const state = stateWithDom("", "/provider/ssh");
  state.activeSessionId = "missing";
  state.providerSessions.set("missing", { token: "token" });
  setupTransfers(state);
  document.getElementById("upload-button").click();
  assert.match(document.getElementById("transfer-status").textContent, /Choose a file/);
  document.getElementById("download-button").click();
  assert.match(document.getElementById("transfer-status").textContent, /remote source/);
  document.getElementById("upload-file").files = [{ name: "a", size: 1 }];
  document.getElementById("upload-path").value = "/tmp";
  document.getElementById("download-path").value = "/tmp/a";
  globalThis.fetch = async () => ({ ok: false, text: async () => "network failed" });
  document.getElementById("upload-button").click();
  await new Promise((resolve) => setImmediate(resolve));
  document.getElementById("download-button").click();
  await new Promise((resolve) => setImmediate(resolve));
  document.getElementById("transfer-toggle").click();
  document.getElementById("transfer-close").click();
  document.getElementById("upload-path").dispatch("keydown", { key: "Enter" });
  document.getElementById("download-path").dispatch("keydown", { key: "Enter" });
  assert.equal(document.getElementById("transfer-panel").style.display, "none");
});
