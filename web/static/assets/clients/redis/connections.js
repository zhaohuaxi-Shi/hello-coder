import { esc, loadHTML } from "../../shared/util.js";
import { encryptSecret } from "../../shared/crypto.js";
import { mountDetailDrawer } from "./details.js";

const HTML_URL = "/assets/clients/redis/connections.html";

const MODE_LABEL = {
  standalone: "Standalone",
  cluster: "Cluster",
  sentinel: "Sentinel",
};

function statusBadge(available, message) {
  if (available == null) {
    return `<span class="badge unknown">Checking…</span>`;
  }
  if (available === false) {
    return `<span class="badge bad" title="${esc(message || "unavailable")}">DOWN</span>`;
  }
  return `<span class="badge ok" title="${esc(message || "ok")}">UP</span>`;
}

function modeLabel(mode) {
  return MODE_LABEL[mode] || mode || "—";
}

/** Cache / Details require a live connection; Edit / Delete always stay enabled. */
function actionButtons(available) {
  const dis = available === true ? "" : " disabled";
  return `<div class="row-actions">
    <button type="button" data-act="cache"${dis}>Cache</button>
    <button type="button" data-act="detail"${dis}>Details</button>
    <button type="button" data-act="edit">Edit</button>
    <button type="button" class="danger" data-act="del">Delete</button>
  </div>`;
}

