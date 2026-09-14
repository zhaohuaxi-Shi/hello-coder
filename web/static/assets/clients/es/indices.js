import { esc, loadHTML } from "../../shared/util.js";

const HTML_URL = "/assets/clients/es/indices.html";

const FIELD_TYPES = [
  { value: "text", label: "text" },
  { value: "text_keyword", label: "text+keyword" },
  { value: "keyword", label: "keyword" },
  { value: "long", label: "long" },
  { value: "integer", label: "integer" },
  { value: "short", label: "short" },
  { value: "byte", label: "byte" },
  { value: "double", label: "double" },
  { value: "float", label: "float" },
  { value: "boolean", label: "boolean" },
  { value: "date", label: "date" },
  { value: "object", label: "object" },
  { value: "nested", label: "nested" },
  { value: "ip", label: "ip" },
  { value: "geo_point", label: "geo_point" },
];

const DEFAULT_INDEX_JSON = `{
  "settings": {
    "number_of_shards": 2,
    "number_of_replicas": 0
  },
  "mappings": {
    "properties": {
    }
  },
  "aliases": {
    "my_alias": {}
  }
}`;

function healthBadge(h) {
  const s = String(h || "").toLowerCase();
  if (s === "green") return `<span class="badge ok">green</span>`;
  if (s === "yellow") return `<span class="badge warn">yellow</span>`;
  if (s === "red") return `<span class="badge bad">red</span>`;
  return `<span class="badge unknown">${esc(h || "—")}</span>`;
}

function statusBadge(st) {
  const s = String(st || "").toLowerCase();
  if (s === "open") return `<span class="badge ok">open</span>`;
  if (s === "close" || s === "closed") return `<span class="badge unknown">closed</span>`;
  return `<span class="badge unknown">${esc(st || "—")}</span>`;
}

function fmtCount(n) {
  return Number(n || 0).toLocaleString();
}

function fieldTypeOptionsHTML(selected = "keyword") {
  return FIELD_TYPES.map(
    (t) =>
      `<option value="${t.value}"${t.value === selected ? " selected" : ""}>${t.label}</option>`
  ).join("");
}

function mappingForFieldType(type) {
  if (type === "text_keyword") {
    return {
      type: "text",
      fields: {
        keyword: {
          type: "keyword",
          ignore_above: 256,
        },
      },
    };
  }
  return { type };
}

