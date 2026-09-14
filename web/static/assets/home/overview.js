import { getAppVersion } from "../shared/api.js";
import { loadHTML } from "../shared/util.js";

const HTML_URL = "/assets/home/overview.html";

const MODULES = [
  { client: "kafka", path: "/api/v1/kafka/status" },
  { client: "es", path: "/api/v1/es/status" },
  { client: "redis", path: "/api/v1/redis/status" },
];

export async function mount(root, ctx) {
  root.innerHTML = await loadHTML(HTML_URL);
  const api = ctx.api;
  let cancelled = false;

  const verEl = root.querySelector("#app-version");
  const ver = getAppVersion();
  if (verEl && ver) {
    verEl.textContent = "v" + ver;
    verEl.hidden = false;
  }

  function applyStats(card, data) {
    const totalEl = card.querySelector('[data-stat="total"]');
    const abnormalEl = card.querySelector('[data-stat="abnormal"]');
    if (totalEl) {
      totalEl.textContent = String(data?.total ?? 0);
      totalEl.classList.remove("is-pending");
    }
    if (abnormalEl) {
      abnormalEl.textContent = String(data?.abnormal ?? 0);
      abnormalEl.classList.remove("is-pending");
    }
  }

  root.querySelectorAll(".home-card").forEach((card) => {
    card.addEventListener("click", () => {
      const client = card.dataset.client;
      if (client) ctx.navigate(client, "connections");
    });
  });

  for (const mod of MODULES) {
    const card = root.querySelector(`.home-card[data-client="${mod.client}"]`);
    if (!card) continue;
    api(mod.path)
      .then((data) => {
        if (!cancelled) applyStats(card, data);
      })
      .catch(() => {
        if (!cancelled) applyStats(card, { total: 0, abnormal: 0 });
      });
  }

  return {
    unmount() {
      cancelled = true;
    },
  };
}
