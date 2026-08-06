"use strict";
const $ = (sel) => document.querySelector(sel);

// --- API ---
async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const resp = await fetch(path, opts);
  if (!resp.ok) {
    let msg = resp.statusText;
    try { msg = (await resp.json()).message; } catch { /* kein JSON */ }
    throw new Error(msg);
  }
  if (resp.status === 204) return null;
  return resp.json();
}

let toastTimer;
function toast(msg, info) {
  const el = $("#toast");
  el.textContent = msg;
  el.className = info ? "info" : "";
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.hidden = true; }, 4000);
}

// --- Formatierung ---
const hm = (m) => {
  const neg = m < 0 ? "-" : "";
  m = Math.abs(Math.round(m));
  return `${neg}${Math.floor(m / 60)}:${String(m % 60).padStart(2, "0")}`;
};
const saldoFmt = (m) => (m > 0 ? "+" : "") + hm(m);
const saldoCls = (m) => (m > 0 ? "pos" : m < 0 ? "neg" : "");
const WD = ["So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"];
const dayLabel = (iso) => {
  const d = new Date(iso + "T12:00:00");
  return `${WD[d.getDay()]} ${String(d.getDate()).padStart(2, "0")}.${String(d.getMonth() + 1).padStart(2, "0")}.`;
};
const isoDate = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
const clock = (iso) => new Date(iso).toLocaleTimeString("de-DE", { hour: "2-digit", minute: "2-digit" });
const absLabel = (t, f) => ({ urlaub: "Urlaub", krank: "Krank", feiertag: "Feiertag" }[t] || t) + (f === 0.5 ? " (½)" : "");
const esc = (s) => { const d = document.createElement("span"); d.textContent = s ?? ""; return d.innerHTML; };
const splitProjects = (v) => v.split("+").map((x) => x.trim()).filter(Boolean);
const addDays = (d, n) => { const x = new Date(d); x.setDate(x.getDate() + n); return x; };
const hourOf = (iso) => { const d = new Date(iso); return d.getHours() + d.getMinutes() / 60; };
const fmtH = (h) => { const m = Math.round(h * 60); return `${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`; };
function startOfWeek(d) {
  const m = new Date(d);
  m.setHours(12, 0, 0, 0);
  m.setDate(m.getDate() - ((m.getDay() + 6) % 7));
  return m;
}
function isoWeek(d) {
  const t = new Date(d);
  t.setHours(0, 0, 0, 0);
  t.setDate(t.getDate() + 3 - ((t.getDay() + 6) % 7));
  const w1 = new Date(t.getFullYear(), 0, 4);
  return 1 + Math.round(((t - w1) / 86400000 - 3 + ((w1.getDay() + 6) % 7)) / 7);
}

// --- Projektfarben (stabil per Namens-Hash) ---
const PROJ_COLORS = [
  ["rgba(194,24,60,.12)", "#c2183c", "rgba(194,24,60,.4)"],
  ["rgba(179,84,15,.12)", "#b3540f", "rgba(179,84,15,.4)"],
  ["rgba(38,33,25,.08)", "#5d564a", "rgba(38,33,25,.3)"],
  ["rgba(46,125,79,.12)", "#2e7d4f", "rgba(46,125,79,.4)"],
];
const BREAK_COLOR = ["#eee9df", "#a49c8a", "#d8d2c4"];
const projColor = (p, kind) => kind === "break" ? BREAK_COLOR
  : PROJ_COLORS[[...(p || "")].reduce((a, c) => a + c.charCodeAt(0), 0) % PROJ_COLORS.length];

// --- State ---
const S = {
  status: null,
  monday: startOfWeek(new Date()),
  entries: [],
  weekReport: null,
  view: "zeit",
  sel: new Set(),   // Listen-Mehrfachauswahl (Entry-IDs)
  editId: null,     // Inline-Edit in der Liste
  newRow: false,    // neue Zeile in der Liste
  selBlock: null,   // Zeitstrahl-Auswahl: Entry-ID oder "draft"
  draft: null,      // ungespeicherter Zeitstrahl-Block
  resizing: null,   // {id, f, t} während Kanten-Drag
  absYear: new Date().getFullYear(),
};

// --- Tabs ---
document.querySelectorAll("nav button").forEach((b) => {
  b.onclick = () => {
    document.querySelectorAll("nav button").forEach((x) => x.classList.toggle("active", x === b));
    document.querySelectorAll("main > section").forEach((s) => { s.hidden = s.id !== "tab-" + b.dataset.tab; });
    if (b.dataset.tab === "entries") loadEntries();
    if (b.dataset.tab === "absences") loadAbsences();
    if (b.dataset.tab === "report") loadReport();
  };
});