export async function mount(root, ctx, params = {}) {
  const { connId, connName } = params;
  if (!connId) {
    ctx.navigate("es", "connections");
    return { unmount() {} };
  }

  root.innerHTML = await loadHTML(HTML_URL);
  const api = ctx.api;
  const tbody = root.querySelector("#index-tbody");
  const dlgStructure = root.querySelector("#dlg-structure");
  const drawer = root.querySelector("#drawer-more");
  const drawerCreate = root.querySelector("#drawer-create-index");
  const fieldRowsEl = root.querySelector("#create-index-field-rows");
  const createJsonEl = root.querySelector("#create-index-json");
  const createNameEl = root.querySelector("#create-index-name");
  const qEl = root.querySelector("#index-q");
  let q = "";
  let currentIndex = "";
  let currentOp = "";
  let cachedStructure = null;

  root.querySelector("#indices-title").textContent = `Indices · ${connName || "#" + connId}`;
  root.querySelector("#indices-sub").textContent = `Connection #${connId}`;
  root.querySelector("#create-index-sub").textContent = `Connection #${connId}`;
  const opHint = root.querySelector("#op-hint");

  function indexPath(name, suffix = "") {
    const base = `/api/v1/es/conns/${connId}/indices/${encodeURIComponent(name)}`;
    return suffix ? `${base}/${suffix}` : base;
  }

  function showOpMsg(text, isError = false) {
    const msg = root.querySelector("#op-msg");
    msg.hidden = false;
    msg.className = isError ? "msg is-error" : "msg is-ok";
    msg.textContent = text;
  }

  function hideOpMsg() {
    root.querySelector("#op-msg").hidden = true;
  }

  function showCreateMsg(text, isError = false) {
    const msg = root.querySelector("#create-index-msg");
    if (!text) {
      msg.hidden = true;
      msg.textContent = "";
      return;
    }
    msg.hidden = false;
    msg.className = isError ? "msg is-error" : "msg is-ok";
    msg.textContent = text;
  }

  function fillRebuildFields(st) {
    if (!st) return;
    root.querySelector("#op-rebuild-mappings").value = JSON.stringify(st.mappings || {}, null, 2);
    root.querySelector("#op-rebuild-settings").value = JSON.stringify(st.settings || {}, null, 2);
  }

  async function ensureStructure() {
    if (!currentIndex) return null;
    if (cachedStructure?.name === currentIndex) return cachedStructure;
    const st = await api(indexPath(currentIndex));
    cachedStructure = st;
    return st;
  }

  async function setOp(op) {
    currentOp = op || "";
    root.querySelectorAll("#op-tabs [data-op]").forEach((btn) => {
      btn.classList.toggle("is-active", !!op && btn.dataset.op === op);
    });
    root.querySelectorAll("[data-op-panel]").forEach((panel) => {
      panel.hidden = !op || panel.dataset.opPanel !== op;
    });
    if (opHint) opHint.hidden = !!op;
    hideOpMsg();
    if (op === "rebuild") {
      try {
        const st = await ensureStructure();
        fillRebuildFields(st);
      } catch (e) {
        showOpMsg(e.message, true);
      }
    }
  }

  function openDrawer() {
    closeCreateDrawer(false);
    drawer.classList.add("is-open");
    drawer.setAttribute("aria-hidden", "false");
    document.body.classList.add("drawer-open");
  }

  function closeDrawer(clearBody = true) {
    drawer.classList.remove("is-open");
    drawer.setAttribute("aria-hidden", "true");
    if (clearBody && !drawerCreate.classList.contains("is-open")) {
      document.body.classList.remove("drawer-open");
    }
    currentIndex = "";
    cachedStructure = null;
    setOp("");
  }

  function appendFieldRow(name = "", type = "keyword") {
    const row = document.createElement("div");
    row.className = "create-index-field-row";
    row.innerHTML = `
      <input type="text" class="create-field-name" placeholder="Field name" autocomplete="off" value="${esc(name)}" />
      <select class="create-field-type" aria-label="Field type">${fieldTypeOptionsHTML(type)}</select>
      <button type="button" class="btn ghost btn-remove-field" data-act="remove-field" title="Remove">✕</button>
    `;
    fieldRowsEl.appendChild(row);
  }

  function resetCreateForm() {
    createNameEl.value = "";
    createJsonEl.value = DEFAULT_INDEX_JSON;
    fieldRowsEl.innerHTML = "";
    appendFieldRow();
    showCreateMsg("");
  }

  function openCreateDrawer() {
    closeDrawer(false);
    resetCreateForm();
    drawerCreate.classList.add("is-open");
    drawerCreate.setAttribute("aria-hidden", "false");
    document.body.classList.add("drawer-open");
    createNameEl.focus();
  }

  function closeCreateDrawer(clearBody = true) {
    drawerCreate.classList.remove("is-open");
    drawerCreate.setAttribute("aria-hidden", "true");
    if (clearBody && !drawer.classList.contains("is-open")) {
      document.body.classList.remove("drawer-open");
    }
  }

  function collectFieldsFromRows() {
    const properties = {};
    const rows = Array.from(fieldRowsEl.querySelectorAll(".create-index-field-row"));
    for (const row of rows) {
      const name = (row.querySelector(".create-field-name")?.value || "").trim();
      const type = (row.querySelector(".create-field-type")?.value || "keyword").trim();
      if (!name) continue;
      properties[name] = mappingForFieldType(type);
    }
    return properties;
  }

  function parseIndexBody(raw) {
    let body;
    try {
      body = JSON.parse(raw || "{}");
    } catch {
      throw new Error("Index JSON must be valid JSON");
    }
    if (!body || typeof body !== "object" || Array.isArray(body)) {
      throw new Error("Index JSON must be a JSON object");
    }
    // Support pasting only mappings.properties.
    if (!body.mappings && body.properties) {
      return {
        settings: {},
        mappings: { properties: body.properties },
        aliases: {},
      };
    }
    return body;
  }

  function generateMappingsJSON() {
    const properties = collectFieldsFromRows();
    let body;
    try {
      body = parseIndexBody(createJsonEl.value || DEFAULT_INDEX_JSON);
    } catch {
      body = JSON.parse(DEFAULT_INDEX_JSON);
    }
    if (!body.mappings || typeof body.mappings !== "object" || Array.isArray(body.mappings)) {
      body.mappings = {};
    }
    body.mappings.properties = properties;
    createJsonEl.value = JSON.stringify(body, null, 2);
    showCreateMsg("");
  }

  async function loadIndices() {
    tbody.innerHTML = `<tr><td colspan="9" class="empty">Loading…</td></tr>`;
    const qs = new URLSearchParams();
    if (q) qs.set("q", q);
    try {
      const data = await api(`/api/v1/es/conns/${connId}/indices?${qs}`);
      const rows = data.items || [];
      if (!rows.length) {
        tbody.innerHTML = `<tr><td colspan="9" class="empty">${q ? "No matching indices" : "No indices"}</td></tr>`;
        return;
      }
      tbody.innerHTML = rows
        .map((r) => {
          const aliases = (r.aliases || []).join(", ") || "—";
          return `<tr data-index="${esc(r.name)}">
            <td><strong>${esc(r.name)}</strong></td>
            <td><code class="uuid">${esc(r.uuid || "—")}</code></td>
            <td>
              <button type="button" class="link-btn" data-act="docs" title="View documents">${fmtCount(r.docsCount)}</button>
            </td>
            <td>${esc(r.storeSize || "—")}</td>
            <td>${healthBadge(r.health)}</td>
            <td>${statusBadge(r.status)}</td>
            <td title="${esc(aliases)}">${esc(aliases)}</td>
            <td>${esc(`${r.primaryShards ?? "—"} / ${r.replicas ?? "—"}`)}</td>
            <td>
              <div class="row-actions">
                <button type="button" data-act="mapping">Mapping</button>
                <button type="button" class="danger" data-act="del">Delete</button>
                <button type="button" data-act="more">More</button>
              </div>
            </td>
          </tr>`;
        })
        .join("");
    } catch (e) {
      tbody.innerHTML = `<tr><td colspan="9" class="empty">${esc(e.message)}</td></tr>`;
    }
  }

  async function openMapping(name) {
    root.querySelector("#dlg-structure-title").textContent = `Mapping · ${name}`;
    const body = root.querySelector("#structure-body");
    body.textContent = "Loading…";
    dlgStructure.showModal();
    try {
      const st = await api(indexPath(name));
      body.textContent = JSON.stringify(st.mappings || {}, null, 2);
    } catch (e) {
      body.textContent = e.message;
    }
  }

  async function openMore(name) {
    currentIndex = name;
    cachedStructure = null;
    root.querySelector("#more-title").textContent = `Operations · ${name}`;
    root.querySelector("#more-sub").textContent = `Connection #${connId}`;
    root.querySelector("#op-alias").value = "";
    root.querySelector("#op-alias-replace").checked = false;
    root.querySelector("#op-clone-target").value = `${name}-clone`;
    root.querySelector("#op-rebuild-mappings").value = "";
    root.querySelector("#op-rebuild-settings").value = "";
    setOp("");
    openDrawer();
    try {
      const st = await ensureStructure();
      fillRebuildFields(st);
      if ((st.aliases || []).length) {
        root.querySelector("#op-alias").value = st.aliases[0];
      }
    } catch (e) {
      showOpMsg(e.message, true);
    }
  }

  tbody.addEventListener("click", async (e) => {
    const btn = e.target.closest("button[data-act]");
    if (!btn) return;
    const tr = btn.closest("tr");
    const name = tr?.dataset.index;
    if (!name) return;
    const act = btn.dataset.act;
    if (act === "docs") {
      ctx.navigate("es", "docs", { connId, connName, index: name });
    } else if (act === "mapping") {
      openMapping(name);
    } else if (act === "del") {
      if (!confirm(`Delete index "${name}"? This cannot be undone.`)) return;
      btn.disabled = true;
      try {
        await api(indexPath(name), { method: "DELETE" });
        loadIndices();
      } catch (err) {
        alert(err.message);
      } finally {
        btn.disabled = false;
      }
    } else if (act === "more") {
      openMore(name);
    }
  });

  root.querySelector("#op-tabs").addEventListener("click", async (e) => {
    const btn = e.target.closest("button[data-op]");
    if (!btn) return;
    await setOp(btn.dataset.op);
  });

  root.querySelector("#btn-op-clear").addEventListener("click", async () => {
    if (!currentIndex) return;
    if (!confirm(`Clear all documents in "${currentIndex}"?`)) return;
    hideOpMsg();
    try {
      await api(indexPath(currentIndex, "clear"), { method: "POST", body: "{}" });
      showOpMsg("Data cleared");
      loadIndices();
    } catch (e) {
      showOpMsg(e.message, true);
    }
  });

  root.querySelector("#btn-op-alias").addEventListener("click", async () => {
    if (!currentIndex) return;
    const alias = root.querySelector("#op-alias").value.trim();
    if (!alias) {
      showOpMsg("Alias is required", true);
      return;
    }
    hideOpMsg();
    try {
      await api(indexPath(currentIndex, "aliases"), {
        method: "POST",
        body: JSON.stringify({
          alias,
          replace: root.querySelector("#op-alias-replace").checked,
        }),
      });
      showOpMsg(`Alias set: ${alias}`);
      loadIndices();
    } catch (e) {
      showOpMsg(e.message, true);
    }
  });

  root.querySelector("#btn-op-clone").addEventListener("click", async () => {
    if (!currentIndex) return;
    const target = root.querySelector("#op-clone-target").value.trim();
    if (!target) {
      showOpMsg("New index name is required", true);
      return;
    }
    hideOpMsg();
    try {
      await api(indexPath(currentIndex, "clone"), {
        method: "POST",
        body: JSON.stringify({ target }),
      });
      showOpMsg(`Cloned to: ${target}`);
      loadIndices();
    } catch (e) {
      showOpMsg(e.message, true);
    }
  });

  root.querySelector("#btn-op-rebuild").addEventListener("click", async () => {
    if (!currentIndex) return;
    let mappings;
    let settings;
    try {
      mappings = JSON.parse(root.querySelector("#op-rebuild-mappings").value || "{}");
      settings = JSON.parse(root.querySelector("#op-rebuild-settings").value || "{}");
    } catch {
      showOpMsg("Mappings / Settings must be valid JSON", true);
      return;
    }
    if (!confirm(`Rebuild index "${currentIndex}"? It may be briefly unavailable.`)) return;
    hideOpMsg();
    const btn = root.querySelector("#btn-op-rebuild");
    btn.disabled = true;
    btn.textContent = "Rebuilding…";
    try {
      const result = await api(indexPath(currentIndex, "rebuild"), {
        method: "POST",
        body: JSON.stringify({ mappings, settings }),
      });
      showOpMsg(result.message || "Rebuild completed");
      loadIndices();
    } catch (e) {
      showOpMsg(e.message, true);
    } finally {
      btn.disabled = false;
      btn.textContent = "Start rebuild";
    }
  });

  root.querySelector("#btn-create-index").addEventListener("click", openCreateDrawer);
  root.querySelector("#btn-add-index-field").addEventListener("click", () => appendFieldRow());
  root.querySelector("#btn-gen-index-json").addEventListener("click", generateMappingsJSON);

  fieldRowsEl.addEventListener("click", (e) => {
    const btn = e.target.closest("[data-act=remove-field]");
    if (!btn) return;
    const row = btn.closest(".create-index-field-row");
    if (!row) return;
    if (fieldRowsEl.querySelectorAll(".create-index-field-row").length <= 1) {
      row.querySelector(".create-field-name").value = "";
      row.querySelector(".create-field-type").value = "keyword";
      return;
    }
    row.remove();
  });

  root.querySelector("#btn-confirm-create-index").addEventListener("click", async () => {
    const name = createNameEl.value.trim();
    if (!name) {
      showCreateMsg("Index name is required", true);
      createNameEl.focus();
      return;
    }
    let body;
    try {
      body = parseIndexBody(createJsonEl.value);
    } catch (e) {
      showCreateMsg(e.message, true);
      return;
    }
    const btn = root.querySelector("#btn-confirm-create-index");
    btn.disabled = true;
    showCreateMsg("");
    try {
      await api(`/api/v1/es/conns/${connId}/indices`, {
        method: "POST",
        body: JSON.stringify({
          name,
          settings: body.settings || {},
          mappings: body.mappings || {},
          aliases: body.aliases || {},
        }),
      });
      closeCreateDrawer();
      await loadIndices();
    } catch (e) {
      showCreateMsg(e.message, true);
    } finally {
      btn.disabled = false;
    }
  });

  drawer.addEventListener("click", (e) => {
    if (e.target.closest("[data-drawer-close]")) closeDrawer();
  });

  drawerCreate.addEventListener("click", (e) => {
    if (e.target.closest("[data-create-close]")) closeCreateDrawer();
  });

  function onKeydown(e) {
    if (e.key !== "Escape") return;
    if (drawerCreate.classList.contains("is-open")) {
      closeCreateDrawer();
      return;
    }
    if (drawer.classList.contains("is-open")) closeDrawer();
  }
  document.addEventListener("keydown", onKeydown);

  root.querySelector("#structure-close").addEventListener("click", () => dlgStructure.close());
  root.querySelector("#btn-back-conns").addEventListener("click", () => ctx.navigate("es", "connections"));
  root.querySelector("#btn-refresh-indices").addEventListener("click", loadIndices);
  root.querySelector("#index-filter").addEventListener("submit", (e) => {
    e.preventDefault();
    q = qEl.value.trim();
    loadIndices();
  });

  await loadIndices();

  return {
    unmount() {
      document.removeEventListener("keydown", onKeydown);
      closeDrawer();
      closeCreateDrawer();
      dlgStructure.close();
      root.innerHTML = "";
    },
  };
}
