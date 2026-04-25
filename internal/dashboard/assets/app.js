// proxyhub dashboard — vanilla JS, no deps
(() => {
  const REFRESH_MS = 5000;
  const HISTORY_LEN = 12; // 60s / 5s
  const history = {
    available: [],
    banned: [],
    latency: [],
  };

  const $ = (s) => document.querySelector(s);
  const $$ = (s) => document.querySelectorAll(s);

  async function api(path, opts = {}) {
    const r = await fetch(path, opts);
    if (!r.ok) throw new Error(`${r.status} ${r.statusText}`);
    const ct = r.headers.get("content-type") || "";
    return ct.includes("json") ? r.json() : r.text();
  }

  function fmtNum(n) {
    if (n === null || n === undefined) return "—";
    if (n < 1000) return String(n);
    if (n < 10000) return (n / 1000).toFixed(1) + "k";
    return Math.round(n / 1000) + "k";
  }
  function fmtMs(n) {
    if (!n && n !== 0) return "—";
    return Math.round(n) + "ms";
  }
  function fmtPct(n) {
    return (n * 100).toFixed(1) + "%";
  }
  function fmtDuration(sec) {
    if (sec < 60) return sec + "s";
    if (sec < 3600) return Math.floor(sec / 60) + "m " + (sec % 60) + "s";
    return Math.floor(sec / 3600) + "h " + Math.floor((sec % 3600) / 60) + "m";
  }
  function flag(iso) {
    if (!iso || iso.length !== 2) return "🌐";
    const A = 0x1f1e6;
    return String.fromCodePoint(A + iso.charCodeAt(0) - 65) +
           String.fromCodePoint(A + iso.charCodeAt(1) - 65);
  }

  function log(msg, cls = "") {
    const el = $("#ops-log");
    const time = new Date().toLocaleTimeString();
    el.textContent = `[${time}] ${msg}\n` + el.textContent;
    if (el.textContent.length > 2000) {
      el.textContent = el.textContent.slice(0, 2000);
    }
  }

  // ============ render ============
  function renderHealth(h) {
    const total = h.pool_size || 0;
    const avail = h.available || 0;
    const banned = total - avail;
    const check = h.checker || {};

    $("#m-total").textContent = fmtNum(total);
    $("#m-avail").textContent = fmtNum(avail);
    $("#m-avail-pct").textContent = total > 0 ? fmtPct(avail / total) : "";
    $("#m-banned").textContent = fmtNum(banned);
    $("#m-sess").textContent = fmtNum(h.sessions || 0);
    $("#m-preq").textContent = fmtNum(h.proxy_reqs || 0);

    $("#uptime").textContent = h.uptime || "—";
    $("#dot").classList.toggle("ok", h.status === "ok");

    history.available.push(avail);
    history.banned.push(banned);
    if (history.available.length > HISTORY_LEN) history.available.shift();
    if (history.banned.length > HISTORY_LEN) history.banned.shift();
    drawStacked("chart-avail", history.available, history.banned);
  }

  function renderStats(s) {
    $("#m-latency").textContent = fmtMs(s.avg_latency_ms);
    history.latency.push(s.avg_latency_ms || 0);
    if (history.latency.length > HISTORY_LEN) history.latency.shift();
    drawLine("chart-lat", history.latency, "#5b8cff");

    // Country distribution
    const box = $("#countries");
    const countries = s.by_country || {};
    const sorted = Object.entries(countries).sort((a, b) => b[1] - a[1]).slice(0, 24);
    box.innerHTML = sorted.map(([c, n]) =>
      `<div class="country-pill"><span class="flag">${flag(c)} ${c || "ZZ"}</span><span class="count">${n}</span></div>`
    ).join("") || '<div style="padding:16px 20px;color:var(--muted)">no data</div>';
  }

  let sortKey = "score", sortDir = -1;
  function renderProxies(proxies) {
    proxies.sort((a, b) => {
      const x = a[sortKey], y = b[sortKey];
      if (typeof x === "string") return x.localeCompare(y) * sortDir;
      return ((x || 0) - (y || 0)) * sortDir;
    });
    const tbody = $("#tbl-proxies tbody");
    tbody.innerHTML = proxies.map(p => {
      const status = p.is_banned
        ? '<span class="badge err">banned</span>'
        : '<span class="badge ok">ok</span>';
      const scorePct = Math.min(100, Math.max(0, (p.score || 0) * 100));
      return `<tr>
        <td>${flag(p.country)} ${p.country || "ZZ"}</td>
        <td class="url" title="${p.url}">${p.url}</td>
        <td><span class="badge proto-${p.protocol}">${p.protocol}</span></td>
        <td class="num">
          <span class="score-bar"><span style="width:${scorePct}%"></span></span>
          ${(p.score || 0).toFixed(2)}
        </td>
        <td class="num">${fmtPct(p.success_rate || 0)}</td>
        <td class="num">${fmtMs(p.avg_latency_ms)}</td>
        <td class="num">${fmtNum(p.total_requests || 0)}</td>
        <td>${status}</td>
      </tr>`;
    }).join("");
    // Header sort indicator
    $$("#tbl-proxies thead th").forEach(th => {
      th.classList.remove("sort-asc", "sort-desc");
      if (th.dataset.sort === sortKey) {
        th.classList.add(sortDir === 1 ? "sort-asc" : "sort-desc");
      }
    });
  }

  function renderSessions(sessions) {
    $("#sess-count").textContent = `${sessions.length} session${sessions.length === 1 ? "" : "s"}`;
    const tbody = $("#tbl-sessions tbody");
    if (sessions.length === 0) {
      tbody.innerHTML = '<tr><td colspan="6" style="text-align:center;padding:20px;color:var(--muted)">no active sessions</td></tr>';
      return;
    }
    tbody.innerHTML = sessions.map(s => {
      const p = s.proxy || {};
      return `<tr data-id="${s.id}">
        <td>${s.id}</td>
        <td class="url" title="${p.url || '—'}">${p.url || '—'}</td>
        <td>${flag(p.country)} ${p.country || '—'}</td>
        <td class="num">${s.failure_count || 0}</td>
        <td class="num">${fmtDuration(Math.max(0, s.expires_in || 0))}</td>
        <td>
          <button class="sm" data-rotate="${s.id}">rotate</button>
          <button class="sm danger" data-delete="${s.id}">×</button>
        </td>
      </tr>`;
    }).join("");
    tbody.querySelectorAll("[data-rotate]").forEach(b => b.onclick = () => rotateSession(b.dataset.rotate));
    tbody.querySelectorAll("[data-delete]").forEach(b => b.onclick = () => deleteSession(b.dataset.delete));
  }

  async function rotateSession(id) {
    try {
      await api(`/api/v1/sessions/rotate?id=${encodeURIComponent(id)}`, { method: "POST" });
      log(`session ${id} rotated`);
      await refreshSessions();
    } catch (e) { log(`rotate failed: ${e.message}`); }
  }
  async function deleteSession(id) {
    try {
      await api(`/api/v1/sessions?id=${encodeURIComponent(id)}`, { method: "DELETE" });
      log(`session ${id} deleted`);
      await refreshSessions();
    } catch (e) { log(`delete failed: ${e.message}`); }
  }

  // ============ canvas charts ============
  function drawStacked(canvasId, availSeries, bannedSeries) {
    const c = $("#" + canvasId);
    const ctx = c.getContext("2d");
    const dpr = window.devicePixelRatio || 1;
    const w = c.clientWidth, h = c.clientHeight;
    c.width = w * dpr; c.height = h * dpr;
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, w, h);

    if (availSeries.length === 0) return;
    const n = availSeries.length;
    const totals = availSeries.map((a, i) => a + bannedSeries[i]);
    const max = Math.max(...totals, 1);
    const barW = w / HISTORY_LEN;

    for (let i = 0; i < n; i++) {
      const total = totals[i];
      const availH = (availSeries[i] / max) * h * 0.9;
      const bannedH = (bannedSeries[i] / max) * h * 0.9;
      const x = i * barW + 1;
      const bw = barW - 2;
      ctx.fillStyle = "#34d399";
      ctx.fillRect(x, h - availH, bw, availH);
      ctx.fillStyle = "#f87171";
      ctx.fillRect(x, h - availH - bannedH, bw, bannedH);
    }

    // Legend
    ctx.font = "11px -apple-system, system-ui";
    ctx.fillStyle = "#34d399"; ctx.fillRect(w - 120, 8, 8, 8);
    ctx.fillStyle = "#7b8394"; ctx.fillText("available", w - 108, 16);
    ctx.fillStyle = "#f87171"; ctx.fillRect(w - 60, 8, 8, 8);
    ctx.fillStyle = "#7b8394"; ctx.fillText("banned", w - 48, 16);
  }

  function drawLine(canvasId, series, color) {
    const c = $("#" + canvasId);
    const ctx = c.getContext("2d");
    const dpr = window.devicePixelRatio || 1;
    const w = c.clientWidth, h = c.clientHeight;
    c.width = w * dpr; c.height = h * dpr;
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, w, h);

    if (series.length < 2) return;
    const max = Math.max(...series, 100);
    const min = Math.min(...series, 0);
    const rng = max - min || 1;
    const step = w / (HISTORY_LEN - 1);

    // Grid
    ctx.strokeStyle = "#242b3a";
    ctx.lineWidth = 1;
    for (let y = 0; y < 4; y++) {
      const yy = (h / 4) * y + 0.5;
      ctx.beginPath();
      ctx.moveTo(0, yy); ctx.lineTo(w, yy);
      ctx.stroke();
    }

    // Area
    ctx.beginPath();
    for (let i = 0; i < series.length; i++) {
      const x = i * step;
      const y = h - ((series[i] - min) / rng) * h * 0.9 - 5;
      if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
    }
    ctx.lineTo((series.length - 1) * step, h);
    ctx.lineTo(0, h);
    ctx.closePath();
    ctx.fillStyle = color + "22";
    ctx.fill();

    // Line
    ctx.beginPath();
    ctx.strokeStyle = color;
    ctx.lineWidth = 2;
    for (let i = 0; i < series.length; i++) {
      const x = i * step;
      const y = h - ((series[i] - min) / rng) * h * 0.9 - 5;
      if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
    }
    ctx.stroke();

    // Last-value label
    ctx.font = "11px -apple-system, system-ui";
    ctx.fillStyle = "#7b8394";
    ctx.textAlign = "right";
    ctx.fillText(Math.round(max) + "ms", w - 4, 12);
    ctx.fillText(Math.round(min) + "ms", w - 4, h - 4);
  }

  // ============ loading ============
  async function refreshHealthStats() {
    try {
      const [h, s] = await Promise.all([
        api("/healthz"),
        api("/api/v1/stats"),
      ]);
      renderHealth(h);
      renderStats(s);
      $("#last-update").textContent = "updated " + new Date().toLocaleTimeString();
    } catch (e) {
      log(`refresh failed: ${e.message}`);
      $("#dot").classList.remove("ok");
    }
  }

  async function refreshProxies() {
    try {
      const params = new URLSearchParams();
      const c = $("#filter-country").value.trim().toUpperCase();
      const p = $("#filter-proto").value;
      const limit = $("#filter-limit").value || 100;
      if (c) params.set("country", c);
      if (p) params.set("protocol", p);
      params.set("limit", limit);
      const status = $("#filter-status").value;
      const data = await api(`/api/v1/proxies?${params}`);
      let list = data.proxies || [];
      if (status === "available") list = list.filter(x => !x.is_banned);
      else if (status === "banned") list = list.filter(x => x.is_banned);
      renderProxies(list);
    } catch (e) { log(`proxies failed: ${e.message}`); }
  }

  async function refreshSessions() {
    try {
      const data = await api("/api/v1/sessions");
      renderSessions(data.sessions || []);
    } catch (e) { log(`sessions failed: ${e.message}`); }
  }

  async function refreshAll() {
    await Promise.all([
      refreshHealthStats(),
      refreshProxies(),
      refreshSessions(),
    ]);
  }

  // ============ events ============
  $$("#tbl-proxies thead th[data-sort]").forEach(th => {
    th.onclick = () => {
      const k = th.dataset.sort;
      if (sortKey === k) sortDir = -sortDir; else { sortKey = k; sortDir = -1; }
      refreshProxies();
    };
  });
  $("#filter-country").oninput = debounce(refreshProxies, 300);
  $("#filter-proto").onchange = refreshProxies;
  $("#filter-status").onchange = refreshProxies;
  $("#filter-limit").onchange = refreshProxies;
  $("#btn-refresh-now").onclick = refreshAll;

  $("#op-refresh").onclick = async () => {
    log("refreshing proxy pool...");
    try {
      const r = await api("/api/v1/refresh", { method: "POST" });
      log(`refresh ok: added=${r.added}, total=${r.total}`);
      refreshAll();
    } catch (e) { log(`refresh failed: ${e.message}`); }
  };
  $("#op-check").onclick = async () => {
    log("requesting health check...");
    try {
      const r = await api("/api/v1/check", { method: "POST" });
      log(`check trigger: ${JSON.stringify(r)}`);
    } catch (e) { log(`check failed: ${e.message}`); }
  };

  function debounce(fn, ms) {
    let t;
    return (...args) => { clearTimeout(t); t = setTimeout(() => fn(...args), ms); };
  }

  // ============ boot ============
  refreshAll();
  setInterval(refreshAll, REFRESH_MS);
})();