// --- Status ---
async function refreshStatus() {
  let st;
  try { st = await api("GET", "/api/status"); } catch (e) { $("#status-state").textContent = "Fehler: " + e.message; return; }
  S.status = st;
  const working = st.state === "working", paused = st.state === "paused";
  $("#status-dot").className = "status-dot " + (working ? "on" : paused ? "paused" : "");
  $("#status-state").textContent = working ? "Am Arbeiten" : paused ? "Pause" : "Nichts läuft";
  const chipEl = $("#status-project");
  chipEl.hidden = !st.project;
  chipEl.textContent = st.project || "";
  $("#status-detail").textContent = working || paused
    ? `seit ${clock(st.since)}` + (paused && st.openMinutes < 15 ? " — zählt erst ab 15 Min. als Pause" : "")
    : "";
  $("#btn-start").hidden = working || paused;
  $("#btn-switch").hidden = !working;
  $("#btn-pause").hidden = !working;
  $("#btn-resume").hidden = !paused;
  $("#btn-stop").hidden = !working && !paused;
  $("#hdr-timer").hidden = !working && !paused;
  $("#hdr-timer").classList.toggle("paused", paused);
  $("#hdr-stop").hidden = !working && !paused;
  $("#hdr-pause").hidden = !working;
  tickTimer();

  const t = st.todaySummary;
  $("#today-summary").innerHTML =
    `<span>Ist</span><b class="mono">${hm(t.workedMinutes)}</b>` +
    `<span>Pause</span><span class="mono">${hm(t.breakMinutes)}</span>` +
    `<span>Soll</span><span class="mono">${hm(t.targetMinutes)}</span>` +
    `<span>Differenz</span><b class="mono ${saldoCls(t.diffMinutes)}">${saldoFmt(t.diffMinutes)}</b>` +
    (t.holidayName ? `<span>Feiertag</span><span>${esc(t.holidayName)}</span>` : "") +
    (t.absenceType ? `<span>Abwesenheit</span><span>${esc(absLabel(t.absenceType, t.fraction))}</span>` : "");
  $("#today-progress").style.width = Math.min(100, t.targetMinutes ? t.workedMinutes / t.targetMinutes * 100 : 0) + "%";
  const warns = [...t.warnings];
  if (st.longSession) warns.unshift("Sitzung läuft seit über 12 Stunden — vergessen zu stoppen?");
  $("#status-warnings").innerHTML = warns.map((w) => `<div class="warn">⚠ ${esc(w)}</div>`).join("");
  const saldoEl = $("#saldo");
  saldoEl.textContent = saldoFmt(st.saldoMinutes) + " Std.";
  saldoEl.className = "big mono " + saldoCls(st.saldoMinutes);
}

function tickTimer() {
  const st = S.status;
  const active = st && (st.state === "working" || st.state === "paused");
  let txt = "0:00:00";
  if (active && st.since) {
    const el = Math.max(0, Math.floor((Date.now() - new Date(st.since)) / 1000));
    txt = `${Math.floor(el / 3600)}:${String(Math.floor(el / 60) % 60).padStart(2, "0")}:${String(el % 60).padStart(2, "0")}`;
  }
  $("#status-timer").textContent = txt;
  $("#status-timer").classList.toggle("idle", !active);
  $("#hdr-timer-text").textContent = txt + (st && st.project ? ` · ${st.project}` : "");
}

async function track(action, body) {
  try {
    const res = await api("POST", "/api/tracking/" + action, body);
    if (res && res.warnings) res.warnings.forEach((w) => toast("⚠ " + w));
    refreshStatus();
    loadWeekChart();
    loadProjects();
  } catch (e) { toast(e.message); }
}
$("#btn-start").onclick = () => track("start", { projects: splitProjects($("#start-project").value) });
$("#btn-switch").onclick = () => {
  const p = $("#start-project").value;
  if (!p) { toast("Projektname ins Feld eingeben, dann Wechseln."); return; }
  track("switch", { projects: splitProjects(p) });
};
$("#btn-pause").onclick = () => track("pause");
$("#btn-resume").onclick = () => track("resume");
$("#btn-stop").onclick = () => track("stop");
$("#hdr-pause").onclick = () => track("pause");
$("#hdr-stop").onclick = () => track("stop");

async function loadProjects() {
  try {
    const ps = await api("GET", "/api/projects");
    $("#project-list").innerHTML = ps.map((p) => `<option value="${esc(p.name)}">`).join("");
  } catch { /* unkritisch */ }
}

