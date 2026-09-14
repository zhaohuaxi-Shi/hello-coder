import { esc, loadHTML } from "../../shared/util.js";

const HTML_URL = "/assets/clients/redis/details.html";

function yesNo(v) {
  return v ? `<span class="badge ok">Yes</span>` : `<span class="badge unknown">No</span>`;
}

function statusText(v) {
  const s = String(v || "").toLowerCase();
  if (s === "ok") return `<span class="badge ok">ok</span>`;
  if (s === "err" || s === "error") return `<span class="badge bad">${esc(v)}</span>`;
  return esc(v || "—");
}

function formatSec(sec) {
  if (sec == null || sec === "" || Number(sec) < 0) return "—";
  const n = Number(sec);
  if (!Number.isFinite(n)) return "—";
  if (n < 1) return `${n}s`;
  if (n < 60) return `${n}s`;
  const m = Math.floor(n / 60);
  const s = n % 60;
  return s ? `${m}m ${s}s` : `${m}m`;
}

function kvItems(items) {
  return items
    .map(([k, v]) => `<div class="kv-item"><div class="kv-label">${k}</div><div class="kv-value">${v}</div></div>`)
    .join("");
}

function persistMetrics(items) {
  return `<div class="persist-metrics">${items
    .map(
      ([label, value]) =>
        `<div class="persist-metric"><div class="persist-metric-label">${label}</div><div class="persist-metric-value">${value}</div></div>`
    )
    .join("")}</div>`;
}

function renderDetail(detailBody, d) {
  if (!d?.available) {
    detailBody.innerHTML = `<div class="empty-card empty">Unavailable: ${esc(d?.message || "failed")}</div>`;
    return;
  }
  const dbs = d.databases || [];
  detailBody.innerHTML = `
    <div class="detail-block">
      <h4>Runtime</h4>
      <div class="kv-grid">
        ${kvItems([
          ["Version", esc(d.version || "—")],
          ["Uptime (days)", esc(String(d.uptimeDays ?? "—"))],
          ["Used memory", esc(d.usedMemory || "—")],
          ["Max memory", esc(d.maxMemory || "—")],
          ["Evicted keys", esc(String(d.evictedKeys ?? 0))],
        ])}
      </div>
    </div>
    <div class="detail-block">
      <h4>Persistence</h4>
      <div class="kv-grid persist-grid">
        <div class="kv-item">
          <div class="kv-label">RDB</div>
          ${persistMetrics([
            ["Enabled", yesNo(d.rdbEnabled)],
            ["Last status", d.rdbEnabled ? statusText(d.rdbLastBgsaveStatus) : "—"],
            ["Last duration", `<strong>${esc(d.rdbEnabled ? formatSec(d.rdbLastBgsaveTimeSec) : "—")}</strong>`],
          ])}
        </div>
        <div class="kv-item">
          <div class="kv-label">AOF</div>
          ${persistMetrics([
            ["Enabled", yesNo(d.aofEnabled)],
            ["Last status", d.aofEnabled ? statusText(d.aofLastBgrewriteStatus) : "—"],
            ["Last duration", `<strong>${esc(d.aofEnabled ? formatSec(d.aofLastRewriteTimeSec) : "—")}</strong>`],
          ])}
        </div>
      </div>
    </div>
    <div class="detail-block">
      <h4>Databases</h4>
      <div class="table-wrap drawer-table">
        <table class="table">
          <thead>
            <tr>
              <th>DB</th>
              <th>Keys</th>
              <th>Expires</th>
            </tr>
          </thead>
          <tbody>
            ${
              dbs.length
                ? dbs
                    .map(
                      (row) => `<tr>
                  <td>db${row.db}</td>
                  <td>${esc(String(row.keys ?? 0))}</td>
                  <td>${esc(String(row.expires ?? 0))}</td>
                </tr>`
                    )
                    .join("")
                : `<tr><td colspan="3" class="empty">No databases with keys</td></tr>`
            }
          </tbody>
        </table>
      </div>
    </div>`;
}

/** Attach Redis details drawer into host. Returns { openDetail, close, unmount }. */
export async function mountDetailDrawer(host, { api }) {
  host.insertAdjacentHTML("beforeend", await loadHTML(HTML_URL));

  const drawer = host.querySelector("#drawer-detail");
  const detailBody = host.querySelector("#drawer-detail-body");
  const titleEl = host.querySelector("#drawer-title");
  const subEl = host.querySelector("#drawer-sub");
  let detailConnId = null;

  function openDrawer() {
    drawer.classList.add("is-open");
    drawer.setAttribute("aria-hidden", "false");
    document.body.classList.add("drawer-open");
  }

  function close() {
    drawer.classList.remove("is-open");
    drawer.setAttribute("aria-hidden", "true");
    document.body.classList.remove("drawer-open");
    detailConnId = null;
  }

  async function loadDetail() {
    if (!detailConnId) return;
    detailBody.innerHTML = `<div class="empty-card empty">Loading…</div>`;
    try {
      const data = await api(`/api/v1/redis/conns/${detailConnId}/info`);
      renderDetail(detailBody, data);
    } catch (e) {
      detailBody.innerHTML = `<div class="empty-card empty">${esc(e.message)}</div>`;
    }
  }

  async function openDetail(id, name) {
    detailConnId = id;
    titleEl.textContent = `Redis · ${name || "#" + id}`;
    subEl.textContent = `Connection #${id}`;
    detailBody.innerHTML = `<div class="empty-card empty">Loading…</div>`;
    openDrawer();
    await loadDetail();
  }

  function onDrawerClick(e) {
    if (e.target.closest("[data-drawer-close]")) close();
  }

  function onKeydown(e) {
    if (e.key === "Escape" && drawer.classList.contains("is-open")) {
      close();
    }
  }

  drawer.addEventListener("click", onDrawerClick);
  document.addEventListener("keydown", onKeydown);
  host.querySelector("#btn-refresh-detail").addEventListener("click", loadDetail);

  return {
    openDetail,
    close,
    unmount() {
      document.removeEventListener("keydown", onKeydown);
      close();
      drawer.remove();
    },
  };
}
