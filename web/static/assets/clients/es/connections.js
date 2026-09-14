import { esc, loadHTML } from "../../shared/util.js";
import { encryptSecret } from "../../shared/crypto.js";

const HTML_URL = "/assets/clients/es/connections.html";

function statusBadge(available, status, message) {
  if (available == null) {
    return `<span class="badge unknown">Checking…</span>`;
  }
  if (available === false) {
    return `<span class="badge bad" title="${esc(message || "unavailable")}">DOWN</span>`;
  }
  const s = String(status || "").toLowerCase();
  if (s === "green") return `<span class="badge ok">GREEN</span>`;
  if (s === "yellow") return `<span class="badge warn">YELLOW</span>`;
  if (s === "red") return `<span class="badge bad">RED</span>`;
  return `<span class="badge unknown" title="${esc(message || "")}">UNKNOWN</span>`;
}

function clusterStatusBadge(status) {
  const s = String(status || "").toLowerCase();
  if (s === "green") return `<span class="badge ok">GREEN</span>`;
  if (s === "yellow") return `<span class="badge warn">YELLOW</span>`;
  if (s === "red") return `<span class="badge bad">RED</span>`;
  return `<span class="badge unknown">${esc(status || "UNKNOWN")}</span>`;
}

function yesNo(v) {
  return v ? `<span class="badge ok">Yes</span>` : `<span class="badge unknown">No</span>`;
}

function roleTags(roles) {
  if (!roles?.length) return `<span class="badge unknown">coordinating</span>`;
  return roles.map((r) => `<span class="badge unknown">${esc(r)}</span>`).join(" ");
}