async function loadWeekChart() {
  const mon = startOfWeek(new Date());
  let rep;
  try { rep = await api("GET", `/api/report?from=${isoDate(mon)}&to=${isoDate(addDays(mon, 6))}`); } catch { return; }
  $("#week-title").textContent = `Diese Woche · KW ${isoWeek(mon)}`;
  const todayIso = isoDate(new Date());
  $("#week-chart").innerHTML = rep.days.slice(0, 5).map((d) => {
    const h = Math.max(d.workedMinutes / (9 * 60) * 100, 3);
    const future = d.date > todayIso;
    return `<div><div class="wc-barbox"><div class="wc-bar${d.workedMinutes ? "" : " empty"}" style="height:${Math.min(h, 100)}%"></div></div>` +
      `<div class="wc-label"><span>${WD[new Date(d.date + "T12:00:00").getDay()]}</span>` +
      `<span class="mono ${future ? "muted-c" : saldoCls(d.diffMinutes)}">${future ? "offen" : saldoFmt(d.diffMinutes)}</span></div></div>`;
  }).join("");
}

// --- Einträge: Laden + gemeinsame Ansichtslogik ---
async function loadEntries() {
  const from = isoDate(S.monday), to = isoDate(addDays(S.monday, 6));
  try {
    [S.entries, S.weekReport] = await Promise.all([
      api("GET", `/api/entries?from=${from}&to=${to}`),
      api("GET", `/api/report?from=${from}&to=${to}`),
    ]);
  } catch (e) { toast(e.message); return; }
  renderEntries();
}

function renderEntries() {
  $("#week-label").textContent = `KW ${isoWeek(S.monday)} · ` +
    `${S.monday.toLocaleDateString("de-DE", { day: "numeric", month: "numeric" })} – ` +
    addDays(S.monday, 6).toLocaleDateString("de-DE", { day: "numeric", month: "numeric", year: "numeric" });
  $("#view-zeit").classList.toggle("active", S.view === "zeit");
  $("#view-liste").classList.toggle("active", S.view === "liste");
  $("#entries-zeit").hidden = S.view !== "zeit";
  $("#entries-liste").hidden = S.view !== "liste";
  if (S.view === "zeit") renderTimeline(); else renderList();
}

$("#view-zeit").onclick = () => { S.view = "zeit"; renderEntries(); };
$("#view-liste").onclick = () => { S.view = "liste"; renderEntries(); };
$("#week-prev").onclick = () => { S.monday = addDays(S.monday, -7); S.draft = null; S.selBlock = null; S.sel.clear(); loadEntries(); };
$("#week-next").onclick = () => { S.monday = addDays(S.monday, 7); S.draft = null; S.selBlock = null; S.sel.clear(); loadEntries(); };

const afterMutation = async () => { await loadEntries(); refreshStatus(); loadWeekChart(); };

// --- Zeitstrahl ---
const H0 = 7, H1 = 19, PXH = 44, TL_H = (H1 - H0) * PXH;

function weekBlocks() {
  const monIso = isoDate(S.monday);
  const now = new Date();
  return S.entries.map((e) => {
    const d = new Date(e.start);
    const day = Math.round((new Date(isoDate(d) + "T12:00:00") - new Date(monIso + "T12:00:00")) / 86400000);
    let f = hourOf(e.start);
    let t = e.open ? now.getHours() + now.getMinutes() / 60 : hourOf(e.end);
    if (!e.open && isoDate(new Date(e.end)) !== isoDate(d)) t = 24; // über Mitternacht: bis Tagesende zeichnen
    if (S.resizing && S.resizing.id === e.id) { f = S.resizing.f; t = S.resizing.t; }
    if (t <= f) t = f + 0.1;
    return { id: e.id, entry: e, day, f, t, kind: e.kind, p: (e.projects || []).join("+"), note: e.note || "", open: e.open };
  }).filter((b) => b.day >= 0 && b.day <= 6);
}

