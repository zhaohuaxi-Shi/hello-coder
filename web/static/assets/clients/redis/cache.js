import { esc, loadHTML } from "../../shared/util.js";

const HTML_URL = "/assets/clients/redis/cache.html";
const PAGE_COUNT = 20;

const ICON_TRASH = `<svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
  <path d="M5 7h14M10 7V5h4v2m-6 3v8m4-8v8m4-8v8M7 7l1 12a2 2 0 0 0 2 2h4a2 2 0 0 0 2-2l1-12"
    fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>
</svg>`;

const ICON_REFRESH = `<svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
  <path d="M20 12a8 8 0 1 1-2.2-5.5M20 4v5h-5"
    fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/>
</svg>`;

const ICON_SAVE = `<svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
  <path d="M5 5h11.5L19 7.5V19H5V5Z"
    fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"/>
  <path d="M8 5v5h8V5M8 19v-6h8v6"
    fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"/>
</svg>`;

function keyLeaf(key) {
  const s = String(key ?? "");
  const i = s.lastIndexOf(":");
  return i >= 0 ? s.slice(i + 1) || s : s;
}

function rawToText(value) {
  if (value == null) return "";
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function toHex(text) {
  const s = String(text ?? "");
  const parts = [];
  for (let i = 0; i < s.length; i++) {
    parts.push(s.charCodeAt(i).toString(16).padStart(2, "0"));
  }
  return parts.join(" ");
}

function fromHex(hex) {
  const parts = String(hex ?? "").trim().split(/\s+/).filter(Boolean);
  let out = "";
  for (const p of parts) {
    if (!/^[0-9a-fA-F]{1,2}$/.test(p)) {
      throw new Error("Invalid hex");
    }
    out += String.fromCharCode(parseInt(p, 16));
  }
  return out;
}

function formatByMode(raw, mode) {
  const text = rawToText(raw);
  if (mode === "hex") return toHex(text);
  if (mode === "json") {
    const parsed = tryParseJson(raw);
    if (parsed.ok) {
      try {
        return JSON.stringify(parsed.value, null, 2);
      } catch {
        return text;
      }
    }
    return text;
  }
  return text;
}

function tryParseJson(raw) {
  try {
    if (typeof raw === "string") {
      const t = raw.trim();
      if (!t) return { ok: false };
      return { ok: true, value: JSON.parse(t) };
    }
    if (typeof raw === "undefined") return { ok: false };
    JSON.stringify(raw);
    return { ok: true, value: raw ?? null };
  } catch {
    return { ok: false };
  }
}

function canFoldJson(value) {
  if (value === null || typeof value !== "object") return false;
  return Array.isArray(value) ? value.length > 0 : Object.keys(value).length > 0;
}

function jsonTokenHTML(value) {
  if (value === null) return `<span class="json-null">null</span>`;
  switch (typeof value) {
    case "string":
      return `<span class="json-string">${esc(JSON.stringify(value))}</span>`;
    case "number":
      return Number.isFinite(value)
        ? `<span class="json-number">${esc(String(value))}</span>`
        : `<span class="json-null">null</span>`;
    case "boolean":
      return `<span class="json-bool">${value ? "true" : "false"}</span>`;
    default:
      return `<span class="json-null">${esc(String(value))}</span>`;
  }
}

function jsonKeyHTML(key) {
  if (key === undefined) return "";
  const label = typeof key === "number" ? String(key) : JSON.stringify(String(key));
  return `<span class="json-key">${esc(label)}</span><span class="json-colon">: </span>`;
}

function jsonTreeHTML(value, { key, isLast = true } = {}) {
  const suffix = isLast ? "" : `<span class="json-punct">,</span>`;
  const prefix = jsonKeyHTML(key);
  if (value !== null && typeof value === "object") {
    const isArr = Array.isArray(value);
    const entries = isArr ? value.map((v, i) => [i, v]) : Object.entries(value);
    const open = isArr ? "[" : "{";
    const close = isArr ? "]" : "}";
    if (!entries.length) {
      return `<div class="json-line json-leaf">${prefix}<span class="json-punct">${open}${close}</span>${suffix}</div>`;
    }
    const kids = entries
      .map(([k, v], i) => jsonTreeHTML(v, { key: k, isLast: i === entries.length - 1 }))
      .join("");
    return `<div class="json-node is-open">
      <div class="json-line">
        <button type="button" class="json-toggle" data-json-toggle aria-expanded="true" title="Collapse">
          <span class="json-caret">▾</span>
        </button>
        ${prefix}<span class="json-punct json-open">${open}</span>
        <span class="json-collapsed-tail">
          <span class="json-ellipsis">…</span>
          <span class="json-punct json-collapsed-close">${close}</span>${suffix}
        </span>
      </div>
      <div class="json-children">${kids}</div>
      <div class="json-line json-close"><span class="json-punct">${close}</span>${suffix}</div>
    </div>`;
  }
  return `<div class="json-line json-leaf">${prefix}${jsonTokenHTML(value)}${suffix}</div>`;
}

function valueInnerHTML(tab) {
  const fmt = tab.format || "text";
  const display = formatByMode(tab.raw, fmt);
  if (fmt === "json") {
    const parsed = tryParseJson(tab.raw);
    if (parsed.ok && canFoldJson(parsed.value)) {
      return `<div class="json-tree" data-json-tree="${tab.id}">${jsonTreeHTML(parsed.value)}</div>
        <textarea class="cache-value code-block" data-value="${tab.id}" spellcheck="false" hidden>${esc(display)}</textarea>`;
    }
  }
  return `<textarea class="cache-value code-block" data-value="${tab.id}" spellcheck="false">${esc(display)}</textarea>`;
}

function setJsonNodeOpen(node, open) {
  if (!node) return;
  node.classList.toggle("is-open", open);
  const btn = node.querySelector(":scope > .json-line > [data-json-toggle]");
  if (!btn) return;
  btn.setAttribute("aria-expanded", open ? "true" : "false");
  btn.title = open ? "Collapse" : "Expand";
  const caret = btn.querySelector(".json-caret");
  if (caret) caret.textContent = open ? "▾" : "▸";
}

function ttlInputValue(ttl) {
  if (ttl == null) return "";
  return String(ttl);
}

export async function mount(root, ctx, params = {}) {
  const { connId, connName } = params;
  if (!connId) {
    ctx.navigate("redis", "connections");
    return { unmount() {} };
  }

  root.innerHTML = await loadHTML(HTML_URL);

  const api = ctx.api;
  const currentConn = { id: connId, name: connName || "" };
  const dbSelect = root.querySelector("#cache-db");
  const searchEl = root.querySelector("#cache-search");
  const treeEl = root.querySelector("#cache-tree");
  const moreBtn = root.querySelector("#btn-key-more");
  const listHint = root.querySelector("#cache-list-hint");
  const tabsEl = root.querySelector("#cache-tabs");
  const panelsEl = root.querySelector("#cache-panels");
  const emptyEl = root.querySelector("#cache-empty");
  const dlgAdd = root.querySelector("#dlg-key-add");
  const formAdd = root.querySelector("#form-key-add");

  root.querySelector("#cache-title").textContent = `Cache · ${currentConn.name || "#" + currentConn.id}`;
  root.querySelector("#cache-sub").textContent = `Connection #${currentConn.id}`;

  let db = 0;
  let match = "";
  let cursor = "0";
  let done = true;
  let keys = [];
  let selected = new Set();
  let selectMode = false;
  let loadGen = 0;
  let clickTimer = null;
  let searchTimer = null;
  let listingUnsupported = false;

  /** @type {{ id: string, key: string, type: string, ttl: number, raw: any, format: string }[]} */
  let tabs = [];
  let activeId = null;
  let tabSeq = 0;

  function currentDB() {
    const n = Number(dbSelect.value);
    return Number.isFinite(n) && n >= 0 ? n : 0;
  }

  async function loadDBs() {
    try {
      const data = await api(`/api/v1/redis/conns/${currentConn.id}/dbs`);
      const items = data.items || [{ db: 0, keys: 0 }];
      const mode = String(data.mode || "").toLowerCase();
      dbSelect.innerHTML = items
        .map((d) => {
          const label =
            mode === "cluster"
              ? `db${d.db} (cluster)`
              : `db${d.db}${d.keys ? ` · ${d.keys}` : ""}`;
          return `<option value="${d.db}">${esc(label)}</option>`;
        })
        .join("");
      if (mode === "cluster") {
        dbSelect.value = "0";
        dbSelect.disabled = true;
      } else {
        dbSelect.disabled = false;
        const prefer = Number(params.db);
        if (Number.isFinite(prefer) && prefer >= 0) dbSelect.value = String(prefer);
      }
      db = currentDB();
    } catch (e) {
      dbSelect.innerHTML = `<option value="0">db0</option>`;
      dbSelect.disabled = false;
    }
  }

  function renderTree() {
    treeEl.classList.toggle("is-selecting", selectMode);
    if (!keys.length) {
      const tip = listingUnsupported
        ? "Listing disabled on this Redis. Type an exact key and press Enter."
        : match
          ? "No matching keys"
          : "No keys";
      treeEl.innerHTML = `<div class="empty">${esc(tip)}</div>`;
      return;
    }
    const tree = buildTree(keys);
    sortTree(tree);

    function renderNode(node, depth) {
      let html = "";
      for (const name of node._folderOrder || []) {
        const folder = node.folders.get(name);
        html += `<div class="cache-folder is-open">
          <div class="cache-folder-row">
            <span class="cache-gutter" aria-hidden="true"></span>
            <button type="button" class="cache-folder-toggle" data-folder-toggle aria-expanded="true">
              <span class="cache-caret">▾</span>
              <span class="cache-folder-name">${esc(name)}</span>
            </button>
          </div>
          <div class="cache-folder-body" data-folder-body>${renderNode(folder, depth + 1)}</div>
        </div>`;
      }
      for (const leaf of node.leaves) {
        const checked = selected.has(leaf.key) ? " checked" : "";
        const active = tabs.some((t) => t.key === leaf.key && t.id === activeId) ? " is-active" : "";
        const checkHtml = selectMode
          ? `<label class="cache-check" title="Select">
              <input type="checkbox" data-check="${esc(leaf.key)}"${checked} />
            </label>`
          : `<span class="cache-gutter" aria-hidden="true"></span>`;
        html += `<div class="cache-leaf${active}" data-key="${esc(leaf.key)}">
          ${checkHtml}
          <button type="button" class="cache-key-btn" data-key-btn="${esc(leaf.key)}" title="${esc(leaf.key)}">
            <span class="cache-key-label">${esc(leaf.label)}</span>
          </button>
        </div>`;
      }
      return html;
    }

    treeEl.innerHTML = renderNode(tree, 0);
  }

  function panelHTML(t) {
    const fmt = t.format || "text";
    const parsed = fmt === "json" ? tryParseJson(t.raw) : { ok: false };
    const showFold = parsed.ok && canFoldJson(parsed.value);
    return `<div class="cache-panel" data-panel="${t.id}"${t.id === activeId ? "" : " hidden"}>
      <div class="cache-panel-head">
        <div class="cache-full-key" title="${esc(t.key)}">${esc(t.key)}</div>
        <label class="cache-ttl-field" title="过期时间（秒）。-1 表示永不过期；回车或失焦保存">
          <span>TTL</span>
          <input type="number" min="-1" step="1" data-ttl-input="${t.id}"
            value="${esc(ttlInputValue(t.ttl))}" />
          <span>s</span>
        </label>
        <button type="button" class="icon-btn is-danger" data-key-delete="${t.id}" title="删除 key" aria-label="删除 key">${ICON_TRASH}</button>
      </div>
      <div class="cache-value-toolbar">
        <div class="cache-value-toolbar-left">
          <select class="cache-format-select" data-format="${t.id}" aria-label="Value format">
            <option value="text"${fmt === "text" ? " selected" : ""}>text</option>
            <option value="hex"${fmt === "hex" ? " selected" : ""}>hex</option>
            <option value="json"${fmt === "json" ? " selected" : ""}>json</option>
          </select>
          <div class="json-fold-btns" data-json-fold="${t.id}"${showFold ? "" : " hidden"}>
            <button type="button" class="cache-fold-btn" data-json-expand="${t.id}">Expand all</button>
            <button type="button" class="cache-fold-btn" data-json-collapse="${t.id}">Collapse all</button>
          </div>
        </div>
        <div class="cache-value-actions">
          <button type="button" class="icon-btn" data-value-refresh="${t.id}" title="刷新内容" aria-label="刷新内容">${ICON_REFRESH}</button>
          <button type="button" class="icon-btn" data-value-save="${t.id}" title="保存" aria-label="保存">${ICON_SAVE}</button>
        </div>
      </div>
      <div class="cache-value-wrap">
        ${valueInnerHTML(t)}
      </div>
    </div>`;
  }

  function syncRightChrome() {
    if (!tabs.length) {
      tabsEl.hidden = true;
      panelsEl.hidden = true;
      emptyEl.hidden = false;
      tabsEl.innerHTML = "";
      panelsEl.innerHTML = "";
      return;
    }
    emptyEl.hidden = true;
    tabsEl.hidden = false;
    panelsEl.hidden = false;
    if (!activeId || !tabs.some((t) => t.id === activeId)) {
      activeId = tabs[0].id;
    }
    tabsEl.innerHTML = tabs
      .map(
        (t) => `<div class="cache-tab${t.id === activeId ? " is-active" : ""}" data-tab="${t.id}" title="${esc(t.key)}">
          <button type="button" class="cache-tab-label" data-tab-activate="${t.id}">${esc(keyLeaf(t.key))}</button>
          <button type="button" class="cache-tab-close" data-tab-close="${t.id}" title="Close">×</button>
        </div>`
      )
      .join("");
    panelsEl.innerHTML = tabs.map((t) => panelHTML(t)).join("");
  }

  function updatePanelValue(tab) {
    const panel = panelsEl.querySelector(`[data-panel="${tab.id}"]`);
    if (!panel) return;
    const wrap = panel.querySelector(".cache-value-wrap");
    const fold = panel.querySelector("[data-json-fold]");
    const parsed = (tab.format || "text") === "json" ? tryParseJson(tab.raw) : { ok: false };
    if (fold) fold.hidden = !(parsed.ok && canFoldJson(parsed.value));
    if (wrap) wrap.innerHTML = valueInnerHTML(tab);
  }

  async function fetchEntry(key) {
    return api(`/api/v1/redis/conns/${currentConn.id}/keys/get`, {
      method: "POST",
      body: JSON.stringify({ db: currentDB(), key }),
    });
  }

  async function openKey(key, { newTab }) {
    let entry;
    try {
      entry = await fetchEntry(key);
    } catch (e) {
      alert(e.message);
      return;
    }
    if (entry.type === "none") {
      alert(`Key not found: ${key}`);
      return;
    }
    const payload = {
      key: entry.key || key,
      type: entry.type || "—",
      ttl: entry.ttl,
      raw: entry.value,
      format: "text",
    };

    if (!keys.includes(payload.key)) {
      keys = [payload.key, ...keys];
    }

    if (newTab) {
      const existing = tabs.find((t) => t.key === payload.key);
      if (existing) {
        existing.type = payload.type;
        existing.ttl = payload.ttl;
        existing.raw = payload.raw;
        activeId = existing.id;
      } else {
        const id = `t${++tabSeq}`;
        tabs.push({ id, ...payload });
        activeId = id;
      }
    } else {
      if (!tabs.length) {
        const id = `t${++tabSeq}`;
        tabs.push({ id, ...payload });
        activeId = id;
      } else {
        const cur = tabs.find((t) => t.id === activeId) || tabs[0];
        cur.key = payload.key;
        cur.type = payload.type;
        cur.ttl = payload.ttl;
        cur.raw = payload.raw;
        cur.format = cur.format || "text";
        activeId = cur.id;
      }
    }
    syncRightChrome();
    renderTree();
  }

  async function refreshTab(id) {
    const tab = tabs.find((t) => t.id === id);
    if (!tab) return;
    try {
      const entry = await fetchEntry(tab.key);
      if (entry.type === "none") {
        alert(`Key not found: ${tab.key}`);
        return;
      }
      tab.type = entry.type || "—";
      tab.ttl = entry.ttl;
      tab.raw = entry.value;
      syncRightChrome();
    } catch (e) {
      alert(e.message);
    }
  }

  async function saveTTL(id, inputEl) {
    const tab = tabs.find((t) => t.id === id);
    if (!tab || !inputEl) return;
    const raw = inputEl.value.trim();
    if (raw === "") {
      inputEl.value = ttlInputValue(tab.ttl);
      return;
    }
    const ttl = Number(raw);
    if (!Number.isFinite(ttl) || !Number.isInteger(ttl) || ttl < -1) {
      alert("TTL 须为整数秒，-1 表示永不过期");
      inputEl.value = ttlInputValue(tab.ttl);
      return;
    }
    // Redis 约定：-1 永不过期；后端 <=0 走 PERSIST
    const displayTtl = ttl <= 0 ? -1 : ttl;
    if (displayTtl === tab.ttl) {
      inputEl.value = ttlInputValue(tab.ttl);
      return;
    }
    try {
      await api(`/api/v1/redis/conns/${currentConn.id}/keys/expire`, {
        method: "POST",
        body: JSON.stringify({ db: currentDB(), key: tab.key, ttl: ttl <= 0 ? 0 : ttl }),
      });
      tab.ttl = displayTtl;
      inputEl.value = ttlInputValue(tab.ttl);
    } catch (e) {
      alert(e.message);
      inputEl.value = ttlInputValue(tab.ttl);
    }
  }

  function valueForSave(tab, text) {
    let payload = text;
    if ((tab.format || "text") === "hex") {
      payload = fromHex(text);
    }
    const typ = String(tab.type || "string").toLowerCase();
    if (typ === "string") return payload;
    if (typ === "hash" || typ === "list" || typ === "set" || typ === "zset") {
      try {
        JSON.parse(payload);
      } catch {
        throw new Error(`${typ} value must be valid JSON`);
      }
      return payload;
    }
    throw new Error(`unsupported type for save: ${typ}`);
  }

  async function saveTabValue(id) {
    const tab = tabs.find((t) => t.id === id);
    if (!tab) return;
    const area = panelsEl.querySelector(`[data-value="${id}"]`);
    if (!area) return;
    let value;
    try {
      value = valueForSave(tab, area.value);
    } catch (e) {
      alert(e.message);
      return;
    }
    const ttlInput = panelsEl.querySelector(`[data-ttl-input="${id}"]`);
    let ttl = tab.ttl;
    if (ttlInput) {
      const n = Number(ttlInput.value.trim());
      if (!Number.isFinite(n) || !Number.isInteger(n) || n < -1) {
        alert("TTL 须为整数秒，-1 表示永不过期");
        return;
      }
      ttl = n;
    }
    const btn = panelsEl.querySelector(`[data-value-save="${id}"]`);
    if (btn) btn.disabled = true;
    try {
      await api(`/api/v1/redis/conns/${currentConn.id}/keys`, {
        method: "POST",
        body: JSON.stringify({
          db: currentDB(),
          key: tab.key,
          type: tab.type,
          value,
          ttl: ttl <= 0 ? 0 : ttl,
        }),
      });
      tab.ttl = ttl <= 0 ? -1 : ttl;
      if (ttlInput) ttlInput.value = ttlInputValue(tab.ttl);
      await refreshTab(id);
    } catch (e) {
      alert(e.message);
    } finally {
      if (btn) btn.disabled = false;
    }
  }

  async function removeKeys(list) {
    if (!list.length) return;
    await api(`/api/v1/redis/conns/${currentConn.id}/keys/delete`, {
      method: "POST",
      body: JSON.stringify({ db: currentDB(), keys: list }),
    });
    const gone = new Set(list);
    keys = keys.filter((k) => !gone.has(k));
    for (const k of gone) selected.delete(k);
    tabs = tabs.filter((t) => !gone.has(t.key));
    if (!tabs.some((t) => t.id === activeId)) activeId = tabs[0]?.id || null;
    syncRightChrome();
    renderTree();
  }

  async function deleteTabKey(id) {
    const tab = tabs.find((t) => t.id === id);
    if (!tab) return;
    if (!confirm(`Delete key "${tab.key}"?`)) return;
    try {
      await removeKeys([tab.key]);
    } catch (e) {
      alert(e.message);
    }
  }

  async function openExactFromSearch() {
    const key = searchEl.value.trim();
    if (!key) {
      alert("Enter an exact key name");
      return;
    }
    await openKey(key, { newTab: false });
  }

  async function scanPage({ reset }) {
    const gen = ++loadGen;
    if (reset) {
      cursor = "0";
      keys = [];
      selected.clear();
      treeEl.innerHTML = `<div class="empty">Loading…</div>`;
    }
    moreBtn.disabled = true;
    try {
      const qs = new URLSearchParams({
        db: String(currentDB()),
        cursor: String(cursor || "0"),
        count: String(PAGE_COUNT),
      });
      if (match) qs.set("match", match);
      const data = await api(`/api/v1/redis/conns/${currentConn.id}/keys/scan?${qs}`);
      if (gen !== loadGen) return;
      listingUnsupported = !!data.listingUnsupported;
      if (listHint) {
        if (data.message) {
          listHint.hidden = false;
          listHint.textContent = data.message;
        } else {
          listHint.hidden = true;
          listHint.textContent = "";
        }
      }
      const batch = data.keys || [];
      const seen = new Set(keys);
      for (const k of batch) {
        if (!seen.has(k)) {
          seen.add(k);
          keys.push(k);
        }
      }
      cursor = String(data.cursorToken ?? data.cursor ?? "0");
      done = !!data.done || cursor === "0" || listingUnsupported;
      moreBtn.disabled = done;
      renderTree();
    } catch (e) {
      if (gen !== loadGen) return;
      treeEl.innerHTML = `<div class="empty">${esc(e.message)}</div>`;
      moreBtn.disabled = true;
      if (listHint) {
        listHint.hidden = false;
        listHint.textContent = e.message;
      }
    }
  }

  root.querySelector("#btn-back-conns").addEventListener("click", () => {
    ctx.navigate("redis", "connections");
  });
  root.querySelector("#btn-refresh-cache").addEventListener("click", () => scanPage({ reset: true }));
  moreBtn.addEventListener("click", () => scanPage({ reset: false }));

  dbSelect.addEventListener("change", () => {
    db = currentDB();
    scanPage({ reset: true });
  });

  searchEl.addEventListener("keydown", (e) => {
    if (e.key === "Enter") {
      e.preventDefault();
      clearTimeout(searchTimer);
      match = searchEl.value.trim();
      if (listingUnsupported) {
        openExactFromSearch();
      } else {
        scanPage({ reset: true });
      }
    }
  });

  treeEl.addEventListener("click", (e) => {
    const toggle = e.target.closest("[data-folder-toggle]");
    if (toggle) {
      const folder = toggle.closest(".cache-folder");
      const body = folder?.querySelector("[data-folder-body]");
      if (!body) return;
      const open = toggle.getAttribute("aria-expanded") !== "false";
      toggle.setAttribute("aria-expanded", open ? "false" : "true");
      body.hidden = open;
      folder.classList.toggle("is-open", !open);
      toggle.querySelector(".cache-caret").textContent = open ? "▸" : "▾";
      return;
    }

    const check = e.target.closest("input[data-check]");
    if (check) {
      const key = check.getAttribute("data-check");
      if (check.checked) selected.add(key);
      else selected.delete(key);
      return;
    }

    const btn = e.target.closest("[data-key-btn]");
    if (!btn) return;
    const key = btn.getAttribute("data-key-btn");
    clearTimeout(clickTimer);
    clickTimer = setTimeout(() => openKey(key, { newTab: false }), 240);
  });

  treeEl.addEventListener("dblclick", (e) => {
    const btn = e.target.closest("[data-key-btn]");
    if (!btn) return;
    e.preventDefault();
    clearTimeout(clickTimer);
    const key = btn.getAttribute("data-key-btn");
    openKey(key, { newTab: true });
  });

  tabsEl.addEventListener("click", (e) => {
    const close = e.target.closest("[data-tab-close]");
    if (close) {
      const id = close.getAttribute("data-tab-close");
      tabs = tabs.filter((t) => t.id !== id);
      if (activeId === id) activeId = tabs[0]?.id || null;
      syncRightChrome();
      renderTree();
      return;
    }
    const act = e.target.closest("[data-tab-activate]");
    if (act) {
      activeId = act.getAttribute("data-tab-activate");
      syncRightChrome();
      renderTree();
    }
  });

  panelsEl.addEventListener("change", (e) => {
    const fmt = e.target.closest("[data-format]");
    if (!fmt) return;
    const id = fmt.getAttribute("data-format");
    const tab = tabs.find((t) => t.id === id);
    if (!tab) return;
    tab.format = fmt.value;
    updatePanelValue(tab);
  });

  panelsEl.addEventListener("click", (e) => {
    const jsonLine = e.target.closest(".json-line");
    if (jsonLine && !jsonLine.classList.contains("json-close") && jsonLine.parentElement?.classList.contains("json-node")) {
      const node = jsonLine.parentElement;
      setJsonNodeOpen(node, !node.classList.contains("is-open"));
      return;
    }
    const expandBtn = e.target.closest("[data-json-expand]");
    if (expandBtn) {
      const tree = panelsEl.querySelector(`[data-json-tree="${expandBtn.getAttribute("data-json-expand")}"]`);
      tree?.querySelectorAll(".json-node").forEach((node) => setJsonNodeOpen(node, true));
      return;
    }
    const collapseBtn = e.target.closest("[data-json-collapse]");
    if (collapseBtn) {
      const tree = panelsEl.querySelector(`[data-json-tree="${collapseBtn.getAttribute("data-json-collapse")}"]`);
      tree?.querySelectorAll(".json-node").forEach((node) => setJsonNodeOpen(node, false));
      return;
    }
    const delBtn = e.target.closest("[data-key-delete]");
    if (delBtn) {
      deleteTabKey(delBtn.getAttribute("data-key-delete"));
      return;
    }
    const saveBtn = e.target.closest("[data-value-save]");
    if (saveBtn) {
      saveTabValue(saveBtn.getAttribute("data-value-save"));
      return;
    }
    const refreshBtn = e.target.closest("[data-value-refresh]");
    if (refreshBtn) {
      refreshTab(refreshBtn.getAttribute("data-value-refresh"));
    }
  });

  panelsEl.addEventListener("keydown", (e) => {
    const input = e.target.closest("[data-ttl-input]");
    if (!input || e.key !== "Enter") return;
    e.preventDefault();
    input.blur();
  });

  panelsEl.addEventListener("focusout", (e) => {
    const input = e.target.closest("[data-ttl-input]");
    if (!input) return;
    const id = input.getAttribute("data-ttl-input");
    saveTTL(id, input);
  });

  root.querySelector("#btn-key-add").addEventListener("click", () => {
    root.querySelector("#add-msg").hidden = true;
    root.querySelector("#add-key").value = "";
    root.querySelector("#add-type").value = "string";
    root.querySelector("#add-value").value = "";
    root.querySelector("#add-ttl").value = "-1";
    dlgAdd.showModal();
  });
  root.querySelector("#add-cancel").addEventListener("click", () => dlgAdd.close());

  formAdd.addEventListener("submit", async (e) => {
    e.preventDefault();
    const msg = root.querySelector("#add-msg");
    const key = root.querySelector("#add-key").value.trim();
    const type = root.querySelector("#add-type").value;
    const value = root.querySelector("#add-value").value;
    let ttl = Number(root.querySelector("#add-ttl").value);
    if (!Number.isFinite(ttl) || !Number.isInteger(ttl) || ttl < -1) {
      msg.hidden = false;
      msg.className = "msg is-error";
      msg.textContent = "TTL 须为整数秒，-1 表示永不过期";
      return;
    }
    // 后端：<=0 表示不设置过期
    if (ttl < 0) ttl = 0;
    try {
      await api(`/api/v1/redis/conns/${currentConn.id}/keys`, {
        method: "POST",
        body: JSON.stringify({ db: currentDB(), key, type, value, ttl }),
      });
      dlgAdd.close();
      await scanPage({ reset: true });
      await openKey(key, { newTab: false });
    } catch (err) {
      msg.hidden = false;
      msg.className = "msg is-error";
      msg.textContent = err.message;
    }
  });

  root.querySelector("#cache-select-mode").addEventListener("change", (e) => {
    selectMode = !!e.target.checked;
    if (!selectMode) selected.clear();
    renderTree();
  });

  root.querySelector("#btn-key-del").addEventListener("click", async () => {
    if (!selectMode) {
      alert("先勾选 Delete 旁的复选框，再选择要删除的 key。");
      return;
    }
    if (!selected.size) {
      alert("Select one or more keys first.");
      return;
    }
    if (!confirm(`Delete ${selected.size} key(s)?`)) return;
    const list = [...selected];
    try {
      await removeKeys(list);
    } catch (err) {
      alert(err.message);
    }
  });

  syncRightChrome();
  // Scan keys immediately; DB dropdown loads in parallel (don't block first paint).
  await Promise.all([loadDBs(), scanPage({ reset: true })]);

  return {
    unmount() {
      loadGen++;
      clearTimeout(clickTimer);
      clearTimeout(searchTimer);
      dlgAdd.close();
      root.innerHTML = "";
    },
  };
}

/** Build a folder/leaf tree from flat keys, split by `:`. */
function buildTree(keys) {
  const root = { folders: new Map(), leaves: [] };
  for (const fullKey of keys) {
    const parts = String(fullKey).split(":");
    let node = root;
    for (let i = 0; i < parts.length; i++) {
      const part = parts[i];
      const isLast = i === parts.length - 1;
      if (isLast) {
        node.leaves.push({ label: part, key: fullKey });
      } else {
        if (!node.folders.has(part)) {
          node.folders.set(part, { name: part, folders: new Map(), leaves: [], open: true });
        }
        node = node.folders.get(part);
      }
    }
  }
  return root;
}

function sortTree(node) {
  const folderNames = [...node.folders.keys()].sort((a, b) => a.localeCompare(b));
  node._folderOrder = folderNames;
  for (const name of folderNames) sortTree(node.folders.get(name));
  node.leaves.sort((a, b) => a.label.localeCompare(b.label) || a.key.localeCompare(b.key));
}
