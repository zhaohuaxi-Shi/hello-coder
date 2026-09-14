(async () => {
  try {
    const res = await fetch("/api/v1/meta");
    const body = await res.json().catch(() => ({}));
    if (res.ok && body.code === 0 && body.data) {
      if (body.data.authRequired === false) {
        location.replace("/app.html");
        return;
      }
      const verEl = document.getElementById("app-version");
      if (verEl && body.data.version) {
        verEl.textContent = "v" + body.data.version;
        verEl.hidden = false;
      }
    }
  } catch (_) {
    /* continue to login form */
  }

  const token = localStorage.getItem("hc_token");
  if (token) {
    location.replace("/app.html");
    return;
  }

  const form = document.getElementById("auth-form");
  const usernameEl = document.getElementById("username");
  const passwordEl = document.getElementById("password");
  const submitEl = document.getElementById("submit");
  const msgEl = document.getElementById("msg");
  const tabs = document.querySelectorAll(".tab");
  let mode = "login";

  function setMode(next) {
    mode = next;
    tabs.forEach((tab) => {
      const active = tab.dataset.tab === mode;
      tab.classList.toggle("is-active", active);
      tab.setAttribute("aria-selected", active ? "true" : "false");
    });
    submitEl.textContent = mode === "login" ? "Sign in" : "Register & continue";
    hideMsg();
  }

  tabs.forEach((tab) => tab.addEventListener("click", () => setMode(tab.dataset.tab)));

  function showMsg(text, ok) {
    msgEl.hidden = false;
    msgEl.textContent = text;
    msgEl.classList.toggle("is-ok", !!ok);
    msgEl.classList.toggle("is-error", !ok);
  }
  function hideMsg() {
    msgEl.hidden = true;
    msgEl.textContent = "";
  }

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    hideMsg();
    const username = usernameEl.value.trim();
    const password = passwordEl.value;
    if (!username || !password) {
      showMsg("Please enter username and password", false);
      return;
    }
    submitEl.disabled = true;
    try {
      const res = await fetch(`/api/v1/auth/${mode}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username, password }),
      });
      const body = await res.json();
      if (!res.ok || body.code !== 0) {
        showMsg(body.message || "Request failed", false);
        return;
      }
      localStorage.setItem("hc_token", body.data.token);
      localStorage.setItem("hc_user", JSON.stringify(body.data.user));
      location.href = "/app.html";
    } catch (_) {
      showMsg("Network error, please try again", false);
    } finally {
      submitEl.disabled = false;
    }
  });

  setMode("login");
})();
