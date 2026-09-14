import { esc, loadHTML } from "../../shared/util.js";

const HTML_URL = "/assets/clients/kafka/topics.html";

export async function mount(root, ctx, params = {}) {
  const { connId, connName } = params;
  if (!connId) {
    ctx.navigate("kafka", "connections");
    return { unmount() {} };
  }

  root.innerHTML = await loadHTML(HTML_URL);

  const api = ctx.api;
  const currentConn = { id: connId, name: connName || "" };
  const topicTbody = root.querySelector("#topic-tbody");
  const dlgTopic = root.querySelector("#dlg-topic");
  const qEl = root.querySelector("#topic-q");
  const pageSizeEl = root.querySelector("#topic-page-size");
  const pagerEl = root.querySelector("#topic-pager");
  const pageInfoEl = root.querySelector("#topic-page-info");
  const prevBtn = root.querySelector("#topic-prev");
  const nextBtn = root.querySelector("#topic-next");

  let page = 1;
  let pageSize = Number(pageSizeEl.value) || 20;
  let q = "";
  let total = 0;

  root.querySelector("#topics-title").textContent = `Topics · ${currentConn.name}`;
  root.querySelector("#topics-sub").textContent = `Connection #${currentConn.id}`;

  function fmtCount(n) {
    const v = Number(n) || 0;
    return v.toLocaleString();
  }

  function fmtUnit(n) {
    const v = Number(n) || 0;
    if (v >= 1e9) return `${(v / 1e9).toFixed(2)}G`;
    if (v >= 1e6) return `${(v / 1e6).toFixed(2)}M`;
    return `${(v / 1e3).toFixed(2)}K`;
  }

  function updatePager() {
    const pages = Math.max(1, Math.ceil(total / pageSize) || 1);
    if (page > pages) page = pages;
    pagerEl.hidden = total === 0;
    pageInfoEl.textContent = `Page ${page} / ${pages} · ${total} topic(s)`;
    prevBtn.disabled = page <= 1;
    nextBtn.disabled = page >= pages;
  }

  async function loadTopics() {
    topicTbody.innerHTML = `<tr><td colspan="6" class="empty">Loading…</td></tr>`;
    const qs = new URLSearchParams({
      page: String(page),
      pageSize: String(pageSize),
    });
    if (q) qs.set("q", q);
    try {
      const data = await api(`/api/v1/kafka/conns/${currentConn.id}/topics?${qs}`);
      const rows = data.items || [];
      total = data.total || 0;
      page = data.page || page;
      pageSize = data.pageSize || pageSize;
      updatePager();
      if (!rows.length) {
        topicTbody.innerHTML = `<tr><td colspan="6" class="empty">${q ? "No matching topics" : "No topics"}</td></tr>`;
        return;
      }
      topicTbody.innerHTML = rows
        .map(
          (t) => `<tr data-topic="${esc(t.name)}">
          <td><strong>${esc(t.name)}</strong></td>
          <td title="Currently retained messages">${fmtCount(t.messages)}</td>
          <td title="${fmtCount(t.total)} (sum of log-end offsets)">${fmtUnit(t.total)}</td>
          <td>${t.partitions}</td>
          <td>${t.replicationFactor}</td>
          <td>
            <div class="row-actions">
              <button type="button" data-act="messages">Message</button>
              <button type="button" data-act="detail">Details</button>
              <button type="button" class="danger" data-act="del">Delete</button>
            </div>
          </td>
        </tr>`
        )
        .join("");
    } catch (e) {
      total = 0;
      updatePager();
      topicTbody.innerHTML = `<tr><td colspan="6" class="empty">${esc(e.message)}</td></tr>`;
    }
  }

  async function openTopicDetail(topic) {
    root.querySelector("#dlg-topic-title").textContent = `Topic · ${topic}`;
    const body = root.querySelector("#topic-detail-body");
    body.innerHTML = "Loading…";
    dlgTopic.showModal();
    try {
      const d = await api(
        `/api/v1/kafka/conns/${currentConn.id}/topics/${encodeURIComponent(topic)}`
      );
      body.innerHTML = `
        <div class="detail-block">
          <h4>Overview</h4>
          <p>Partitions ${d.partitions?.length || 0} · Replication factor ${d.replicationFactor}</p>
        </div>
        <div class="detail-block">
          <h4>Partitions</h4>
          <div class="table-wrap">
            <table class="table">
              <thead><tr><th>Id</th><th>Leader</th><th>Replicas</th><th>Isr</th></tr></thead>
              <tbody>
                ${(d.partitions || [])
                  .map(
                    (p) => `<tr>
                    <td>${p.id}</td>
                    <td>${p.leader}</td>
                    <td>${esc((p.replicas || []).join(", "))}</td>
                    <td>${esc((p.isr || []).join(", "))}</td>
                  </tr>`
                  )
                  .join("") || `<tr><td colspan="4" class="empty">No data</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
        <div class="detail-block">
          <h4>Brokers</h4>
          <div class="table-wrap">
            <table class="table">
              <thead><tr><th>Id</th><th>Address</th><th>Rack</th></tr></thead>
              <tbody>
                ${(d.brokers || [])
                  .map(
                    (b) => `<tr>
                    <td>${b.id}</td>
                    <td>${esc(b.addr)}</td>
                    <td>${esc(b.rack || "-")}</td>
                  </tr>`
                  )
                  .join("") || `<tr><td colspan="3" class="empty">No data</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>
        <div class="detail-block">
          <h4>Consumer Groups</h4>
          <div class="table-wrap">
            <table class="table">
              <thead><tr><th>Group</th><th>State</th><th>Members</th><th>Topics</th></tr></thead>
              <tbody>
                ${(d.consumerGroups || [])
                  .map(
                    (g) => `<tr>
                    <td>${esc(g.groupId)}</td>
                    <td>${esc(g.state)}</td>
                    <td>${g.members}</td>
                    <td>${esc((g.topics || []).join(", "))}</td>
                  </tr>`
                  )
                  .join("") || `<tr><td colspan="4" class="empty">No consumer groups for this topic</td></tr>`}
              </tbody>
            </table>
          </div>
        </div>`;
    } catch (e) {
      body.innerHTML = `<p class="msg is-error">${esc(e.message)}</p>`;
    }
  }

  root.querySelector("#btn-back-conns").addEventListener("click", () => {
    ctx.navigate("kafka", "connections");
  });
  root.querySelector("#btn-refresh-topics").addEventListener("click", loadTopics);
  root.querySelector("#topic-close").addEventListener("click", () => dlgTopic.close());

  root.querySelector("#topic-filter").addEventListener("submit", (e) => {
    e.preventDefault();
    q = qEl.value.trim();
    pageSize = Number(pageSizeEl.value) || 20;
    page = 1;
    loadTopics();
  });

  pageSizeEl.addEventListener("change", () => {
    pageSize = Number(pageSizeEl.value) || 20;
    page = 1;
    loadTopics();
  });

  prevBtn.addEventListener("click", () => {
    if (page <= 1) return;
    page -= 1;
    loadTopics();
  });
  nextBtn.addEventListener("click", () => {
    const pages = Math.max(1, Math.ceil(total / pageSize) || 1);
    if (page >= pages) return;
    page += 1;
    loadTopics();
  });

  topicTbody.addEventListener("click", async (e) => {
    const btn = e.target.closest("button[data-act]");
    if (!btn) return;
    const tr = btn.closest("tr");
    const topic = tr?.dataset.topic;
    if (!topic) return;
    if (btn.dataset.act === "messages") {
      ctx.navigate("kafka", "messages", {
        connId: currentConn.id,
        connName: currentConn.name,
        topic,
      });
    } else if (btn.dataset.act === "del") {
      if (!confirm(`Delete topic "${topic}"? This cannot be undone.`)) return;
      try {
        await api(`/api/v1/kafka/conns/${currentConn.id}/topics/${encodeURIComponent(topic)}`, {
          method: "DELETE",
        });
        loadTopics();
      } catch (err) {
        alert(err.message);
      }
    } else if (btn.dataset.act === "detail") {
      openTopicDetail(topic);
    }
  });

  await loadTopics();

  return {
    unmount() {
      dlgTopic.close();
      root.innerHTML = "";
    },
  };
}
