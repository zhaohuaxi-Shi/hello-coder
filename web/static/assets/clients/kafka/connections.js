import { esc, loadHTML } from "../../shared/util.js";
import { encryptSecret } from "../../shared/crypto.js";

const HTML_URL = "/assets/clients/kafka/connections.html";

function statusBadge(available, message) {
  if (available == null) {
    return `<span class="badge unknown">Checking…</span>`;
  }
  if (available === false) {
    return `<span class="badge bad" title="${esc(message || "unavailable")}">DOWN</span>`;
  }
  return `<span class="badge ok" title="${esc(message || "ok")}">UP</span>`;
}

function yesNo(v) {
  return v ? `<span class="badge ok">Yes</span>` : `<span class="badge unknown">No</span>`;
}

function fmtCount(n) {
  const v = Number(n);
  if (!Number.isFinite(v)) return "—";
  return v.toLocaleString();
}

function fmtBytes(n) {
  const v = Number(n);
  if (!Number.isFinite(v) || v <= 0) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let x = v;
  while (x >= 1024 && i < units.length - 1) {
    x /= 1024;
    i += 1;
  }
  return `${x.toFixed(i === 0 ? 0 : 2)} ${units[i]}`;
}

/** Topics / Details require a live connection; Edit / Delete always stay enabled. */
function actionButtons(available) {
  const dis = available === true ? "" : " disabled";
  return `<div class="row-actions">
    <button type="button" data-act="topics"${dis}>Topics</button>
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
  const drawer = root.querySelector("#drawer-detail");
  const summaryEl = root.querySelector("#cluster-summary");
  const nodeTbody = root.querySelector("#node-tbody");
  let detailConnId = null;
  let loadGen = 0;

  function openDrawer() {
    drawer.classList.add("is-open");
    drawer.setAttribute("aria-hidden", "false");
    document.body.classList.add("drawer-open");
  }

  function closeDrawer() {
    drawer.classList.remove("is-open");
    drawer.setAttribute("aria-hidden", "true");
    document.body.classList.remove("drawer-open");
    detailConnId = null;
  }

  function renderSummary(ov) {
    if (!ov?.available) {
      summaryEl.innerHTML = `<div class="empty-card empty">Unavailable: ${esc(ov?.message || "failed")}</div>`;
      return;
    }
    const controller =
      ov.controllerId >= 0
        ? `${ov.controllerId}${ov.controllerAddr ? ` · ${ov.controllerAddr}` : ""}`
        : "—";
    const items = [
      ["Cluster ID", esc(ov.clusterId || "—")],
      ["Controller", esc(controller)],
      ["Brokers", esc(fmtCount(ov.brokerCount))],
      ["Topics", esc(fmtCount(ov.topicCount))],
      ["Partitions", esc(fmtCount(ov.partitionCount))],
      ["Groups", esc(fmtCount(ov.groupCount))],
    ];
    summaryEl.innerHTML = items
      .map(([k, v]) => `<div class="kv-item"><div class="kv-label">${k}</div><div class="kv-value">${v}</div></div>`)
      .join("");
  }

  async function loadDetail() {
    if (!detailConnId) return;
    nodeTbody.innerHTML = `<tr><td colspan="8" class="empty">Loading…</td></tr>`;
    try {
      const data = await api(`/api/v1/kafka/conns/${detailConnId}/cluster`);
      renderSummary(data.overview || {});
      const nodes = data.nodes || [];
      if (!nodes.length) {
        nodeTbody.innerHTML = `<tr><td colspan="8" class="empty">No brokers returned</td></tr>`;
        return;
      }
      nodeTbody.innerHTML = nodes
        .map((n) => {
          const logHint = n.logDirCount ? `${n.logDirCount} dir(s)` : "";
          return `<tr>
            <td>
              <strong>${esc(String(n.id))}</strong>
              ${n.isController ? `<div class="muted" style="margin:.2rem 0 0;font-size:.75rem">controller</div>` : ""}
            </td>
            <td><code>${esc(n.addr || "—")}</code></td>
            <td>${esc(n.rack || "—")}</td>
            <td>${yesNo(!!n.isController)}</td>
            <td>${yesNo(!!n.connected)}</td>
            <td>${esc(fmtCount(n.leaderCount))}</td>
            <td>${esc(fmtCount(n.replicaCount))}</td>
            <td title="${esc(logHint)}">${esc(fmtBytes(n.logDirSize))}</td>
          </tr>`;
        })
        .join("");
    } catch (e) {
      summaryEl.innerHTML = "";
      nodeTbody.innerHTML = `<tr><td colspan="8" class="empty">${esc(e.message)}</td></tr>`;
    }
  }

  async function openDetail(id, name) {
    detailConnId = id;
    root.querySelector("#drawer-title").textContent = `Cluster · ${name || "#" + id}`;
    root.querySelector("#drawer-sub").textContent = `Connection #${id}`;
    summaryEl.innerHTML = "";
    nodeTbody.innerHTML = `<tr><td colspan="8" class="empty">Loading…</td></tr>`;
    openDrawer();
    await loadDetail();
  }

  function applyRowStatus(tr, ping) {
    if (!tr?.isConnected) return;
    const available = !!ping.available;
    const cells = tr.children;
    cells[3].innerHTML = statusBadge(available, ping.message);
    cells[5].innerHTML = actionButtons(available);
    tr.dataset.available = available ? "1" : "0";
  }

  async function probeRow(tr, id) {
    try {
      const ping = await api(`/api/v1/kafka/conns/${id}/ping`, { method: "POST" });
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
      const rows = await api(`/api/v1/kafka/conns?check=0`);
      if (gen !== loadGen) return;
      if (!rows.length) {
        connTbody.innerHTML = `<tr><td colspan="6" class="empty">No connections yet. Click "New connection".</td></tr>`;
        return;
      }
      connTbody.innerHTML = rows
        .map(
          (r) => `<tr data-id="${r.id}">
          <td><strong>${esc(r.name)}</strong></td>
          <td><code>${esc(r.brokers)}</code></td>
          <td>${esc(r.securityProtocol)}</td>
          <td>${statusBadge(null)}</td>
          <td>${esc(r.createdAt)}</td>
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
    root.querySelector("#conn-brokers").value = "";
    root.querySelector("#conn-protocol").value = "PLAINTEXT";
    root.querySelector("#conn-sasl-mech").value = "";
    root.querySelector("#conn-sasl-user").value = "";
    root.querySelector("#conn-sasl-pass").value = "";
    root.querySelector("#conn-desc").value = "";
    if (id) {
      try {
        const row = await api(`/api/v1/kafka/conns/${id}`);
        root.querySelector("#conn-name").value = row.name || "";
        root.querySelector("#conn-brokers").value = row.brokers || "";
        root.querySelector("#conn-protocol").value = row.securityProtocol || "PLAINTEXT";
        root.querySelector("#conn-sasl-mech").value = row.saslMechanism || "";
        root.querySelector("#conn-sasl-user").value = row.saslUsername || "";
        root.querySelector("#conn-desc").value = row.description || "";
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
    if (act === "topics") {
      ctx.navigate("kafka", "topics", {
        connId: id,
        connName: tr.querySelector("strong")?.textContent || "",
      });
    } else if (act === "detail") {
      openDetail(id, tr.querySelector("strong")?.textContent || "");
    } else if (act === "edit") {
      openConnDialog(id);
    } else if (act === "del") {
      if (!confirm("Delete this connection?")) return;
      try {
        await api(`/api/v1/kafka/conns/${id}`, { method: "DELETE" });
        loadConns();
      } catch (err) {
        alert(err.message);
      }
    }
  });

  async function formPayload() {
    const id = root.querySelector("#conn-id").value;
    const saslPassword = root.querySelector("#conn-sasl-pass").value;
    return {
      id: id ? Number(id) : 0,
      name: root.querySelector("#conn-name").value.trim(),
      brokers: root.querySelector("#conn-brokers").value.trim(),
      securityProtocol: root.querySelector("#conn-protocol").value,
      saslMechanism: root.querySelector("#conn-sasl-mech").value.trim(),
      saslUsername: root.querySelector("#conn-sasl-user").value.trim(),
      saslPassword: await encryptSecret(saslPassword, api),
      description: root.querySelector("#conn-desc").value.trim(),
    };
  }

  function showConnMsg(text, ok) {
    const msg = root.querySelector("#conn-msg");
    msg.hidden = false;
    msg.className = ok ? "msg is-ok" : "msg is-error";
    msg.textContent = text;
  }

  drawer.addEventListener("click", (e) => {
    if (e.target.closest("[data-drawer-close]")) closeDrawer();
  });

  function onKeydown(e) {
    if (e.key === "Escape" && drawer.classList.contains("is-open")) {
      closeDrawer();
    }
  }
  document.addEventListener("keydown", onKeydown);

  root.querySelector("#btn-refresh-detail").addEventListener("click", loadDetail);
  root.querySelector("#btn-refresh-conns").addEventListener("click", loadConns);
  root.querySelector("#btn-add-conn").addEventListener("click", () => openConnDialog(null));
  root.querySelector("#conn-cancel").addEventListener("click", () => dlgConn.close());

  root.querySelector("#conn-test").addEventListener("click", async () => {
    const payload = await formPayload();
    if (!payload.brokers) {
      showConnMsg("Brokers is required", false);
      return;
    }
    const btn = root.querySelector("#conn-test");
    btn.disabled = true;
    try {
      const ping = await api(`/api/v1/kafka/conns/test`, { method: "POST", body: JSON.stringify(payload) });
      if (ping.available) {
        showConnMsg(`Available: ${ping.message || "ok"}`, true);
      } else {
        showConnMsg(`Unavailable: ${ping.message || "failed"}`, false);
      }
    } catch (err) {
      showConnMsg(err.message, false);
    } finally {
      btn.disabled = false;
    }
  });

  formConn.addEventListener("submit", async (e) => {
    e.preventDefault();
    const payload = await formPayload();
    const id = payload.id || "";
    try {
      if (id) {
        await api(`/api/v1/kafka/conns/${id}`, { method: "PUT", body: JSON.stringify(payload) });
      } else {
        await api(`/api/v1/kafka/conns`, { method: "POST", body: JSON.stringify(payload) });
      }
      dlgConn.close();
      loadConns();
    } catch (err) {
      showConnMsg(err.message, false);
    }
  });

  await loadConns();

  return {
    unmount() {
      loadGen++;
      document.removeEventListener("keydown", onKeydown);
      closeDrawer();
      dlgConn.close();
      root.innerHTML = "";
    },
  };
}
