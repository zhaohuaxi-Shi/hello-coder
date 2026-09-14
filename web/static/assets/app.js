import { loadMeta, requireAuth, createApi, isAuthRequired } from "./shared/api.js";

async function main() {
  const meta = await loadMeta();
  const token = requireAuth();
  if (token === null) return;

  const api = createApi(token);
  let user = JSON.parse(localStorage.getItem("hc_user") || "{}");
  if (!isAuthRequired()) {
    try {
      user = (await api("/api/v1/me")) || user;
      localStorage.setItem("hc_user", JSON.stringify(user));
    } catch (_) {
      /* optional */
    }
    document.getElementById("logout")?.remove();
    document.querySelector(".sidebar-foot")?.remove();
  }

  const brand = document.getElementById("sidebar-brand");
  if (brand) {
    const parts = ["Home"];
    if (user.username) parts.push(user.username);
    if (meta.version) parts.push(`v${meta.version}`);
    brand.dataset.tip = parts.join(" · ");
  }

  const tabbarEl = document.getElementById("workspace-tabbar");
  const panesEl = document.getElementById("workspace-panes");

  const screens = {
    home: {
      overview: () => import("./home/overview.js"),
    },
    kafka: {
      connections: () => import("./clients/kafka/connections.js"),
      topics: () => import("./clients/kafka/topics.js"),
      messages: () => import("./clients/kafka/messages.js"),
    },
    es: {
      connections: () => import("./clients/es/connections.js"),
      indices: () => import("./clients/es/indices.js"),
      docs: () => import("./clients/es/docs.js"),
    },
    redis: {
      connections: () => import("./clients/redis/connections.js"),
      cache: () => import("./clients/redis/cache.js"),
    },
  };

  const DEFAULT_ROUTE = { client: "home", screen: "overview", params: {} };
  const CLIENT_MARK = { kafka: "KFK", es: "ES", redis: "RDS" };
  const CLIENT_LABEL = { kafka: "Kafka", es: "Elasticsearch", redis: "Redis" };

  const tabs = [];
  const hubs = new Map();
  let activeTabId = null;
  let tabSeq = 0;
  let applyingUrl = false;

  const ctx = { api, navigate };

  function buildHash(client, screen, params = {}) {
    const qs = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v == null || v === "") continue;
      qs.set(k, String(v));
    }
    const q = qs.toString();
    return `#/${client}/${screen}${q ? `?${q}` : ""}`;
  }

  function parseHash() {
    let raw = location.hash || "";
    if (raw.startsWith("#")) raw = raw.slice(1);
    if (raw.startsWith("/")) raw = raw.slice(1);
    if (!raw) return null;

    const qIdx = raw.indexOf("?");
    const path = qIdx >= 0 ? raw.slice(0, qIdx) : raw;
    const query = qIdx >= 0 ? raw.slice(qIdx + 1) : "";
    const [client, screen] = path.split("/").filter(Boolean);
    if (!client || !screen || !screens[client]?.[screen]) return null;

    return {
      client,
      screen,
      params: Object.fromEntries(new URLSearchParams(query)),
    };
  }

  function isHub(client, screen) {
    return client === "home" || screen === "connections";
  }

  function hubKey(client) {
    return client === "home" ? "home" : client;
  }

  function hubScreen(client) {
    return client === "home" ? "overview" : "connections";
  }

  function workspaceKey(client, params = {}) {
    return `${client}:conn:${params.connId}`;
  }

  function tabTitle(params = {}) {
    return String(params.connName || `#${params.connId}`);
  }

  function paramQuery(params = {}) {
    const qs = new URLSearchParams();
    const keys = Object.keys(params).sort();
    for (const k of keys) {
      const v = params[k];
      if (v == null || v === "") continue;
      qs.set(k, String(v));
    }
    return qs.toString();
  }

  function sameRoute(tab, client, screen, params) {
    return tab.client === client && tab.screen === screen && paramQuery(tab.params) === paramQuery(params);
  }

  function syncSidebar(client) {
    document.querySelectorAll(".nav-item").forEach((btn) => {
      btn.classList.toggle("is-active", btn.dataset.tool === client);
    });
    document.getElementById("sidebar-brand")?.classList.toggle("is-active", client === "home");
  }

  function syncUrl(client, screen, params, { push = false } = {}) {
    const next = buildHash(client, screen, params);
    if (location.hash === next) return;
    applyingUrl = true;
    if (push) history.pushState(null, "", next);
    else history.replaceState(null, "", next);
    applyingUrl = false;
  }

  function setPaneVisible(pane, on) {
    pane.classList.toggle("is-active", on);
    pane.hidden = !on;
    pane.inert = !on;
  }

  function hideAllPanes() {
    for (const t of tabs) setPaneVisible(t.pane, false);
    for (const h of hubs.values()) setPaneVisible(h.pane, false);
  }

  function makePane(id) {
    const pane = document.createElement("div");
    pane.className = "workspace-pane";
    pane.id = id;
    pane.hidden = true;
    pane.setAttribute("role", "tabpanel");
    pane.innerHTML = `<div class="empty-card">Loading…</div>`;
    panesEl.appendChild(pane);
    return pane;
  }

  function renderTabbar() {
    tabbarEl.hidden = tabs.length === 0;
    tabbarEl.replaceChildren();
    for (const tab of tabs) {
      const el = document.createElement("div");
      el.className = "workspace-tab" + (tab.id === activeTabId ? " is-active" : "");
      el.dataset.tabId = tab.id;
      el.dataset.client = tab.client;
      el.setAttribute("role", "tab");
      el.setAttribute("aria-selected", tab.id === activeTabId ? "true" : "false");
      const kind = CLIENT_MARK[tab.client] || tab.client;
      const product = CLIENT_LABEL[tab.client] || tab.client;
      el.title = `${product} · ${tab.title}`;

      const hit = document.createElement("button");
      hit.type = "button";
      hit.className = "workspace-tab-hit";

      const mark = document.createElement("span");
      mark.className = "workspace-tab-kind";
      mark.textContent = kind;
      hit.appendChild(mark);

      const name = document.createElement("span");
      name.className = "workspace-tab-name";
      name.textContent = tab.title;
      hit.appendChild(name);
      el.appendChild(hit);

      const close = document.createElement("button");
      close.type = "button";
      close.className = "workspace-tab-close";
      close.setAttribute("aria-label", `Close ${product} ${tab.title}`);
      close.textContent = "×";
      el.appendChild(close);

      tabbarEl.appendChild(el);
    }
    const activeBtn = tabbarEl.querySelector(".workspace-tab.is-active");
    activeBtn?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }

  function showTab(tab) {
    hideAllPanes();
    activeTabId = tab.id;
    setPaneVisible(tab.pane, true);
    syncSidebar(tab.client);
    renderTabbar();
  }

  function showHubPane(hub) {
    hideAllPanes();
    activeTabId = null;
    setPaneVisible(hub.pane, true);
    syncSidebar(hub.client);
    renderTabbar();
  }

  async function mountPane(owner, client, screen, params) {
    const seq = ++owner.mountSeq;
    if (owner.view?.unmount) {
      try {
        owner.view.unmount();
      } catch (_) {
        /* ignore */
      }
      owner.view = null;
    }

    const loader = screens[client]?.[screen];
    if (!loader) {
      owner.pane.innerHTML = `<div class="empty-card">Unknown view</div>`;
      return;
    }

    owner.pane.innerHTML = `<div class="empty-card">Loading…</div>`;
    try {
      const mod = await loader();
      if (seq !== owner.mountSeq) return;
      owner.view = (await mod.mount(owner.pane, ctx, params)) || { unmount() {} };
    } catch (e) {
      if (seq !== owner.mountSeq) return;
      const msg = String(e.message || e);
      const safe = msg.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
      owner.pane.innerHTML = `<div class="empty-card">${safe}</div>`;
    }
  }

  function ensureHub(client) {
    const key = hubKey(client);
    let hub = hubs.get(key);
    if (hub) return hub;
    hub = {
      key,
      client,
      screen: hubScreen(client),
      pane: makePane(`pane-hub-${key}`),
      view: null,
      mountSeq: 0,
    };
    hubs.set(key, hub);
    return hub;
  }

  function createTab(client, screen, params) {
    const id = `t${++tabSeq}`;
    const tab = {
      id,
      key: workspaceKey(client, params),
      client,
      screen,
      params: { ...params },
      title: tabTitle(params),
      closable: true,
      pane: makePane(`pane-${id}`),
      view: null,
      mountSeq: 0,
    };
    tabs.push(tab);
    return tab;
  }

  async function openHub(client, { fromHistory = false } = {}) {
    const leavingTab = activeTabId != null;
    const hub = ensureHub(client);
    const first = !hub.view;
    showHubPane(hub);
    if (!fromHistory) syncUrl(hub.client, hub.screen, {}, { push: leavingTab });
    if (first) await mountPane(hub, hub.client, hub.screen, {});
  }

  function closeTab(id) {
    const idx = tabs.findIndex((t) => t.id === id);
    if (idx < 0) return;
    const tab = tabs[idx];

    tab.mountSeq += 1;
    if (tab.view?.unmount) {
      try {
        tab.view.unmount();
      } catch (_) {
        /* ignore */
      }
    }
    tab.pane.remove();
    tabs.splice(idx, 1);

    if (activeTabId !== id) {
      renderTabbar();
      return;
    }

    const next = tabs[idx] || tabs[idx - 1];
    if (next) {
      showTab(next);
      syncUrl(next.client, next.screen, next.params, { push: false });
      return;
    }
    navigate("home", "overview");
  }

  function activateExisting(tab) {
    showTab(tab);
    syncUrl(tab.client, tab.screen, tab.params, { push: false });
  }

  async function navigate(client, screen, params = {}, opts = {}) {
    if (!screens[client]?.[screen]) return;
    if (!isHub(client, screen) && !params.connId) {
      screen = client === "home" ? "overview" : "connections";
      params = {};
    }

    const fromHistory = !!opts.fromHistory;
    if (isHub(client, screen)) {
      await openHub(client, { fromHistory });
      return;
    }

    const key = workspaceKey(client, params);
    let tab = tabs.find((t) => t.key === key);
    const created = !tab;
    if (!tab) tab = createTab(client, screen, params);

    if (!created && !fromHistory && tab.id !== activeTabId) {
      showTab(tab);
      syncUrl(tab.client, tab.screen, tab.params, { push: false });
      return;
    }

    const unchanged = !created && sameRoute(tab, client, screen, params);
    if (!unchanged) {
      tab.client = client;
      tab.screen = screen;
      tab.params = { ...params };
      tab.title = tabTitle(params);
      tab.key = key;
    }

    showTab(tab);
    if (!fromHistory) syncUrl(tab.client, tab.screen, tab.params, { push: created || !unchanged });
    if (!unchanged) await mountPane(tab, tab.client, tab.screen, tab.params);
  }

  function onHistoryChange() {
    if (applyingUrl) return;
    const route = parseHash();
    if (!route) {
      applyingUrl = true;
      history.replaceState(null, "", buildHash(DEFAULT_ROUTE.client, DEFAULT_ROUTE.screen, DEFAULT_ROUTE.params));
      applyingUrl = false;
      navigate(DEFAULT_ROUTE.client, DEFAULT_ROUTE.screen, DEFAULT_ROUTE.params, { fromHistory: true });
      return;
    }
    navigate(route.client, route.screen, route.params, { fromHistory: true });
  }

  tabbarEl.addEventListener("click", (e) => {
    const closeBtn = e.target.closest(".workspace-tab-close");
    if (closeBtn) {
      e.preventDefault();
      const id = closeBtn.closest("[data-tab-id]")?.dataset.tabId;
      if (id) closeTab(id);
      return;
    }
    const tabEl = e.target.closest("[data-tab-id]");
    if (!tabEl) return;
    const tab = tabs.find((t) => t.id === tabEl.dataset.tabId);
    if (tab) activateExisting(tab);
  });

  tabbarEl.addEventListener("mousedown", (e) => {
    if (e.button === 1 && e.target.closest("[data-tab-id]")) e.preventDefault();
  });
  tabbarEl.addEventListener("auxclick", (e) => {
    if (e.button !== 1) return;
    const tabEl = e.target.closest("[data-tab-id]");
    if (!tabEl) return;
    e.preventDefault();
    closeTab(tabEl.dataset.tabId);
  });

  document.getElementById("logout")?.addEventListener("click", () => {
    localStorage.removeItem("hc_token");
    localStorage.removeItem("hc_user");
    location.href = "/";
  });

  document.getElementById("sidebar-brand")?.addEventListener("click", () => {
    navigate("home", "overview");
  });

  document.querySelectorAll(".nav-item").forEach((btn) => {
    btn.addEventListener("click", () => {
      const tool = btn.dataset.tool;
      if (tool === "kafka") navigate("kafka", "connections");
      else if (tool === "es") navigate("es", "connections");
      else if (tool === "redis") navigate("redis", "connections");
    });
  });

  window.addEventListener("hashchange", onHistoryChange);
  window.addEventListener("popstate", onHistoryChange);

  const initial = parseHash();
  if (initial) {
    navigate(initial.client, initial.screen, initial.params, { fromHistory: true });
  } else {
    history.replaceState(null, "", buildHash(DEFAULT_ROUTE.client, DEFAULT_ROUTE.screen, DEFAULT_ROUTE.params));
    navigate(DEFAULT_ROUTE.client, DEFAULT_ROUTE.screen, DEFAULT_ROUTE.params, { fromHistory: true });
  }
}

main();
