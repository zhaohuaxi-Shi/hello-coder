export function esc(s) {
  return String(s ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

export function statusBadge(available, message) {
  if (available === true) return `<span class="badge ok" title="${esc(message || "")}">Available</span>`;
  if (available === false) return `<span class="badge bad" title="${esc(message || "")}">Unavailable</span>`;
  return `<span class="badge unknown">Unknown</span>`;
}

export async function loadHTML(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`Failed to load ${url}`);
  return res.text();
}
