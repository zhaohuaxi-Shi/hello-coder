import { esc, loadHTML } from "../../shared/util.js";

const HTML_URL = "/assets/clients/es/docs.html";
const DEFAULT_CONDITIONS = 2;
const DEFAULT_DSL = `{
  "query": {
    "match_all": {}
  }
}`;

const OPS = [
  { value: "*", label: "*" },
  { value: "=", label: "=" },
  { value: "!=", label: "!=" },
  { value: ">", label: ">" },
  { value: ">=", label: ">=" },
  { value: "<", label: "<" },
  { value: "<=", label: "<=" },
];

const OBJECT_TYPES = new Set([
  "object",
  "nested",
  "flattened",
  "geo_point",
  "geo_shape",
  "completion",
  "percolator",
  "rank_feature",
  "rank_features",
  "dense_vector",
  "sparse_vector",
  "join",
]);

const NUMBER_TYPES = new Set([
  "long",
  "integer",
  "short",
  "byte",
  "double",
  "float",
  "half_float",
  "scaled_float",
  "token_count",
]);

function clip(s, n = 180) {
  const t = String(s ?? "");
  if (t.length <= n) return t;
  return `${t.slice(0, n)}…`;
}

function sourcePreview(src) {
  try {
    return JSON.stringify(src ?? {});
  } catch {
    return String(src ?? "");
  }
}

function setPath(obj, path, value) {
  const parts = String(path).split(".").filter(Boolean);
  if (!parts.length) return;
  let cur = obj;
  for (let i = 0; i < parts.length - 1; i++) {
    const key = parts[i];
    if (!cur[key] || typeof cur[key] !== "object" || Array.isArray(cur[key])) {
      cur[key] = {};
    }
    cur = cur[key];
  }
  cur[parts[parts.length - 1]] = value;
}

function defaultValueForType(type) {
  const t = String(type || "").toLowerCase();
  if (t === "boolean") return false;
  if (NUMBER_TYPES.has(t)) return 0;
  if (OBJECT_TYPES.has(t)) return {};
  return "";
}

function buildDocTemplate(fieldInfos) {
  const source = {};
  for (const f of fieldInfos) {
    if (!f?.name) continue;
    setPath(source, f.name, defaultValueForType(f.type));
  }
  return source;
}

function topLevelColumns(fieldInfos, docs) {
  const names = new Set();
  for (const f of fieldInfos || []) {
    const top = String(f.name || "").split(".")[0];
    if (top) names.add(top);
  }
  for (const d of docs || []) {
    const src = d?.source;
    if (!src || typeof src !== "object" || Array.isArray(src)) continue;
    for (const key of Object.keys(src)) names.add(key);
  }
  return Array.from(names).sort((a, b) => a.localeCompare(b));
}

function formatCellValue(value) {
  if (value == null) return { text: "", isJson: false };
  if (typeof value === "object") {
    try {
      return { text: JSON.stringify(value), isJson: true };
    } catch {
      return { text: String(value), isJson: true };
    }
  }
  return { text: String(value), isJson: false };
}

