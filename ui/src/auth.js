import { MSG_AUTH_TRY } from "./protocol.js";
import { sendSocket } from "./terminal.js";

export function clearDigits() {
  for (let i = 1; i <= 6; i++) document.getElementById(`digit${i}`).value = "";
  document.getElementById("digit1").focus();
  document.getElementById("result").textContent = "Invalid code";
}

export function setupAuth(state, parentEvent) {
  const submitCode = () => {
    const code = Array.from(
      { length: 6 },
      (_, index) => document.getElementById(`digit${index + 1}`).value,
    ).join("");
    sendSocket(state, MSG_AUTH_TRY + code, parentEvent);
  };
  for (let i = 1; i <= 6; i++) {
    document.getElementById(`digit${i}`).addEventListener("input", () => {
      const input = document.getElementById(`digit${i}`);
      if (input.value.length !== 1) return;
      if (i < 6) {
        document.getElementById("result").textContent = "";
        document.getElementById(`digit${i + 1}`).focus();
      } else submitCode();
    });
  }
  return { clearDigits };
}