function renderTimeline() {
  const blocks = weekBlocks();
  if (S.draft) blocks.push({ ...S.draft, id: "draft", draft: true });
  const nDays = blocks.some((b) => b.day >= 5) ? 7 : 5;

  const hoursEl = $("#tl-hours");
  hoursEl.innerHTML = "";
  for (let h = H0; h <= H1; h += 2) {
    const el = document.createElement("div");
    el.className = "tl-hour";
    el.style.top = (h - H0) * PXH - 6 + "px";
    el.textContent = `${h}:00`;
    hoursEl.appendChild(el);
  }

  const daysEl = $("#tl-days");
  daysEl.innerHTML = "";
  daysEl.style.gridTemplateColumns = `repeat(${nDays},1fr)`;
  const todayIso = isoDate(new Date());
  for (let i = 0; i < nDays; i++) {
    const dateIso = isoDate(addDays(S.monday, i));
    const wrap = document.createElement("div");
    const head = document.createElement("div");
    head.className = "tl-dayhead" + (dateIso === todayIso ? " today" : "");
    head.textContent = dayLabel(dateIso);
    const col = document.createElement("div");
    col.className = "tl-col";
    col.onpointerdown = startCreate(i, col, blocks);
    for (const b of blocks.filter((x) => x.day === i)) col.appendChild(blockEl(b, col, blocks));
    wrap.append(head, col);
    daysEl.appendChild(wrap);
  }
  renderPanel();
  renderWeekSummary();
}

function blockEl(b, col, blocks) {
  const [bg, fg, bd] = projColor(b.p, b.kind);
  const isSel = S.selBlock === b.id;
  const el = document.createElement("div");
  el.className = "tl-block" + (isSel ? " sel" : "") + (b.open ? " live" : "") + (b.draft ? " draft" : "");
  const top = Math.max(0, (b.f - H0) * PXH);
  const bot = Math.min(TL_H, (b.t - H0) * PXH);
  el.style.top = top + "px";
  el.style.height = Math.max(bot - top - 3, 12) + "px";
  el.style.background = bg;
  el.style.color = fg;
  el.style.borderColor = isSel ? fg : bd;
  el.innerHTML = `<div class="tl-bl">${esc(b.kind === "break" ? "Pause" : b.p || "—")}</div>` +
    `<div class="tl-bt">${fmtH(b.f)}–${b.open ? "…läuft" : fmtH(b.t)}</div>`;
  el.onclick = (e) => { e.stopPropagation(); S.selBlock = b.id; renderTimeline(); };
  if (!b.draft) {
    const ht = document.createElement("div");
    ht.className = "tl-h tl-ht";
    ht.onpointerdown = startResize(b, "t", col, blocks);
    el.appendChild(ht);
    if (!b.open) {
      const hb = document.createElement("div");
      hb.className = "tl-h tl-hb";
      hb.onpointerdown = startResize(b, "b", col, blocks);
      el.appendChild(hb);
    }
  }
  return el;
}

