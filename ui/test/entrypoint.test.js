import test from "node:test";
import assert from "node:assert/strict";
import { installDom } from "./fakes.js";

test("script entrypoint boots and routes embedded parent messages", async () => {
  installDom([
    "terminal",
    "auth",
    "provider-select",
    "session-tabs",
    "new-session",
    ...Array.from({ length: 6 }, (_, index) => `digit${index + 1}`),
    "result",
    "transfer-panel",
    "transfer-toggle",
    "transfer-close",
    "upload-file",
    "upload-path",
    "upload-button",
    "download-path",
    "download-button",
    "transfer-status",
    "target-select",
    "user-select",
    "connect-provider",
    "provider-error",
  ]);
  window.location.pathname = "/bash";
  window.location.search =
    "?embed=1&bridge=parent&bridgeToken=token&parentOrigin=https%3A%2F%2Fparent.test";
  const parentMessages = [];
  window.parent = { postMessage: (...message) => parentMessages.push(message) };
  globalThis.WebSocket = class {
    static OPEN = 1;
    static CONNECTING = 0;
    constructor() {
      this.readyState = 1;
    }
    addEventListener() {}
    send() {}
  };
  globalThis.Terminal = class {
    constructor() {
      this.cols = 80;
      this.rows = 24;
      this.buffer = { active: { viewportY: 0, baseY: 0 } };
    }
    loadAddon() {}
    open() {}
    onData() {}
    onResize() {}
    write() {}
    dispose() {}
    scrollToBottom() {}
  };
  globalThis.FitAddon = {
    FitAddon: class {
      fit() {}
    },
  };
  await import("../src/script.js?entrypoint-test");
  const event = (data) => ({
    source: window.parent,
    origin: "https://parent.test",
    data: { ...data, bridgeToken: "token" },
  });
  window.dispatch("message", event({ type: "sessions-request", requestId: "request" }));
  window.dispatch("message", event({ type: "authentication-required" }));
  window.dispatch("message", event({ type: "authenticated" }));
  window.dispatch("message", event({ type: "output", data: "aGVsbG8=" }));
  window.dispatch("message", event({ type: "write", input: "x" }));
  window.dispatch("message", event({ type: "authenticate", code: "123456" }));
  window.dispatch("message", event({ type: "resize", cols: 80, rows: 24 }));
  window.dispatch("message", event({ type: "reset" }));
  window.dispatch("message", event({ type: "disconnected" }));
  assert.equal(document.body.embedded, true);
  assert.equal(
    parentMessages.some(([message]) => message.type === "sessions-response"),
    true,
  );
});