export async function mount(root, ctx) {
  root.innerHTML = await loadHTML(HTML_URL);

  const api = ctx.api;
  const connTbody = root.querySelector("#conn-tbody");
  const dlgConn = root.querySelector("#dlg-conn");
  const formConn = root.querySelector("#form-conn");
  const modeSelect = root.querySelector("#conn-mode");
  let loadGen = 0;

  const detailDrawer = await mountDetailDrawer(root, { api });

  function splitAddrs(raw) {
    return String(raw || "")
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
  }

  function looksLikeHostPort(addr) {
    // host:port or [ipv6]:port
    if (addr.startsWith("[")) {
      const m = addr.match(/^\[[^\]]+\]:(\d+)$/);
      return !!(m && Number(m[1]) > 0 && Number(m[1]) <= 65535);
    }
    const i = addr.lastIndexOf(":");
    if (i <= 0) return false;
    const port = Number(addr.slice(i + 1));
    const host = addr.slice(0, i).trim();
    return host.length > 0 && Number.isInteger(port) && port > 0 && port <= 65535;
  }

  /** Mode-specific required fields (aligned with ARDM). */
  function validateFormFields() {
    const mode = modeSelect.value || "standalone";
    const name = root.querySelector("#conn-name").value.trim();
    if (!name) return "Name 必填";
    if (!mode) return "Mode 必填";

    if (mode === "standalone") {
      const addresses = root.querySelector("#conn-addr-standalone").value.trim();
      const addrs = splitAddrs(addresses);
      const db = Number(root.querySelector("#conn-db-standalone").value);
      if (!addrs.length) return "Standalone 必须填写 Address（host:port）";
      if (addrs.length !== 1) return "Standalone 只填一个 Address";
      if (!looksLikeHostPort(addrs[0])) return `地址格式无效: ${addrs[0]}（应为 host:port）`;
      if (!Number.isFinite(db) || db < 0 || !Number.isInteger(db)) return "DB 必须是 >= 0 的整数";
      return "";
    }

    if (mode === "cluster") {
      const addresses = root.querySelector("#conn-addr-cluster").value.trim();
      const addrs = splitAddrs(addresses);
      if (!addrs.length) return "Cluster 必须填写至少一个 Seed node（host:port）";
      for (const a of addrs) {
        if (!looksLikeHostPort(a)) return `地址格式无效: ${a}（应为 host:port）`;
      }
      return "";
    }

    // sentinel
    const addresses = root.querySelector("#conn-addr-sentinel").value.trim();
    const addrs = splitAddrs(addresses);
    const masterName = root.querySelector("#conn-master").value.trim();
    const db = Number(root.querySelector("#conn-db-sentinel").value);
    if (!addrs.length) return "Sentinel 必须填写至少一个 Sentinel 地址（host:port）";
    for (const a of addrs) {
      if (!looksLikeHostPort(a)) return `地址格式无效: ${a}（应为 host:port）`;
    }
    if (!masterName) return "Sentinel 必须填写 Master name（如 mymaster）";
    if (!Number.isFinite(db) || db < 0 || !Number.isInteger(db)) return "DB 必须是 >= 0 的整数";
    return "";
  }

  function syncModeFields() {
    const mode = modeSelect.value || "standalone";
    root.querySelectorAll("[data-mode-panel]").forEach((panel) => {
      panel.hidden = panel.dataset.modePanel !== mode;
    });
    root.querySelector("#conn-addr-standalone").required = mode === "standalone";
    root.querySelector("#conn-addr-cluster").required = mode === "cluster";
    root.querySelector("#conn-addr-sentinel").required = mode === "sentinel";
    root.querySelector("#conn-master").required = mode === "sentinel";
  }

  function fillModeAddresses(mode, addresses, db, masterName) {
    root.querySelector("#conn-addr-standalone").value = "";
    root.querySelector("#conn-addr-cluster").value = "";
    root.querySelector("#conn-addr-sentinel").value = "";
    root.querySelector("#conn-db-standalone").value = "0";
    root.querySelector("#conn-db-sentinel").value = "0";
    root.querySelector("#conn-master").value = "";
    if (mode === "cluster") {
      root.querySelector("#conn-addr-cluster").value = addresses || "";
    } else if (mode === "sentinel") {
      root.querySelector("#conn-addr-sentinel").value = addresses || "";
      root.querySelector("#conn-master").value = masterName || "";
      root.querySelector("#conn-db-sentinel").value = String(db ?? 0);
    } else {
      root.querySelector("#conn-addr-standalone").value = addresses || "";
      root.querySelector("#conn-db-standalone").value = String(db ?? 0);
    }
  }

  function applyRowStatus(tr, ping) {
    if (!tr?.isConnected) return;
    const available = !!ping.available;
    const cells = tr.children;
    cells[4].innerHTML = statusBadge(available, ping.message);
    cells[5].innerHTML = actionButtons(available);
    tr.dataset.available = available ? "1" : "0";
  }

  async function probeRow(tr, id) {
    try {
      const ping = await api(`/api/v1/redis/conns/${id}/ping`, { method: "POST" });
      applyRowStatus(tr, ping);
      return ping;
    } catch (e) {
      applyRowStatus(tr, { available: false, message: e.message });
      return null;
    }
  }

  async function loadConns() {
    const gen = ++loadGen;
    connTbody.innerHTML = `<tr><td colspan="6" class="empty">Loading…</td></tr>`;
    try {
      const rows = await api(`/api/v1/redis/conns?check=0`);
      if (gen !== loadGen) return;
      if (!rows.length) {
        connTbody.innerHTML = `<tr><td colspan="6" class="empty">No connections yet. Click "New connection".</td></tr>`;
        return;
      }
      connTbody.innerHTML = rows
        .map(
          (r) => `<tr data-id="${r.id}">
          <td><strong>${esc(r.name)}</strong></td>
          <td>${esc(modeLabel(r.mode))}</td>
          <td><code>${esc(r.addresses)}</code>${
            r.mode === "sentinel" && r.masterName
              ? `<div class="muted" style="margin:.2rem 0 0;font-size:.75rem">master: ${esc(r.masterName)}</div>`
              : ""
          }</td>
          <td>${esc(r.version || "—")}</td>
          <td>${statusBadge(null)}</td>
          <td>${actionButtons(null)}</td>
        </tr>`
        )
        .join("");

      const probes = [...connTbody.querySelectorAll("tr[data-id]")].map((tr) =>
        probeRow(tr, tr.dataset.id).then(() => {
          if (gen !== loadGen) return;
        })
      );
      await Promise.allSettled(probes);
    } catch (e) {
      if (gen !== loadGen) return;
      connTbody.innerHTML = `<tr><td colspan="6" class="empty">${esc(e.message)}</td></tr>`;
    }
  }

  async function openConnDialog(id) {
    root.querySelector("#dlg-conn-title").textContent = id ? "Edit connection" : "New connection";
    root.querySelector("#conn-id").value = id || "";
    root.querySelector("#conn-msg").hidden = true;
    root.querySelector("#conn-name").value = "";
    modeSelect.value = "standalone";
    root.querySelector("#conn-user").value = "";
    root.querySelector("#conn-pass").value = "";
    root.querySelector("#conn-desc").value = "";
    fillModeAddresses("standalone", "", 0, "");
    syncModeFields();
    if (id) {
      try {
        const row = await api(`/api/v1/redis/conns/${id}`);
        root.querySelector("#conn-name").value = row.name || "";
        modeSelect.value = row.mode || "standalone";
        root.querySelector("#conn-user").value = row.username || "";
        root.querySelector("#conn-desc").value = row.description || "";
        fillModeAddresses(row.mode || "standalone", row.addresses || "", row.db ?? 0, row.masterName || "");
        syncModeFields();
      } catch (e) {
        alert(e.message);
        return;
      }
    }
    dlgConn.showModal();
  }

  connTbody.addEventListener("click", async (e) => {
    const btn = e.target.closest("button[data-act]");
    if (!btn || btn.disabled) return;
    const tr = btn.closest("tr");
    const id = tr?.dataset.id;
    if (!id) return;
    const act = btn.dataset.act;
    if (act === "cache") {
      ctx.navigate("redis", "cache", {
        connId: id,
        connName: tr.querySelector("strong")?.textContent || "",
      });
    } else if (act === "detail") {
      detailDrawer.openDetail(id, tr.querySelector("strong")?.textContent || "");
    } else if (act === "edit") {
      openConnDialog(id);
    } else if (act === "del") {
      if (!confirm("Delete this connection?")) return;
      try {
        await api(`/api/v1/redis/conns/${id}`, { method: "DELETE" });
        loadConns();
      } catch (err) {
        alert(err.message);
      }
    }
  });

  async function formPayload() {
    const id = root.querySelector("#conn-id").value;
    const password = root.querySelector("#conn-pass").value;
    const mode = modeSelect.value || "standalone";
    let addresses = "";
    let masterName = "";
    let db = 0;
    if (mode === "cluster") {
      addresses = root.querySelector("#conn-addr-cluster").value.trim();
      db = 0;
    } else if (mode === "sentinel") {
      addresses = root.querySelector("#conn-addr-sentinel").value.trim();
      masterName = root.querySelector("#conn-master").value.trim();
      db = Number(root.querySelector("#conn-db-sentinel").value);
    } else {
      addresses = root.querySelector("#conn-addr-standalone").value.trim();
      db = Number(root.querySelector("#conn-db-standalone").value);
    }
    if (!Number.isFinite(db) || db < 0) db = 0;
    return {
      id: id ? Number(id) : 0,
      name: root.querySelector("#conn-name").value.trim(),
      mode,
      addresses,
      masterName,
      db,
      username: root.querySelector("#conn-user").value.trim(),
      password: await encryptSecret(password, api),
      description: root.querySelector("#conn-desc").value.trim(),
    };
  }

  function showConnMsg(text, ok) {
    const msg = root.querySelector("#conn-msg");
    msg.hidden = false;
    msg.className = ok ? "msg is-ok" : "msg is-error";
    msg.textContent = text;
  }

  modeSelect.addEventListener("change", syncModeFields);
  root.querySelector("#btn-refresh-conns").addEventListener("click", loadConns);
  root.querySelector("#btn-add-conn").addEventListener("click", () => openConnDialog(null));
  root.querySelector("#conn-cancel").addEventListener("click", () => dlgConn.close());

  root.querySelector("#conn-test").addEventListener("click", async () => {
    const err = validateFormFields();
    if (err) {
      showConnMsg(err, false);
      return;
    }
    const payload = await formPayload();
    const btn = root.querySelector("#conn-test");
    btn.disabled = true;
    try {
      const ping = await api(`/api/v1/redis/conns/test`, { method: "POST", body: JSON.stringify(payload) });
      if (ping.available) {
        const detail = [ping.mode, ping.role, ping.version ? `v${ping.version}` : ""]
          .filter(Boolean)
          .join(" · ");
        showConnMsg(`Available: ${detail || ping.message || "ok"}`, true);
      } else {
        showConnMsg(`Unavailable: ${ping.message || "failed"}`, false);
      }
    } catch (e) {
      showConnMsg(e.message, false);
    } finally {
      btn.disabled = false;
    }
  });

  formConn.addEventListener("submit", async (e) => {
    e.preventDefault();
    const err = validateFormFields();
    if (err) {
      showConnMsg(err, false);
      return;
    }
    const payload = await formPayload();
    const id = payload.id || "";
    try {
      if (id) {
        await api(`/api/v1/redis/conns/${id}`, { method: "PUT", body: JSON.stringify(payload) });
      } else {
        await api(`/api/v1/redis/conns`, { method: "POST", body: JSON.stringify(payload) });
      }
      dlgConn.close();
      loadConns();
    } catch (e) {
      showConnMsg(e.message, false);
    }
  });

  await loadConns();

  return {
    unmount() {
      loadGen++;
      detailDrawer.unmount();
      dlgConn.close();
      root.innerHTML = "";
    },
  };
}
