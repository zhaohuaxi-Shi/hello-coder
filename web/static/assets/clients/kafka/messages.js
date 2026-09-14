import { esc, loadHTML } from "../../shared/util.js";

const HTML_URL = "/assets/clients/kafka/messages.html";

function clip(s, n = 120) {
  const t = String(s ?? "");
  if (t.length <= n) return t;
  return `${t.slice(0, n)}…`;
}

function normalizeDateTimeLocal(value) {
  const v = String(value ?? "").trim().replace("T", " ");
  if (!v) return "";
  if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/.test(v)) return `${v}:00`;
  return v;
}

export async function mount(root, ctx, params = {}) {
  const { connId, connName, topic } = params;
  if (!connId || !topic) {
    ctx.navigate("kafka", "connections");
    return { unmount() {} };
  }

  root.innerHTML = await loadHTML(HTML_URL);

  const api = ctx.api;
  const partitionEl = root.querySelector("#msg-partition");
  const fromEl = root.querySelector("#msg-from");
  const offsetField = root.querySelector("#msg-offset-field");
  const offsetEl = root.querySelector("#msg-offset");
  const sinceEl = root.querySelector("#msg-since");
  const untilEl = root.querySelector("#msg-until");
  const limitEl = root.querySelector("#msg-limit");
  const tbody = root.querySelector("#msg-tbody");
  const statusEl = root.querySelector("#msg-status");
  const consumerBtn = root.querySelector("#btn-consumer");
  const producerBtn = root.querySelector("#btn-producer");
  const dlgProduce = root.querySelector("#dlg-produce");
  const formProduce = root.querySelector("#form-produce");
  const producePartitionEl = root.querySelector("#produce-partition");
  const produceMsg = root.querySelector("#produce-msg");
  const produceSubmit = root.querySelector("#produce-submit");
  const dlgMessage = root.querySelector("#dlg-message");
  const dlgMessageBody = root.querySelector("#dlg-message-body");
  const dlgMessageMeta = root.querySelector("#dlg-message-meta");
  const dlgMessageHint = root.querySelector("#dlg-message-hint");
  const btnFormatJson = root.querySelector("#btn-format-json");

  let loadedMessages = [];
  let currentRawValue = "";

  root.querySelector("#messages-title").textContent = `Topic · ${topic}`;
  root.querySelector("#messages-sub").textContent = `${connName || "Connection"} · #${connId}`;

  function setStatus(text, isError) {
    if (!text) {
      statusEl.hidden = true;
      statusEl.textContent = "";
      return;
    }
    statusEl.hidden = false;
    statusEl.className = `msg ${isError ? "is-error" : "is-ok"}`;
    statusEl.textContent = text;
  }

  function openMessageDetail(m) {
    currentRawValue = m.value ?? "";
    root.querySelector("#dlg-message-title").textContent = `Message · offset ${m.offset}`;
    dlgMessageMeta.textContent = `Partition ${m.partition} · ${m.timestamp || "-"} · key: ${m.key || "—"}`;
    dlgMessageBody.textContent = currentRawValue || "(empty)";
    dlgMessageHint.hidden = true;
    dlgMessageHint.textContent = "";
    dlgMessage.showModal();
  }

  fromEl.addEventListener("change", () => {
    offsetField.hidden = fromEl.value !== "offset";
  });

  root.querySelector("#btn-back-topics").addEventListener("click", () => {
    ctx.navigate("kafka", "topics", { connId, connName });
  });

  try {
    const detail = await api(`/api/v1/kafka/conns/${connId}/topics/${encodeURIComponent(topic)}`);
    const parts = (detail.partitions || []).map((p) => p.id).sort((a, b) => a - b);
    const partOpts = parts.map((id) => `<option value="${id}">${id}</option>`).join("");
    partitionEl.innerHTML = `<option value="">All</option>` + partOpts;
    producePartitionEl.innerHTML = `<option value="">Auto</option>` + partOpts;
  } catch (e) {
    setStatus(`Failed to load partitions: ${e.message}`, true);
  }

  async function runConsumer() {
    setStatus("");
    const from = fromEl.value;
    const limit = Number(limitEl.value) || 20;
    const since = normalizeDateTimeLocal(sinceEl.value);
    const until = normalizeDateTimeLocal(untilEl.value);
    if (since && until && since >= until) {
      setStatus("Start Time must be before End Time", true);
      return;
    }
    const payload = { topic, from, limit };
    if (partitionEl.value !== "") {
      payload.partition = Number(partitionEl.value);
    }
    if (from === "offset") {
      payload.offset = Number(offsetEl.value) || 0;
    }
    if (since) payload.since = since;
    if (until) payload.until = until;

    consumerBtn.disabled = true;
    tbody.innerHTML = `<tr><td colspan="3" class="empty">Loading…</td></tr>`;
    try {
      const rows = await api(`/api/v1/kafka/conns/${connId}/consume`, {
        method: "POST",
        body: JSON.stringify(payload),
      });
      loadedMessages = rows || [];
      if (!loadedMessages.length) {
        tbody.innerHTML = `<tr><td colspan="3" class="empty">No messages</td></tr>`;
        setStatus("Consumed 0 messages", false);
        return;
      }
      tbody.innerHTML = loadedMessages
        .map(
          (m, i) => `<tr data-idx="${i}">
          <td class="col-message">
            <button type="button" class="msg-link" data-act="open-msg" title="${esc(m.value)}">${esc(clip(m.value, 220)) || "(empty)"}</button>
          </td>
          <td class="col-time">${esc(m.timestamp || "-")}</td>
          <td class="col-part" title="Partition ${m.partition}">${m.partition}</td>
        </tr>`
        )
        .join("");
      setStatus(`Consumed ${loadedMessages.length} message(s)`, false);
    } catch (err) {
      loadedMessages = [];
      tbody.innerHTML = `<tr><td colspan="3" class="empty">${esc(err.message)}</td></tr>`;
      setStatus(err.message, true);
    } finally {
      consumerBtn.disabled = false;
    }
  }

  consumerBtn.addEventListener("click", runConsumer);

  tbody.addEventListener("click", (e) => {
    const btn = e.target.closest("[data-act='open-msg']");
    if (!btn) return;
    const tr = btn.closest("tr");
    const idx = Number(tr?.dataset.idx);
    const m = loadedMessages[idx];
    if (m) openMessageDetail(m);
  });

  root.querySelector("#message-close").addEventListener("click", () => dlgMessage.close());

  btnFormatJson.addEventListener("click", () => {
    dlgMessageHint.hidden = true;
    const raw = currentRawValue.trim();
    if (!raw) {
      dlgMessageHint.hidden = false;
      dlgMessageHint.className = "msg is-error";
      dlgMessageHint.textContent = "Empty message";
      return;
    }
    try {
      const parsed = JSON.parse(raw);
      dlgMessageBody.textContent = JSON.stringify(parsed, null, 2);
      dlgMessageHint.hidden = false;
      dlgMessageHint.className = "msg is-ok";
      dlgMessageHint.textContent = "Formatted as JSON";
    } catch (err) {
      dlgMessageHint.hidden = false;
      dlgMessageHint.className = "msg is-error";
      dlgMessageHint.textContent = "Not valid JSON";
    }
  });

  producerBtn.addEventListener("click", () => {
    produceMsg.hidden = true;
    produceMsg.textContent = "";
    root.querySelector("#produce-key").value = "";
    root.querySelector("#produce-value").value = "";
    producePartitionEl.value = "";
    dlgProduce.showModal();
  });

  root.querySelector("#produce-cancel").addEventListener("click", () => dlgProduce.close());

  formProduce.addEventListener("submit", async (e) => {
    e.preventDefault();
    produceMsg.hidden = true;
    const payload = {
      topic,
      key: root.querySelector("#produce-key").value,
      value: root.querySelector("#produce-value").value,
    };
    if (producePartitionEl.value !== "") {
      payload.partition = Number(producePartitionEl.value);
    }
    produceSubmit.disabled = true;
    try {
      const res = await api(`/api/v1/kafka/conns/${connId}/produce`, {
        method: "POST",
        body: JSON.stringify(payload),
      });
      dlgProduce.close();
      setStatus(`Produced to partition ${res.partition} @ offset ${res.offset}`, false);
    } catch (err) {
      produceMsg.hidden = false;
      produceMsg.className = "msg is-error";
      produceMsg.textContent = err.message;
    } finally {
      produceSubmit.disabled = false;
    }
  });

  return {
    unmount() {
      dlgProduce.close();
      dlgMessage.close();
      root.innerHTML = "";
    },
  };
}