/** Indices / Details require a live connection; Edit / Delete always stay enabled. */
function actionButtons(available) {
  const dis = available === true ? "" : " disabled";
  return `<div class="row-actions">
    <button type="button" data-act="indices"${dis}>Indices</button>
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
    const items = [
      ["Status", clusterStatusBadge(ov.status)],
      ["Cluster", esc(ov.clusterName || "—")],
      ["UUID", esc(ov.clusterUuid || "—")],
      ["Version", esc(ov.version || "—")],
      ["Nodes", esc(String(ov.numberOfNodes ?? "—"))],
      ["Data nodes", esc(String(ov.numberOfDataNodes ?? "—"))],
      ["Active shards", esc(String(ov.activeShards ?? "—"))],
      ["Primary shards", esc(String(ov.activePrimaryShards ?? "—"))],
      ["Relocating", esc(String(ov.relocatingShards ?? 0))],
      ["Initializing", esc(String(ov.initializingShards ?? 0))],
      ["Unassigned", esc(String(ov.unassignedShards ?? 0))],
    ];
    summaryEl.innerHTML = items
      .map(([k, v]) => `<div class="kv-item"><div class="kv-label">${k}</div><div class="kv-value">${v}</div></div>`)
      .join("");
  }

  async function loadDetail() {
    if (!detailConnId) return;
    nodeTbody.innerHTML = `<tr><td colspan="8" class="empty">Loading…</td></tr>`;
    try {
      const data = await api(`/api/v1/es/conns/${detailConnId}/cluster`);
      renderSummary(data.overview || {});
      const nodes = data.nodes || [];
      if (!nodes.length) {
        nodeTbody.innerHTML = `<tr><td colspan="8" class="empty">No nodes returned</td></tr>`;
        return;
      }
      nodeTbody.innerHTML = nodes
        .map((n) => {
          const jvm = [n.jvmVersion, n.jvmHeapMax ? `heap ${n.jvmHeapMax}` : "", n.jvmPid ? `pid ${n.jvmPid}` : ""]
            .filter(Boolean)
            .join(" · ");
          const cpu = [
            n.availableProcessors ? `${n.availableProcessors} avail` : "",
            n.allocatedProcessors ? `${n.allocatedProcessors} alloc` : "",
          ]
            .filter(Boolean)
            .join(" / ");
          const plugins = (n.plugins || []).slice(0, 6).map((p) => esc(p)).join(", ") || "—";
          const more = (n.plugins || []).length > 6 ? ` (+${n.plugins.length - 6})` : "";
          return `<tr>
            <td>
              <strong>${esc(n.name)}</strong>
              <div class="muted" style="margin:0;font-size:.78rem">${esc(n.id)}</div>
            </td>
            <td>
              ${yesNo(n.isMaster)}
              <div class="muted" style="margin:.2rem 0 0;font-size:.75rem">eligible: ${n.isMasterEligible ? "yes" : "no"}</div>
            </td>
            <td><div class="role-tags">${roleTags(n.roles)}</div></td>
            <td><code>${esc(n.httpPublishAddress || "—")}</code></td>
            <td>${esc(n.version || "—")}</td>
            <td>${esc(jvm || "—")}</td>
            <td>${esc(cpu || "—")}</td>
            <td title="${esc((n.plugins || []).join(", "))}">${plugins}${more}</td>
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
    cells[2].textContent = ping.version || "—";
    cells[3].innerHTML = statusBadge(available, ping.status, ping.message);
    cells[4].textContent = available ? String(ping.nodeCount ?? "—") : "—";
    cells[5].innerHTML = actionButtons(available);
    tr.dataset.available = available ? "1" : "0";
  }

  async function probeRow(tr, id) {
    try {
      const ping = await api(`/api/v1/es/conns/${id}/ping`, { method: "POST" });
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
      const rows = await api(`/api/v1/es/conns?check=0`);
      if (gen !== loadGen) return;
      if (!rows.length) {
        connTbody.innerHTML = `<tr><td colspan="6" class="empty">No connections yet. Click "New connection".</td></tr>`;
        return;
      }
      connTbody.innerHTML = rows
        .map(
          (r) => `<tr data-id="${r.id}">
          <td><strong>${esc(r.name)}</strong></td>
          <td><code>${esc(r.addresses)}</code></td>
          <td>—</td>
          <td>${statusBadge(null)}</td>
          <td>—</td>
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
    root.querySelector("#conn-addresses").value = "";
    root.querySelector("#conn-user").value = "";
    root.querySelector("#conn-pass").value = "";
    root.querySelector("#conn-desc").value = "";
    if (id) {
      try {
        const row = await api(`/api/v1/es/conns/${id}`);
        root.querySelector("#conn-name").value = row.name || "";
        root.querySelector("#conn-addresses").value = row.addresses || "";
        root.querySelector("#conn-user").value = row.username || "";
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
    if (act === "indices") {
      ctx.navigate("es", "indices", {
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
        await api(`/api/v1/es/conns/${id}`, { method: "DELETE" });
        loadConns();
      } catch (err) {
        alert(err.message);
      }
    }
  });

  drawer.addEventListener("click", (e) => {
    if (e.target.closest("[data-drawer-close]")) closeDrawer();
  });

  function onKeydown(e) {
    if (e.key === "Escape" && drawer.classList.contains("is-open")) {
      closeDrawer();
    }
  }
  document.addEventListener("keydown", onKeydown);

  async function formPayload() {
    const id = root.querySelector("#conn-id").value;
    const password = root.querySelector("#conn-pass").value;
    return {
      id: id ? Number(id) : 0,
      name: root.querySelector("#conn-name").value.trim(),
      addresses: root.querySelector("#conn-addresses").value.trim(),
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

  root.querySelector("#btn-refresh-detail").addEventListener("click", loadDetail);
  root.querySelector("#btn-refresh-conns").addEventListener("click", loadConns);
  root.querySelector("#btn-add-conn").addEventListener("click", () => openConnDialog(null));
  root.querySelector("#conn-cancel").addEventListener("click", () => dlgConn.close());

  root.querySelector("#conn-test").addEventListener("click", async () => {
    const payload = await formPayload();
    if (!payload.addresses) {
      showConnMsg("Addresses is required", false);
      return;
    }
    const btn = root.querySelector("#conn-test");
    btn.disabled = true;
    try {
      const ping = await api(`/api/v1/es/conns/test`, { method: "POST", body: JSON.stringify(payload) });
      if (ping.available) {
        const detail = [ping.clusterName, ping.status, ping.version ? `v${ping.version}` : ""]
          .filter(Boolean)
          .join(" · ");
        showConnMsg(`Available: ${detail || ping.message || "ok"}`, true);
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
        await api(`/api/v1/es/conns/${id}`, { method: "PUT", body: JSON.stringify(payload) });
      } else {
        await api(`/api/v1/es/conns`, { method: "POST", body: JSON.stringify(payload) });
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
