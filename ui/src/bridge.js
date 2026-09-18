export function parentEvent(state, type, data = {}) {
  if (!state.embedded || window.parent === window || !state.bridgeToken || !state.parentOrigin)
    return;
  window.parent.postMessage(
    { source: "rterm", type, bridgeToken: state.bridgeToken, ...data },
    state.parentOrigin,
  );
}

export function acceptsParentMessage(state, event) {
  return Boolean(
    state.embedded &&
    event.source === window.parent &&
    event.origin === state.parentOrigin &&
    event.data &&
    event.data.bridgeToken === state.bridgeToken,
  );
}