// Ziehen auf freier Fläche: neuen Draft-Block aufziehen (5-Min-Raster, begrenzt durch Nachbarn)
function startCreate(dayIdx, col, blocks) {
  return (e) => {
    if (e.target !== col || e.button !== 0) return;
    e.preventDefault();
    const rect = col.getBoundingClientRect();
    const scale = rect.height / TL_H || 1;
    const snap = (y) => Math.round((H0 + (y - rect.top) / (PXH * scale)) * 12) / 12;
    const h0 = Math.min(Math.max(H0, snap(e.clientY)), H1 - 0.25);
    const others = blocks.filter((b) => b.day === dayIdx && !b.draft);
    if (others.some((b) => h0 > b.f && h0 < b.t)) return;
    const prevEnd = Math.max(H0, ...others.filter((b) => b.t <= h0 + 0.001).map((b) => b.t));
    const nextStart = Math.min(H1, ...others.filter((b) => b.f >= h0 - 0.001).map((b) => b.f));
    if (nextStart - prevEnd < 0.25) return;
    S.draft = { day: dayIdx, f: h0, t: Math.min(h0 + 0.25, nextStart), kind: "work", p: "", note: "" };
    S.selBlock = "draft";
    const move = (ev) => {
      const h = snap(ev.clientY);
      if (h >= h0) { S.draft.f = h0; S.draft.t = Math.min(Math.max(h, h0 + 0.25), nextStart); }
      else { S.draft.f = Math.max(Math.min(h, h0 - 0.25), prevEnd); S.draft.t = h0; }
      renderTimeline();
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      $("#ep-project").focus();
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    renderTimeline();
  };
}

// Kante ziehen: Zeit ändern, bei Loslassen PUT
function startResize(b, edge, col, blocks) {
  return (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    const rect = col.getBoundingClientRect();
    const scale = rect.height / TL_H || 1;
    const y0 = e.clientY, f0 = b.f, t0 = b.t;
    const others = blocks.filter((x) => x.day === b.day && x.id !== b.id && !x.draft);
    const prevEnd = Math.max(H0, ...others.filter((x) => x.t <= f0 + 0.001).map((x) => x.t));
    const nextStart = Math.min(H1, ...others.filter((x) => x.f >= t0 - 0.001).map((x) => x.f));
    let moved = false;
    S.selBlock = b.id;
    const move = (ev) => {
      const dh = Math.round((ev.clientY - y0) / (PXH * scale) * 12) / 12;
      if (!dh && !moved) return;
      moved = true;
      const r = { id: b.id, f: f0, t: t0 };
      if (edge === "t") r.f = Math.min(Math.max(prevEnd, f0 + dh), t0 - 0.25);
      else r.t = Math.max(Math.min(nextStart, t0 + dh), f0 + 0.25);
      S.resizing = r;
      renderTimeline();
    };
    const up = async () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      const r = S.resizing;
      S.resizing = null;
      if (!r || !moved) { renderTimeline(); return; }
      const dateIso = isoDate(addDays(S.monday, b.day));
      const patch = edge === "t"
        ? { start: new Date(`${dateIso}T${fmtH(r.f)}:00`).toISOString() }
        : { end: new Date(`${dateIso}T${fmtH(r.t)}:00`).toISOString() };
      try { await api("PUT", "/api/entries/" + b.id, patch); } catch (err) { toast(err.message); }
      afterMutation();
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    renderTimeline();
  };
}

function selectedBlock() {
  if (S.selBlock === "draft" && S.draft) return { ...S.draft, id: "draft", draft: true };
  return weekBlocks().find((b) => b.id === S.selBlock) || null;
}

function renderPanel() {
  const b = selectedBlock();
  $("#ep-empty").hidden = !!b;
  $("#ep-form").hidden = !b;
  if (!b) return;
  const dateIso = isoDate(addDays(S.monday, b.day));
  const [bg, fg] = projColor(b.p, b.kind);
  $("#ep-head").innerHTML = `<b>${dayLabel(dateIso)}</b> · ` +
    `<span class="chip" style="background:${bg};color:${fg}">${esc(b.kind === "break" ? "Pause" : b.p || "neu")}</span>` +
    (b.open ? ' · <span class="muted-c">läuft</span>' : "");
  $("#ep-from").value = b.entry ? clock(b.entry.start) : fmtH(b.f);
  $("#ep-to").value = b.entry ? (b.open ? "" : clock(b.entry.end)) : fmtH(b.t);
  $("#ep-kind").value = b.kind;
  $("#ep-project").value = b.p;
  $("#ep-note").value = b.note;
  $("#ep-dup").hidden = $("#ep-del").hidden = !!b.draft;
}

$("#ep-save").onclick = async () => {
  const b = selectedBlock();
  if (!b) return;
  const dateIso = isoDate(addDays(S.monday, b.day));
  const fromV = $("#ep-from").value, toV = $("#ep-to").value;
  if (!fromV) { toast("Von-Zeit fehlt."); return; }
  const body = {
    kind: $("#ep-kind").value,
    projects: splitProjects($("#ep-project").value),
    note: $("#ep-note").value,
    start: new Date(`${dateIso}T${fromV}:00`).toISOString(),
  };
  if (toV) {
    body.end = new Date(`${dateIso}T${toV}:00`).toISOString();
    if (body.end <= body.start) body.end = new Date(new Date(body.end).getTime() + 86400000).toISOString();
  } else if (!b.open) { toast("Bis-Zeit fehlt."); return; }
  try {
    if (b.draft) {
      const res = await api("POST", "/api/entries", body);
      S.draft = null;
      S.selBlock = res.id;
    } else {
      await api("PUT", "/api/entries/" + b.id, body);
    }
    afterMutation();
  } catch (e) { toast(e.message); }
};

async function dupEntry(e) {
  if (e.open) { toast("Laufender Eintrag lässt sich nicht duplizieren."); return; }
  const dur = new Date(e.end) - new Date(e.start);
  try {
    const res = await api("POST", "/api/entries", {
      kind: e.kind, projects: e.projects || [], note: e.note || "",
      start: e.end, end: new Date(new Date(e.end).getTime() + dur).toISOString(),
    });
    S.selBlock = res.id;
    afterMutation();
  } catch (err) { toast(err.message); }
}

$("#ep-dup").onclick = () => {
  const b = selectedBlock();
  if (b && b.entry) dupEntry(b.entry);
};
$("#ep-del").onclick = async () => {
  const b = selectedBlock();
  if (!b || b.draft) return;
  if (!confirm("Eintrag löschen?")) return;
  try { await api("DELETE", "/api/entries/" + b.id); S.selBlock = null; afterMutation(); }
  catch (e) { toast(e.message); }
};

function renderWeekSummary() {
  const r = S.weekReport;
  if (!r) return;
  $("#week-summary").innerHTML =
    `<span>Woche Ist</span><b class="mono">${hm(r.totalWorkedMinutes)}</b>` +
    `<span>Soll</span><span class="mono">${hm(r.totalTargetMinutes)}</span>` +
    `<span>Saldo Zeitraum</span><b class="mono ${saldoCls(r.saldoMinutes)}">${saldoFmt(r.saldoMinutes)}</b>`;
}

// --- Neuer Eintrag (beide Ansichten) ---
$("#entry-new").onclick = () => {
  if (S.view === "liste") { S.newRow = true; S.editId = null; renderList(); return; }
  const todayIso = isoDate(new Date());
  let day = Math.round((new Date(todayIso + "T12:00:00") - new Date(isoDate(S.monday) + "T12:00:00")) / 86400000);
  if (day < 0 || day > 6) day = 0;
  const others = weekBlocks().filter((b) => b.day === day);
  let f = others.length ? Math.max(...others.map((b) => b.t)) : 9;
  f = Math.min(Math.max(H0, Math.round(f * 12) / 12), H1 - 1);
  S.draft = { day, f, t: Math.min(f + 1, H1), kind: "work", p: "", note: "" };
  S.selBlock = "draft";
  renderTimeline();
  $("#ep-project").focus();
};

// --- Listen-Ansicht ---
function renderList() {
  const n = S.sel.size;
  $("#bulk-bar").hidden = n === 0;
  $("#bulk-count").textContent = n + " ausgewählt";

  const repByDate = Object.fromEntries(((S.weekReport || {}).days || []).map((d) => [d.date, d]));
  const wrap = $("#entries-list");
  wrap.innerHTML = "";
  const head = document.createElement("div");
  head.className = "lr lr-head";
  head.innerHTML = "<span></span><span>Zeit</span><span>Dauer</span><span>Typ</span><span>Projekt</span><span>Notiz</span><span></span>";
  wrap.appendChild(head);
  if (S.newRow) wrap.appendChild(editRowEl(null));
  let lastDate = "";
  const todayIso = isoDate(new Date());
  for (const e of S.entries) {
    const dIso = isoDate(new Date(e.start));
    if (dIso !== lastDate) {
      lastDate = dIso;
      const r = repByDate[dIso];
      const gh = document.createElement("div");
      gh.className = "lr-group";
      gh.innerHTML = `<b>${dayLabel(dIso)}${dIso === todayIso ? " · heute" : ""}</b>` +
        (r ? ` · Ist ${hm(r.workedMinutes)} · Pause ${hm(r.breakMinutes)} · <span class="${saldoCls(r.diffMinutes)}">${saldoFmt(r.diffMinutes)}</span>` : "");
      wrap.appendChild(gh);
    }
    wrap.appendChild(S.editId === e.id ? editRowEl(e) : rowEl(e));
  }
  if (!S.entries.length && !S.newRow) {
    wrap.insertAdjacentHTML("beforeend", '<p class="muted-c pad">Keine Einträge in dieser Woche.</p>');
  }
}

function rowEl(e) {
  const el = document.createElement("div");
  el.className = "lr";
  const endTxt = e.open ? "…läuft" : clock(e.end);
  const dur = e.open ? "" : hm((new Date(e.end) - new Date(e.start)) / 60000);
  const isBreak = e.kind === "break";
  el.innerHTML =
    `<input type="checkbox"${S.sel.has(e.id) ? " checked" : ""}>` +
    `<span class="mono">${clock(e.start)}–${endTxt}</span>` +
    `<span class="mono muted-c">${dur}</span>` +
    `<span><span class="chip ${isBreak ? "chip-break" : "chip-work"}">${isBreak ? "Pause" : "Arbeit"}</span></span>` +
    `<span>${esc((e.projects || []).join("+"))}</span>` +
    `<span class="muted-c ellip">${esc(e.note || "")}</span>` +
    `<span class="lr-actions"><button class="rowbtn" data-a="edit">Bearbeiten</button>` +
    `<button class="rowbtn" data-a="dup" title="Duplizieren">⧉</button>` +
    `<button class="rowbtn danger" data-a="del" title="Löschen">×</button></span>`;
  el.querySelector("input").onchange = (ev) => { ev.target.checked ? S.sel.add(e.id) : S.sel.delete(e.id); renderList(); };
  el.querySelector('[data-a="edit"]').onclick = () => { S.editId = e.id; S.newRow = false; renderList(); };
  el.querySelector('[data-a="dup"]').onclick = () => dupEntry(e);
  el.querySelector('[data-a="del"]').onclick = async () => {
    if (!confirm("Eintrag löschen?")) return;
    try { await api("DELETE", "/api/entries/" + e.id); afterMutation(); }
    catch (err) { toast(err.message); }
  };
  return el;
}

function editRowEl(e) {
  const el = document.createElement("div");
  el.className = "lr-edit";
  el.innerHTML =
    '<input type="date" class="ee-date">' +
    '<input type="time" class="ee-from">' +
    '<span class="muted-c">–</span>' +
    '<input type="time" class="ee-to">' +
    '<select class="ee-kind"><option value="work">Arbeit</option><option value="break">Pause</option></select>' +
    '<input class="ee-project" list="project-list" placeholder="Projekt(e)">' +
    '<input class="ee-note" placeholder="Notiz">' +
    '<button class="primary ee-save">Speichern</button><button class="btn ee-cancel">Abbrechen</button>';
  const q = (c) => el.querySelector(c);
  q(".ee-date").value = e ? isoDate(new Date(e.start)) : isoDate(new Date());
  q(".ee-from").value = e ? clock(e.start) : "";
  q(".ee-to").value = e && !e.open ? clock(e.end) : "";
  q(".ee-kind").value = e ? e.kind : "work";
  q(".ee-project").value = e ? (e.projects || []).join("+") : "";
  q(".ee-note").value = e ? e.note || "" : "";
  q(".ee-cancel").onclick = () => { S.editId = null; S.newRow = false; renderList(); };
  q(".ee-save").onclick = async () => {
    if (!q(".ee-from").value) { toast("Von-Zeit fehlt."); return; }
    const date = q(".ee-date").value;
    const body = {
      kind: q(".ee-kind").value,
      projects: splitProjects(q(".ee-project").value),
      note: q(".ee-note").value,
      start: new Date(`${date}T${q(".ee-from").value}:00`).toISOString(),
    };
    if (q(".ee-to").value) {
      body.end = new Date(`${date}T${q(".ee-to").value}:00`).toISOString();
      if (body.end <= body.start) body.end = new Date(new Date(body.end).getTime() + 86400000).toISOString();
    } else if (!e || !e.open) { toast("Bis-Zeit fehlt."); return; }
    try {
      if (e) await api("PUT", "/api/entries/" + e.id, body);
      else await api("POST", "/api/entries", body);
      S.editId = null;
      S.newRow = false;
      afterMutation();
    } catch (err) { toast(err.message); }
  };
  return el;
}

// --- Bulk-Aktionen ---
$("#bulk-clear").onclick = () => { S.sel.clear(); renderList(); };
async function bulkPatch(patch) {
  try {
    for (const id of S.sel) await api("PUT", "/api/entries/" + id, patch);
    S.sel.clear();
    afterMutation();
  } catch (e) { toast(e.message); loadEntries(); }
}
$("#bulk-project").onclick = () => {
  const v = prompt("Neue(s) Projekt(e), z.B. acme+intern");
  if (v === null) return;
  bulkPatch({ projects: splitProjects(v) });
};
$("#bulk-kind").onclick = () => {
  const v = prompt('Neuer Typ: "arbeit" oder "pause"', "arbeit");
  if (v === null) return;
  bulkPatch({ kind: /^p/i.test(v.trim()) ? "break" : "work" });
};
$("#bulk-del").onclick = async () => {
  if (!confirm(`${S.sel.size} Einträge löschen?`)) return;
  try { for (const id of S.sel) await api("DELETE", "/api/entries/" + id); }
  catch (e) { toast(e.message); }
  S.sel.clear();
  afterMutation();
};

// --- Abwesenheiten ---
async function loadAbsences() {
  let list;
  try { list = await api("GET", "/api/absences?year=" + S.absYear); }
  catch (e) { toast(e.message); return; }
  $("#abs-year-label").textContent = S.absYear;
  const tbody = $("#absences-table tbody");
  tbody.innerHTML = "";
  const todayIso = isoDate(new Date());
  let taken = 0, planned = 0;
  for (const a of list) {
    if (a.type === "urlaub") { if (a.date > todayIso) planned += a.fraction; else taken += a.fraction; }
    const chipCls = a.type === "urlaub" ? "chip-vac" : a.type === "krank" ? "chip-sick" : "chip-hol";
    const tr = document.createElement("tr");
    tr.innerHTML = `<td class="mono">${dayLabel(a.date)} ${a.date}</td>` +
      `<td><span class="chip ${chipCls}">${absLabel(a.type, a.fraction)}</span></td>` +
      `<td class="muted-c">${esc(a.note)}</td><td><button class="rowbtn danger" title="Löschen">×</button></td>`;
    tr.querySelector("button").onclick = async () => {
      try { await api("DELETE", "/api/absences/" + a.id); loadAbsences(); refreshStatus(); }
      catch (err) { toast(err.message); }
    };
    tbody.appendChild(tr);
  }
  $("#vacation-count").innerHTML = `<b class="mono">${taken}</b> Tage genommen · ${planned} geplant`;
}
$("#abs-prev").onclick = () => { S.absYear--; loadAbsences(); };
$("#abs-next").onclick = () => { S.absYear++; loadAbsences(); };
$("#absence-form").onsubmit = async (ev) => {
  ev.preventDefault();
  const body = {
    type: $("#absence-type").value,
    from: $("#absence-from").value,
    half: $("#absence-half").checked,
  };
  if ($("#absence-to").value) body.to = $("#absence-to").value;
  try {
    const res = await api("POST", "/api/absences", body);
    toast(`${res.added.length} Tag(e) eingetragen.`, true);
    loadAbsences();
    refreshStatus();
  } catch (e) { toast(e.message); }
};

// --- Bericht ---
function weekRange() {
  const mon = startOfWeek(new Date());
  return [isoDate(mon), isoDate(addDays(mon, 6))];
}
function setRange(kind) {
  const now = new Date();
  let from, to;
  if (kind === "week") [from, to] = weekRange();
  else if (kind === "month") {
    from = isoDate(new Date(now.getFullYear(), now.getMonth(), 1));
    to = isoDate(new Date(now.getFullYear(), now.getMonth() + 1, 0));
  } else {
    from = `${now.getFullYear()}-01-01`;
    to = `${now.getFullYear()}-12-31`;
  }
  $("#report-from").value = from;
  $("#report-to").value = to;
  document.querySelectorAll("[data-range]").forEach((b) => b.classList.toggle("active", b.dataset.range === kind));
  loadReport();
}
document.querySelectorAll("[data-range]").forEach((b) => { b.onclick = () => setRange(b.dataset.range); });
$("#report-reload").onclick = loadReport;

async function loadReport() {
  if (!$("#report-from").value) {
    const [f, t] = weekRange();
    $("#report-from").value = f;
    $("#report-to").value = t;
    $('[data-range="week"]').classList.add("active");
  }
  let rep;
  try { rep = await api("GET", `/api/report?from=${$("#report-from").value}&to=${$("#report-to").value}`); }
  catch (e) { toast(e.message); return; }
  $("#report-totals").innerHTML = [
    ["Ist", hm(rep.totalWorkedMinutes), ""],
    ["Soll", hm(rep.totalTargetMinutes), ""],
    ["Gutschrift", hm(rep.totalCreditMinutes), ""],
    ["Saldo Zeitraum", saldoFmt(rep.saldoMinutes), saldoCls(rep.saldoMinutes)],
  ].map(([l, v, c]) => `<div class="stat"><div class="stat-l">${l}</div><div class="stat-v mono ${c}">${v}</div></div>`).join("");
  const tbody = $("#report-table tbody");
  tbody.innerHTML = "";
  for (const d of rep.days) {
    if (!d.workedMinutes && !d.targetMinutes && !d.absenceType && !d.holidayName) continue;
    const extra = [d.holidayName, d.absenceType && absLabel(d.absenceType, d.fraction)].filter(Boolean).join(" · ");
    const tr = document.createElement("tr");
    tr.innerHTML =
      `<td>${dayLabel(d.date)}</td><td class="mono">${hm(d.workedMinutes)}</td>` +
      `<td class="mono muted-c">${hm(d.breakMinutes)}</td><td class="mono muted-c">${hm(d.targetMinutes)}</td>` +
      `<td class="mono ${saldoCls(d.diffMinutes)}">${saldoFmt(d.diffMinutes)}</td>` +
      `<td class="muted-c">${esc(extra)}${d.warnings.length ? " ⚠ " + esc(d.warnings.join(" · ")) : ""}</td>`;
    tbody.appendChild(tr);
  }
  const projects = [...rep.projects].sort((a, b) => b.percent - a.percent);
  $("#report-projects").innerHTML = projects.map((p) =>
    `<div class="bar-row"><span class="ellip">${esc(p.name)}</span>` +
    `<span class="mono muted-c">${p.percent.toFixed(1)}%</span><span class="mono">${hm(p.minutes)}</span>` +
    `<div class="bar-track"><div class="bar" style="width:${Math.max(p.percent, 1)}%"></div></div></div>`
  ).join("") || '<p class="muted-c">Keine Projektzeiten im Zeitraum.</p>';
}

// --- Init ---
loadProjects();
refreshStatus();
loadWeekChart();
setInterval(refreshStatus, 2000);
setInterval(tickTimer, 1000);
