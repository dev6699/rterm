export function hideTransfers() {
  const panel = document.getElementById("transfer-panel");
  const toggle = document.getElementById("transfer-toggle");
  if (panel) panel.style.display = "none";
  if (toggle) {
    toggle.style.display = "none";
    toggle.setAttribute("aria-expanded", "false");
  }
}

export function setupTransfers(state) {
  const panel = document.getElementById("transfer-panel");
  const toggle = document.getElementById("transfer-toggle");
  const close = document.getElementById("transfer-close");
  const uploadFile = document.getElementById("upload-file");
  const uploadPath = document.getElementById("upload-path");
  const uploadButton = document.getElementById("upload-button");
  const downloadPath = document.getElementById("download-path");
  const downloadButton = document.getElementById("download-button");
  const status = document.getElementById("transfer-status");
  toggle.style.display = "block";
  if (state.transferHandlersReady) return;
  state.transferHandlersReady = true;
  toggle.addEventListener("click", () => {
    panel.style.display = "block";
    toggle.style.display = "none";
    toggle.setAttribute("aria-expanded", "true");
  });
  close.addEventListener("click", () => {
    panel.style.display = "none";
    toggle.style.display = "block";
    toggle.setAttribute("aria-expanded", "false");
  });
  uploadPath.addEventListener("keydown", (event) => {
    if (event.key === "Enter") {
      event.preventDefault();
      uploadButton.click();
    }
  });
  downloadPath.addEventListener("keydown", (event) => {
    if (event.key === "Enter") {
      event.preventDefault();
      downloadButton.click();
    }
  });
  uploadButton.addEventListener("click", async () => {
    const file = uploadFile.files[0];
    if (!file || !uploadPath.value.trim()) {
      status.textContent = "Choose a file and enter a remote destination.";
      return;
    }
    try {
      uploadButton.disabled = true;
      status.textContent = `Uploading ${file.name}…`;
      const query = new URLSearchParams({ path: uploadPath.value.trim(), filename: file.name });
      const session = state.providerSessions.get(state.activeSessionId);
      const response = await fetch(
        `${state.providerApiPrefix}/sessions/${encodeURIComponent(state.activeSessionId)}/upload?${query}`,
        { method: "POST", headers: { Authorization: `Bearer ${session.token}` }, body: file },
      );
      if (!response.ok) throw new Error(await response.text());
      const result = await response.json();
      status.textContent = `Upload complete (${result.bytes || file.size} bytes).`;
    } catch (error) {
      status.textContent = `Upload failed: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      uploadButton.disabled = false;
    }
  });
  downloadButton.addEventListener("click", async () => {
    const remotePath = downloadPath.value.trim();
    if (!remotePath) {
      status.textContent = "Enter a remote source path.";
      return;
    }
    try {
      downloadButton.disabled = true;
      status.textContent = "Downloading…";
      const session = state.providerSessions.get(state.activeSessionId);
      const response = await fetch(
        `${state.providerApiPrefix}/sessions/${encodeURIComponent(state.activeSessionId)}/download?path=${encodeURIComponent(remotePath)}`,
        { headers: { Authorization: `Bearer ${session.token}` } },
      );
      if (!response.ok) throw new Error(await response.text());
      const blob = await response.blob();
      const link = document.createElement("a");
      link.href = URL.createObjectURL(blob);
      link.download = remotePath.split("/").pop() || "download";
      link.style.display = "none";
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(link.href);
      status.textContent = `Download complete (${blob.size} bytes).`;
    } catch (error) {
      status.textContent = `Download failed: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      downloadButton.disabled = false;
    }
  });
}
