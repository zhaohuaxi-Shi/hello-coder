let authRequired = true;
let appVersion = "";

export function getToken() {
  return localStorage.getItem("hc_token");
}

export function getAppVersion() {
  return appVersion;
}

export async function loadMeta() {
  try {
    const res = await fetch("/api/v1/meta");
    const body = await res.json().catch(() => ({}));
    if (res.ok && body.code === 0 && body.data) {
      authRequired = body.data.authRequired !== false;
      appVersion = body.data.version || "";
      return body.data;
    }
  } catch (_) {
    /* fall through — assume login required */
  }
  authRequired = true;
  return { authRequired: true };
}

export function isAuthRequired() {
  return authRequired;
}

export function requireAuth() {
  if (!authRequired) {
    return "";
  }
  const token = getToken();
  if (!token) {
    location.replace("/");
    return null;
  }
  return token;
}

export function createApi(token) {
  return async function api(path, options = {}) {
    const headers = {
      "Content-Type": "application/json",
      ...(options.headers || {}),
    };
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    }
    const res = await fetch(path, {
      ...options,
      headers,
    });
    const body = await res.json().catch(() => ({}));
    if (res.status === 401 && authRequired) {
      localStorage.removeItem("hc_token");
      location.replace("/");
      throw new Error("Unauthorized");
    }
    if (!res.ok || body.code !== 0) {
      throw new Error(body.message || "Request failed");
    }
    return body.data;
  };
}