function parseFieldValue(raw) {
  const text = String(raw ?? "").trim();
  if (text === "") return "";
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function editableFields(fields) {
  return (fields || []).filter((f) => f && f !== "_id");
}

export async function mount(root, ctx, params = {}) {
  const { connId, connName, index } = params;
  if (!connId || !index) {
    ctx.navigate("es", "indices", { connId, connName });
    return { unmount() {} };
  }

  root.innerHTML = await loadHTML(HTML_URL);
  const api = ctx.api;
  const conditionsEl = root.querySelector("#docs-conditions");
  const theadRow = root.querySelector("#docs-thead-row");
  const tbody = root.querySelector("#docs-tbody");
  const statusEl = root.querySelector("#docs-status");
  const pageSizeEl = root.querySelector("#docs-page-size");
  const pagerEl = root.querySelector("#docs-pager");
  const pageInfoEl = root.querySelector("#docs-page-info");
  const prevBtn = root.querySelector("#docs-prev");
  const nextBtn = root.querySelector("#docs-next");
  const drawerDsl = root.querySelector("#drawer-dsl");
  const drawerCreate = root.querySelector("#drawer-create-doc");
  const drawerEdit = root.querySelector("#drawer-edit-doc");
  const drawerBatch = root.querySelector("#drawer-batch-edit");
  const dslInput = root.querySelector("#dsl-input");
  const dslMsg = root.querySelector("#dsl-msg");
  const createJsonEl = root.querySelector("#create-doc-json");
  const createIdEl = root.querySelector("#create-doc-id");
  const createMsg = root.querySelector("#create-doc-msg");
  const editJsonEl = root.querySelector("#edit-doc-json");
  const editIdEl = root.querySelector("#edit-doc-id");
  const editMsg = root.querySelector("#edit-doc-msg");
  const editSub = root.querySelector("#edit-doc-sub");
  const batchRowsEl = root.querySelector("#batch-edit-rows");
  const batchMsg = root.querySelector("#batch-edit-msg");
  const batchSub = root.querySelector("#batch-edit-sub");

  let fields = ["_id"];
  let fieldInfos = [];
  let columns = [];
  let loadedDocs = [];
  let selectedIds = new Set();
  let editingDocId = "";
  let page = 1;
  let pageSize = Number(pageSizeEl.value) || 20;
  let total = 0;
  let searchMode = "filters"; // filters | dsl
  let activeDsl = null;

  root.querySelector("#docs-title").textContent = `Docs · ${index}`;
  root.querySelector("#docs-sub").textContent = `${connName || "Connection"} · #${connId}`;
  root.querySelector("#create-doc-sub").textContent = `Index · ${index}`;
  dslInput.value = DEFAULT_DSL;
  createJsonEl.value = "{}";

  function setMsg(el, text, isError) {
    if (!text) {
      el.hidden = true;
      el.textContent = "";
      return;
    }
    el.hidden = false;
    el.className = `msg ${isError ? "is-error" : "is-ok"}`;
    el.textContent = text;
  }

  function setStatus(text, isError) {
    setMsg(statusEl, text, isError);
  }

  function setDslMsg(text, isError) {
    setMsg(dslMsg, text, isError);
  }

  function setCreateMsg(text, isError) {
    setMsg(createMsg, text, isError);
  }

  function setEditMsg(text, isError) {
    setMsg(editMsg, text, isError);
  }

  function setBatchMsg(text, isError) {
    setMsg(batchMsg, text, isError);
  }

  function writableFieldOptionsHTML(selected = "") {
    const list = editableFields(fields);
    if (!list.length) {
      return `<option value="">No fields</option>`;
    }
    const pref = selected && list.includes(selected) ? selected : list[0];
    return list
      .map((f) => `<option value="${esc(f)}"${f === pref ? " selected" : ""}>${esc(f)}</option>`)
      .join("");
  }

  function fieldOptionsHTML(selected = "") {
    return fields
      .map((f) => `<option value="${esc(f)}"${f === selected ? " selected" : ""}>${esc(f)}</option>`)
      .join("");
  }

  function sortFieldOptionsHTML(selected = "_doc") {
    const sortable = fields.filter((f) => f !== "_id");
    const opts = [`<option value="_doc"${selected === "_doc" ? " selected" : ""}>Default</option>`];
    for (const f of sortable) {
      opts.push(`<option value="${esc(f)}"${f === selected ? " selected" : ""}>${esc(f)}</option>`);
    }
    return opts.join("");
  }

  function opOptionsHTML(selected = "*") {
    return OPS.map(
      (op) =>
        `<option value="${esc(op.value)}"${op.value === selected ? " selected" : ""}>${esc(op.label)}</option>`
    ).join("");
  }

  function getSelectedIds() {
    return Array.from(selectedIds);
  }

  function syncSelectAll() {
    const all = root.querySelector("#docs-check-all");
    if (!all) return;
    const boxes = Array.from(tbody.querySelectorAll(".doc-check"));
    if (!boxes.length) {
      all.checked = false;
      all.indeterminate = false;
      return;
    }
    const checked = boxes.filter((el) => el.checked).length;
    all.checked = checked === boxes.length;
    all.indeterminate = checked > 0 && checked < boxes.length;
  }

  function renderFilterBar() {
    const prefField = fields[0] || "_id";
    const conditions = Array.from({ length: DEFAULT_CONDITIONS }, (_, i) => {
      return `<label class="field docs-condition" data-idx="${i}">
        <span>Condition</span>
        <div class="docs-condition-row">
          <select class="cond-field" aria-label="Field">${fieldOptionsHTML(prefField)}</select>
          <select class="cond-op" aria-label="Operator">${opOptionsHTML("*")}</select>
          <input class="cond-value" type="text" placeholder="Value" autocomplete="off" aria-label="Value" />
        </div>
      </label>`;
    }).join("");

    const sort = `<label class="field docs-sort">
      <span>Sort</span>
      <div class="docs-sort-row">
        <select id="docs-sort-field" aria-label="Sort field">${sortFieldOptionsHTML("_doc")}</select>
        <select id="docs-sort-order" aria-label="Sort order">
          <option value="asc" selected>Asc</option>
          <option value="desc">Desc</option>
        </select>
      </div>
    </label>`;

    conditionsEl.innerHTML = conditions + sort;
  }

  function resetCreateJson() {
    const template = buildDocTemplate(fieldInfos);
    createJsonEl.value = JSON.stringify(template, null, 2);
  }

  function renderTableHeader(cols) {
    columns = cols;
    theadRow.innerHTML =
      `<th class="col-check"><input type="checkbox" id="docs-check-all" aria-label="Select all" /></th>` +
      `<th class="col-id">_id</th>` +
      cols.map((name) => `<th class="col-field" title="${esc(name)}">${esc(name)}</th>`).join("");
  }

  function emptyRow(text) {
    const span = Math.max(1, columns.length + 2);
    return `<tr><td colspan="${span}" class="empty">${esc(text)}</td></tr>`;
  }

  function renderDocsTable(docs) {
    const cols = topLevelColumns(fieldInfos, docs);
    renderTableHeader(cols);
    if (!docs.length) {
      selectedIds.clear();
      tbody.innerHTML = emptyRow("No documents");
      syncSelectAll();
      return;
    }
    const pageIds = new Set(docs.map((d) => String(d.id)));
    selectedIds = new Set(Array.from(selectedIds).filter((id) => pageIds.has(id)));
    tbody.innerHTML = docs
      .map((d, i) => {
        const id = String(d.id);
        const checked = selectedIds.has(id) ? " checked" : "";
        const src = d.source && typeof d.source === "object" ? d.source : {};
        const cells = cols
          .map((name) => {
            const { text, isJson } = formatCellValue(src[name]);
            const shown = clip(text, 160);
            const cls = `cell-value${isJson ? " cell-json" : ""}`;
            return `<td class="col-field">
              <span class="${cls}" title="${esc(text)}">${esc(shown) || "—"}</span>
            </td>`;
          })
          .join("");
        return `<tr data-idx="${i}" data-id="${esc(id)}">
          <td class="col-check">
            <input type="checkbox" class="doc-check" data-id="${esc(id)}" aria-label="Select ${esc(id)}"${checked} />
          </td>
          <td class="col-id">
            <button type="button" class="msg-link" data-act="open-doc" title="${esc(id)}"><code>${esc(id)}</code></button>
          </td>
          ${cells}
        </tr>`;
      })
      .join("");
    syncSelectAll();
  }

  function clearFilterValues() {
    renderFilterBar();
  }

  function collectFilters() {
    return Array.from(conditionsEl.querySelectorAll(".docs-condition"))
      .map((row) => ({
        field: row.querySelector(".cond-field")?.value || "",
        op: row.querySelector(".cond-op")?.value || "*",
        value: (row.querySelector(".cond-value")?.value || "").trim(),
      }))
      .filter((f) => f.value !== "");
  }

  function collectSort() {
    return {
      sortField: root.querySelector("#docs-sort-field")?.value || "",
      sortOrder: root.querySelector("#docs-sort-order")?.value || "asc",
    };
  }

  function parseDslInput() {
    const raw = (dslInput.value || "").trim();
    if (!raw) throw new Error("DSL is required");
    return JSON.parse(raw);
  }

  function parseDocJson(raw, label = "Document JSON") {
    const text = (raw || "").trim();
    if (!text) throw new Error(`${label} is required`);
    const parsed = JSON.parse(text);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error(`${label} must be an object`);
    }
    return parsed;
  }

  function parseCreateJson() {
    return parseDocJson(createJsonEl.value);
  }

  function parseEditJson() {
    return parseDocJson(editJsonEl.value);
  }

  function formatDsl() {
    try {
      const parsed = parseDslInput();
      dslInput.value = JSON.stringify(parsed, null, 2);
      setDslMsg("");
    } catch (e) {
      setDslMsg(e.message || "Invalid JSON", true);
    }
  }

  function formatCreateJson() {
    try {
      const parsed = parseCreateJson();
      createJsonEl.value = JSON.stringify(parsed, null, 2);
      setCreateMsg("");
    } catch (e) {
      setCreateMsg(e.message || "Invalid JSON", true);
    }
  }

  function formatEditJson() {
    try {
      const parsed = parseEditJson();
      editJsonEl.value = JSON.stringify(parsed, null, 2);
      setEditMsg("");
    } catch (e) {
      setEditMsg(e.message || "Invalid JSON", true);
    }
  }

  function openPanel(panel) {
    closeAllDrawers(false);
    panel.classList.add("is-open");
    panel.setAttribute("aria-hidden", "false");
    document.body.classList.add("drawer-open");
  }

  function closePanel(panel) {
    panel.classList.remove("is-open");
    panel.setAttribute("aria-hidden", "true");
  }

  function closeAllDrawers(clearBody = true) {
    closePanel(drawerDsl);
    closePanel(drawerCreate);
    closePanel(drawerEdit);
    closePanel(drawerBatch);
    if (clearBody) document.body.classList.remove("drawer-open");
  }

  function openDslDrawer() {
    clearFilterValues();
    searchMode = "filters";
    activeDsl = null;
    setDslMsg("");
    openPanel(drawerDsl);
    dslInput.focus();
  }

  function openCreateDrawer() {
    createIdEl.value = "";
    resetCreateJson();
    setCreateMsg("");
    openPanel(drawerCreate);
    createJsonEl.focus();
  }

  function openEditDrawer(doc) {
    editingDocId = String(doc.id || "");
    editIdEl.textContent = editingDocId;
    editSub.textContent = `Index · ${doc.index || index}`;
    try {
      editJsonEl.value = JSON.stringify(doc.source ?? {}, null, 2);
    } catch {
      editJsonEl.value = sourcePreview(doc.source);
    }
    setEditMsg("");
    openPanel(drawerEdit);
    editJsonEl.focus();
  }

  function appendBatchEditRow(selected = "", value = "") {
    const row = document.createElement("div");
    row.className = "batch-edit-row";
    row.innerHTML = `
      <label class="field">
        <span>Field</span>
        <select class="batch-field" aria-label="Field">${writableFieldOptionsHTML(selected)}</select>
      </label>
      <label class="field">
        <span>Value</span>
        <input class="batch-value" type="text" placeholder="Value" autocomplete="off" aria-label="Value" value="${esc(value)}" />
      </label>
      <button type="button" class="btn ghost btn-icon-add" data-act="add-batch-row" title="Add field">+</button>
    `;
    batchRowsEl.appendChild(row);
  }

  function resetBatchEditRows() {
    batchRowsEl.innerHTML = "";
    appendBatchEditRow();
  }

  function openBatchEditDrawer() {
    const ids = getSelectedIds();
    if (!ids.length) {
      setStatus("Select documents first", true);
      return;
    }
    if (!editableFields(fields).length) {
      setStatus("No editable fields", true);
      return;
    }
    batchSub.textContent = `${ids.length} document(s) · ${index}`;
    resetBatchEditRows();
    setBatchMsg("");
    openPanel(drawerBatch);
  }

  function collectBatchDoc() {
    const rows = Array.from(batchRowsEl.querySelectorAll(".batch-edit-row"));
    if (!rows.length) throw new Error("Add at least one field");
    const doc = {};
    let count = 0;
    for (const row of rows) {
      const field = (row.querySelector(".batch-field")?.value || "").trim();
      const raw = row.querySelector(".batch-value")?.value ?? "";
      if (!field) continue;
      setPath(doc, field, parseFieldValue(raw));
      count += 1;
    }
    if (!count) throw new Error("Select at least one field");
    return doc;
  }

  function resetAll() {
    clearFilterValues();
    dslInput.value = DEFAULT_DSL;
    searchMode = "filters";
    activeDsl = null;
    page = 1;
    editingDocId = "";
    selectedIds.clear();
    setDslMsg("");
    setCreateMsg("");
    setEditMsg("");
    setBatchMsg("");
    setStatus("");
    closeAllDrawers();
  }

  function updatePager() {
    const pages = Math.max(1, Math.ceil(total / pageSize) || 1);
    if (page > pages) page = pages;
    pageInfoEl.textContent = `Page ${page} / ${pages} · ${total} doc(s)`;
    prevBtn.disabled = page <= 1;
    nextBtn.disabled = page >= pages || total === 0;
    pagerEl.hidden = false;
  }

  async function loadFields() {
    try {
      const data = await api(
        `/api/v1/es/conns/${connId}/indices/${encodeURIComponent(index)}/fields`
      );
      const list = data.fields || [];
      fields = list.length ? list : ["_id"];
      fieldInfos = Array.isArray(data.items) ? data.items : [];
    } catch {
      fields = ["_id"];
      fieldInfos = [];
    }
    renderFilterBar();
    renderTableHeader(topLevelColumns(fieldInfos, []));
    resetCreateJson();
  }

  async function searchDocs() {
    setStatus("");
    const from = (page - 1) * pageSize;
    const body =
      searchMode === "dsl" && activeDsl
        ? { dsl: activeDsl, from, size: pageSize }
        : (() => {
            const filters = collectFilters();
            const { sortField, sortOrder } = collectSort();
            return { filters, from, size: pageSize, sortField, sortOrder };
          })();

    tbody.innerHTML = emptyRow("Loading…");
    try {
      const data = await api(
        `/api/v1/es/conns/${connId}/indices/${encodeURIComponent(index)}/docs/search`,
        {
          method: "POST",
          body: JSON.stringify(body),
        }
      );
      loadedDocs = data.items || [];
      total = Number(data.total) || 0;
      updatePager();
      renderDocsTable(loadedDocs);
      setStatus("");
    } catch (e) {
      loadedDocs = [];
      total = 0;
      selectedIds.clear();
      updatePager();
      renderTableHeader(topLevelColumns(fieldInfos, []));
      tbody.innerHTML = emptyRow(e.message);
      setStatus(e.message, true);
    }
  }

  async function searchWithDsl() {
    try {
      const dsl = parseDslInput();
      activeDsl = dsl;
      searchMode = "dsl";
      page = 1;
      setDslMsg("");
      await searchDocs();
      if (statusEl.classList.contains("is-error")) {
        setDslMsg(statusEl.textContent, true);
      } else {
        setDslMsg(statusEl.textContent || "OK", false);
      }
    } catch (e) {
      setDslMsg(e.message || "Invalid JSON", true);
    }
  }

  async function saveDoc() {
    setCreateMsg("");
    let source;
    try {
      source = parseCreateJson();
    } catch (e) {
      setCreateMsg(e.message || "Invalid JSON", true);
      return;
    }
    const id = (createIdEl.value || "").trim();
    const btn = root.querySelector("#btn-save-doc");
    btn.disabled = true;
    try {
      const data = await api(`/api/v1/es/conns/${connId}/indices/${encodeURIComponent(index)}/docs`, {
        method: "POST",
        body: JSON.stringify({ id, source }),
      });
      setCreateMsg("");
      setStatus(`Created document ${data.id}`, false);
      closeAllDrawers();
      page = 1;
      await searchDocs();
    } catch (e) {
      setCreateMsg(e.message || "Save failed", true);
    } finally {
      btn.disabled = false;
    }
  }

  async function updateDoc() {
    setEditMsg("");
    const id = (editingDocId || editIdEl.textContent || "").trim();
    if (!id) {
      setEditMsg("Document id is required", true);
      return;
    }
    let source;
    try {
      source = parseEditJson();
    } catch (e) {
      setEditMsg(e.message || "Invalid JSON", true);
      return;
    }
    const btn = root.querySelector("#btn-update-doc");
    btn.disabled = true;
    try {
      const data = await api(
        `/api/v1/es/conns/${connId}/indices/${encodeURIComponent(index)}/docs/${encodeURIComponent(id)}`,
        {
          method: "PUT",
          body: JSON.stringify({ source }),
        }
      );
      setEditMsg("");
      setStatus(`Updated document ${data.id}`, false);
      closeAllDrawers();
      await searchDocs();
    } catch (e) {
      setEditMsg(e.message || "Save failed", true);
    } finally {
      btn.disabled = false;
    }
  }

  async function deleteSelectedDocs() {
    const ids = getSelectedIds();
    if (!ids.length) {
      setStatus("Select documents first", true);
      return;
    }
    if (!confirm(`确认删除选中的 ${ids.length} 条文档？`)) return;
    const btn = root.querySelector("#btn-delete-docs");
    btn.disabled = true;
    try {
      const data = await api(
        `/api/v1/es/conns/${connId}/indices/${encodeURIComponent(index)}/docs/bulk-delete`,
        {
          method: "POST",
          body: JSON.stringify({ ids }),
        }
      );
      selectedIds.clear();
      setStatus(`Deleted ${data.deleted ?? ids.length} document(s)`, false);
      await searchDocs();
    } catch (e) {
      setStatus(e.message || "Delete failed", true);
    } finally {
      btn.disabled = false;
    }
  }

  async function runBatchEdit() {
    setBatchMsg("");
    const ids = getSelectedIds();
    if (!ids.length) {
      setBatchMsg("Select documents first", true);
      return;
    }
    let doc;
    try {
      doc = collectBatchDoc();
    } catch (e) {
      setBatchMsg(e.message || "Invalid fields", true);
      return;
    }
    const btn = root.querySelector("#btn-run-batch-edit");
    btn.disabled = true;
    try {
      const data = await api(
        `/api/v1/es/conns/${connId}/indices/${encodeURIComponent(index)}/docs/bulk-update`,
        {
          method: "POST",
          body: JSON.stringify({ ids, doc }),
        }
      );
      const failed = Number(data.failed) || 0;
      const updated = Number(data.updated) || 0;
      if (failed) {
        setBatchMsg(`Updated ${updated}, failed ${failed}`, true);
      } else {
        setBatchMsg("");
        setStatus(`Updated ${updated} document(s)`, false);
        closeAllDrawers();
      }
      await searchDocs();
    } catch (e) {
      setBatchMsg(e.message || "Update failed", true);
    } finally {
      btn.disabled = false;
    }
  }

  function onKeydown(e) {
    if (e.key !== "Escape") return;
    if (
      drawerDsl.classList.contains("is-open") ||
      drawerCreate.classList.contains("is-open") ||
      drawerEdit.classList.contains("is-open") ||
      drawerBatch.classList.contains("is-open")
    ) {
      closeAllDrawers();
    }
  }

  root.querySelector("#btn-back-indices").addEventListener("click", () => {
    ctx.navigate("es", "indices", { connId, connName });
  });
  root.querySelector("#btn-refresh-docs").addEventListener("click", searchDocs);
  root.querySelector("#btn-custom-docs").addEventListener("click", openDslDrawer);
  root.querySelector("#btn-create-doc").addEventListener("click", openCreateDrawer);
  root.querySelector("#btn-delete-docs").addEventListener("click", deleteSelectedDocs);
  root.querySelector("#btn-batch-edit-docs").addEventListener("click", openBatchEditDrawer);
  root.querySelector("#btn-reset-docs").addEventListener("click", () => {
    resetAll();
    searchDocs();
  });
  root.querySelector("#btn-format-dsl").addEventListener("click", formatDsl);
  root.querySelector("#btn-search-dsl").addEventListener("click", searchWithDsl);
  root.querySelector("#btn-format-create-doc").addEventListener("click", formatCreateJson);
  root.querySelector("#btn-save-doc").addEventListener("click", saveDoc);
  root.querySelector("#btn-format-edit-doc").addEventListener("click", formatEditJson);
  root.querySelector("#btn-update-doc").addEventListener("click", updateDoc);
  root.querySelector("#btn-run-batch-edit").addEventListener("click", runBatchEdit);
  drawerDsl.addEventListener("click", (e) => {
    if (e.target.closest("[data-drawer-close]")) closeAllDrawers();
  });
  drawerCreate.addEventListener("click", (e) => {
    if (e.target.closest("[data-drawer-close]")) closeAllDrawers();
  });
  drawerEdit.addEventListener("click", (e) => {
    if (e.target.closest("[data-drawer-close]")) closeAllDrawers();
  });
  drawerBatch.addEventListener("click", (e) => {
    if (e.target.closest("[data-drawer-close]")) {
      closeAllDrawers();
      return;
    }
    if (e.target.closest("[data-act='add-batch-row']")) {
      appendBatchEditRow();
    }
  });
  document.addEventListener("keydown", onKeydown);
  root.querySelector("#docs-filter").addEventListener("submit", (e) => {
    e.preventDefault();
    searchMode = "filters";
    activeDsl = null;
    page = 1;
    selectedIds.clear();
    searchDocs();
  });
  pageSizeEl.addEventListener("change", () => {
    pageSize = Number(pageSizeEl.value) || 20;
    page = 1;
    selectedIds.clear();
    searchDocs();
  });
  prevBtn.addEventListener("click", () => {
    if (page <= 1) return;
    page -= 1;
    selectedIds.clear();
    searchDocs();
  });
  nextBtn.addEventListener("click", () => {
    const pages = Math.max(1, Math.ceil(total / pageSize) || 1);
    if (page >= pages) return;
    page += 1;
    selectedIds.clear();
    searchDocs();
  });
  theadRow.addEventListener("change", (e) => {
    const all = e.target.closest("#docs-check-all");
    if (!all) return;
    const checked = all.checked;
    tbody.querySelectorAll(".doc-check").forEach((el) => {
      el.checked = checked;
      const id = el.dataset.id;
      if (!id) return;
      if (checked) selectedIds.add(id);
      else selectedIds.delete(id);
    });
    all.indeterminate = false;
  });
  tbody.addEventListener("change", (e) => {
    const box = e.target.closest(".doc-check");
    if (!box) return;
    const id = box.dataset.id;
    if (!id) return;
    if (box.checked) selectedIds.add(id);
    else selectedIds.delete(id);
    syncSelectAll();
  });
  tbody.addEventListener("click", (e) => {
    const btn = e.target.closest("[data-act='open-doc']");
    if (!btn) return;
    const tr = btn.closest("tr");
    const idx = Number(tr?.dataset.idx);
    const doc = loadedDocs[idx];
    if (doc) openEditDrawer(doc);
  });

  await loadFields();
  await searchDocs();

  return {
    unmount() {
      document.removeEventListener("keydown", onKeydown);
      closeAllDrawers();
      root.innerHTML = "";
    },
  };
}
