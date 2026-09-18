import test from "node:test";
import assert from "node:assert/strict";
import { acceptsParentMessage, parentEvent } from "../src/bridge.js";

test("accepts only messages from the configured parent and token", () => {
  const parent = {};
  globalThis.window = { parent };
  const state = { embedded: true, parentOrigin: "https://host.test", bridgeToken: "token" };
  assert.equal(
    acceptsParentMessage(state, {
      source: parent,
      origin: "https://host.test",
      data: { bridgeToken: "token" },
    }),
    true,
  );
  assert.equal(
    acceptsParentMessage(state, {
      source: parent,
      origin: "https://other.test",
      data: { bridgeToken: "token" },
    }),
    false,
  );
  assert.equal(
    acceptsParentMessage(state, {
      source: {},
      origin: "https://host.test",
      data: { bridgeToken: "token" },
    }),
    false,
  );
  assert.equal(
    acceptsParentMessage(state, {
      source: parent,
      origin: "https://host.test",
      data: { bridgeToken: "wrong" },
    }),
    false,
  );
});

test("emits token-bearing events to the exact parent origin", () => {
  const messages = [];
  const parent = { postMessage: (...message) => messages.push(message) };
  globalThis.window = { parent };
  parentEvent(
    { embedded: true, bridgeToken: "secret", parentOrigin: "https://host.test" },
    "loaded",
    { ready: true },
  );
  assert.deepEqual(messages, [
    [{ source: "rterm", type: "loaded", bridgeToken: "secret", ready: true }, "https://host.test"],
  ]);
});
