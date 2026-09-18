import { installDom } from "./fakes.js";
import { createState } from "../src/state.js";

export const authIds = [
  "terminal",
  "auth",
  "provider-select",
  "session-tabs",
  "new-session",
  ...Array.from({ length: 6 }, (_, i) => `digit${i + 1}`),
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
];

export function stateWithDom(search = "", pathname = "/bash") {
  installDom(authIds);
  globalThis.WebSocket = { OPEN: 1, CONNECTING: 0 };
  window.location.search = search;
  window.location.pathname = pathname;
  return createState();
}

export function installTerminalFakes() {
  globalThis.Terminal = class {
    constructor() {
      this.cols = 80;
      this.rows = 24;
      this.buffer = { active: { viewportY: 0, baseY: 0 } };
      this.handlers = {};
    }
    loadAddon(addon) {
      this.addon = addon;
    }
    open() {}
    onData(handler) {
      this.handlers.data = handler;
    }
    onResize(handler) {
      this.handlers.resize = handler;
    }
    write(data, callback) {
      this.data = data;
      callback?.();
    }
    scrollToBottom() {
      this.scrolled = true;
    }
    dispose() {
      this.disposed = true;
    }
  };
  globalThis.FitAddon = {
    FitAddon: class {
      fit() {
        this.fitted = true;
      }
    },
  };
}
