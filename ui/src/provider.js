import { parentEvent } from "./bridge.js";
import { setupTransfers } from "./transfers.js";
import { renderTabs } from "./sessions.js";

export async function setupProvider(state) {
  state.dom.provider.style.display = "flex";
  const targetSelect = document.getElementById("target-select");
  const userSelect = document.getElementById("user-select");
  const connect = document.getElementById("connect-provider");
  const error = document.getElementById("provider-error");
  let targets = [];
  const updateUsers = () => {
    userSelect.replaceChildren();
    const target = targets.find((item) => item.id === targetSelect.value);
    for (const user of target?.users || []) {
      const option = document.createElement("option");
      option.value = user;
      option.textContent = user;
      userSelect.appendChild(option);
    }
    connect.disabled = !target || userSelect.options.length === 0;
  };
  const renderTargets = () => {
    const selected = targetSelect.value;
    targetSelect.replaceChildren();
    for (const target of targets) {
      const option = document.createElement("option");
      option.value = target.id;
      option.textContent = target.label || target.id;
      targetSelect.appendChild(option);
    }
    if (targets.some((target) => target.id === selected)) targetSelect.value = selected;
    updateUsers();
  };
  try {
    connect.disabled = true;
    const response = await fetch(
      `${state.providerApiPrefix}/providers/${encodeURIComponent(state.providerName)}/targets`,
    );
    if (!response.ok) throw new Error(await response.text());
    const discovery = await response.json();
    targets = discovery.targets || [];
    renderTargets();
    targetSelect.addEventListener("change", updateUsers);
    connect.addEventListener("click", async () => {
      try {
        state.selectingNewSession = false;
        connect.disabled = true;
        error.textContent = "";
        const create = await fetch(
          `${state.providerApiPrefix}/providers/${encodeURIComponent(state.providerName)}/sessions${state.roomId ? `?roomId=${encodeURIComponent(state.roomId)}` : ""}`,
          {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ target: targetSelect.value, user: userSelect.value }),
          },
        );
        if (!create.ok) throw new Error(await create.text());
        const session = await create.json();
        const providerSession = {
          id: session.id,
          token: session.token,
          target: session.target,
          user: session.user,
          state: "connecting",
          socket: undefined,
          terminal: undefined,
          fitAddon: undefined,
          container: undefined,
        };
        state.providerSessions.set(providerSession.id, providerSession);
        state.activeProviderSession = providerSession;
        state.activeSessionId = providerSession.id;
        renderTabs(state);
        state.sendRoomEvent({
          type: "new-session",
          sessionId: session.id,
          token: session.token,
          target: session.target,
          user: session.user,
        });
        parentEvent(state, "connecting");
        state.dom.provider.style.display = "none";
        setupTransfers(state);
        state.attachSocket(
          `${state.providerApiPrefix}/sessions/${encodeURIComponent(session.id)}/ws?token=${encodeURIComponent(session.token)}`,
          providerSession,
        );
      } catch (caught) {
        connect.disabled = false;
        error.textContent = caught instanceof Error ? caught.message : String(caught);
      }
    });
  } catch (caught) {
    error.textContent = caught instanceof Error ? caught.message : String(caught);
  }
}
