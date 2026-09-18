export const MSG_INPUT = "0";
export const MSG_OUTPUT = "1";
export const MSG_RESIZE_TERMINAL = "2";
export const MSG_AUTH = "a";
export const MSG_AUTH_TRY = "b";
export const MSG_AUTH_OK = "c";
export const MSG_AUTH_FAILED = "d";

export function removeTerminalReports(data) {
  return data.replace(
    /\x1b\](?:10|11|12);rgb:[0-9a-f]+\/[0-9a-f]+\/[0-9a-f]+(?:\x07|\x1b\\)|\x1b\[\??\d+;\d+R/gi,
    "",
  );
}

export function terminalWebSocketUrl(state, path) {
  return `${state.wsProtocol}${state.wsHost}${state.wsPort}${path}`;
}
