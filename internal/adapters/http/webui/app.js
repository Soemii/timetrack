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

// --- Projektfarben: im Projekte-Tab vergeben (API), ohne Zuweisung stabiler Namens-Hash auf die Palette ---
const PALETTE = ["#c2183c", "#b3540f", "#2e7d4f", "#1f6f8b", "#7b4bb7", "#a3245f", "#5d564a"];
const colorOf = (name) => {
  const p = S.projects.find((x) => x.name.toLowerCase() === (name || "").toLowerCase());
  return (p && p.color) || PALETTE[[...(name || "")].reduce((a, c) => a + c.charCodeAt(0), 0) % PALETTE.length];
};
const tint = (c, pct) => `color-mix(in srgb, ${c} ${pct}%, transparent)`;
const projChip = (name) => {
  const c = colorOf(name);
  return `<span class="chip" style="background:${tint(c, 12)};color:${c}">${esc(name)}</span>`;
};

// --- Theme: ohne Wahl folgt CSS dem System; Toggle setzt data-theme + localStorage,
// zurück auf "auto" sobald die Wahl wieder der System-Präferenz entspricht ---
const sysDark = matchMedia("(prefers-color-scheme: dark)");
const effTheme = () => document.documentElement.dataset.theme || (sysDark.matches ? "dark" : "light");
function themeIcon() { $("#theme-toggle").textContent = effTheme() === "dark" ? "☀" : "🌙"; }
$("#theme-toggle").onclick = () => {
  const next = effTheme() === "dark" ? "light" : "dark";
  if ((next === "dark") === sysDark.matches) {
    delete document.documentElement.dataset.theme;
    localStorage.removeItem("theme");
  } else {
    document.documentElement.dataset.theme = next;
    localStorage.setItem("theme", next);
  }
  themeIcon();
};
sysDark.onchange = themeIcon;
themeIcon();

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
  projects: [],     // Projektliste inkl. Farbe/Notiz/Unternehmen (Projekte-Tab)
  companies: [],    // Unternehmen (Projekte-Tab, Bericht)
  ep: null,         // Panel-Edit-Zustand: {forSel, projects, kind, extras, taskId}
  tasksByProject: {}, // Cache: projectId → unarchivierte Aufgaben
  openTasks: new Set(), // Projekte-Tab: aufgeklappte Aufgabenlisten (Projekt-IDs)
};

// --- Aufgaben ---
const taskLabel = (t) => (t.jiraKey ? t.jiraKey + " " + t.title : t.title);
async function loadTasks(pid) {
  if (!S.tasksByProject[pid]) S.tasksByProject[pid] = await api("GET", `/api/projects/${pid}/tasks`);
  return S.tasksByProject[pid];
}

// --- Tabs ---
document.querySelectorAll("nav button").forEach((b) => {
  b.onclick = () => {
    document.querySelectorAll("nav button").forEach((x) => x.classList.toggle("active", x === b));
    document.querySelectorAll("main > section").forEach((s) => { s.hidden = s.id !== "tab-" + b.dataset.tab; });
    if (b.dataset.tab === "entries") loadEntries();
    if (b.dataset.tab === "absences") loadAbsences();
    if (b.dataset.tab === "report") loadReport();
    if (b.dataset.tab === "projects") loadProjectsTab();
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
  const stc = colorOf(splitProjects(st.project || "")[0] || "");
  chipEl.style.background = tint(stc, 12);
  chipEl.style.color = stc;
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
  $("#hdr-resume").hidden = !paused;
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
$("#hdr-resume").onclick = () => track("resume");
$("#hdr-stop").onclick = () => track("stop");

async function loadProjects() {
  try {
    S.projects = await api("GET", "/api/projects");
    $("#project-list").innerHTML = S.projects.map((p) => `<option value="${esc(p.name)}">`).join("");
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
  S.tasksByProject = {}; // JIRA-Sync kann neue Aufgaben gebracht haben
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
// Dynamischer Stundenbereich: mindestens 7–19, erweitert sich um die Einträge
// der Woche (plus 1 h Zieh-Reserve am Rand). H0/H1 setzt renderTimeline().
const PXH = 44;
let H0 = 7, H1 = 19, TL_H = (H1 - H0) * PXH;

// Ein Entry ergibt pro berührtem Kalendertag ein Segment; über Mitternacht
// laufende Einträge erscheinen so an Tag A (bis 24:00) und Tag B (ab 0:00).
function weekBlocks() {
  const monIso = isoDate(S.monday);
  const now = new Date();
  const segs = [];
  for (const e of S.entries) {
    const start = new Date(e.start);
    const end = e.open ? now : new Date(e.end);
    for (const d = new Date(start); isoDate(d) <= isoDate(end); d.setDate(d.getDate() + 1)) {
      const day = Math.round((new Date(isoDate(d) + "T12:00:00") - new Date(monIso + "T12:00:00")) / 86400000);
      const first = isoDate(d) === isoDate(start), last = isoDate(d) === isoDate(end);
      if (day < 0 || day > 6) continue;
      let f = first ? start.getHours() + start.getMinutes() / 60 : 0;
      let t = last ? end.getHours() + end.getMinutes() / 60 : 24;
      if (!first && last && t <= 0) { // endet exakt 0:00 → kein Sliver am Folgetag
        const prev = segs[segs.length - 1];
        if (prev && prev.id === e.id) prev.contBottom = false;
        continue;
      }
      if (S.resizing && S.resizing.id === e.id && S.resizing.day === day) { f = S.resizing.f; t = S.resizing.t; }
      if (t <= f) t = f + 0.1;
      segs.push({ id: e.id, entry: e, day, f, t, kind: e.kind, p: (e.projects || []).join("+"),
        note: e.note || "", open: e.open && last, contTop: !first, contBottom: !last });
    }
  }
  return segs;
}

function renderTimeline() {
  const blocks = weekBlocks();
  if (S.draft) blocks.push({ ...S.draft, id: "draft", draft: true });
  const nDays = blocks.some((b) => b.day >= 5) ? 7 : 5;

  const minF = Math.min(...blocks.map((b) => b.f));
  const maxT = Math.max(...blocks.map((b) => b.t));
  H0 = minF < 7 ? Math.max(0, Math.floor(minF) - 1) : 7;
  H1 = maxT > 19 ? Math.min(24, Math.ceil(maxT) + 1) : 19;
  TL_H = (H1 - H0) * PXH;

  const headsEl = $("#tl-heads");
  const scrollEl = $("#tl-scroll");
  const prevScroll = scrollEl.dataset.init ? scrollEl.scrollTop : 0;
  headsEl.innerHTML = "";
  scrollEl.innerHTML = "";
  headsEl.style.gridTemplateColumns = scrollEl.style.gridTemplateColumns = `44px repeat(${nDays},1fr)`;
  headsEl.appendChild(document.createElement("div"));

  const hoursEl = document.createElement("div");
  hoursEl.className = "tl-hours";
  hoursEl.style.height = TL_H + "px";
  for (let h = H0; h <= H1; h += 2) {
    const el = document.createElement("div");
    el.className = "tl-hour";
    el.style.top = Math.max(0, (h - H0) * PXH - 6) + "px";
    el.textContent = `${h}:00`;
    hoursEl.appendChild(el);
  }
  scrollEl.appendChild(hoursEl);

  const todayIso = isoDate(new Date());
  for (let i = 0; i < nDays; i++) {
    const dateIso = isoDate(addDays(S.monday, i));
    const head = document.createElement("div");
    head.className = "tl-dayhead" + (dateIso === todayIso ? " today" : "");
    head.textContent = dayLabel(dateIso);
    headsEl.appendChild(head);
    const col = document.createElement("div");
    col.className = "tl-col";
    col.style.height = TL_H + "px";
    col.onpointerdown = startCreate(i, col, blocks);
    for (const b of blocks.filter((x) => x.day === i)) col.appendChild(blockEl(b, col, blocks));
    scrollEl.appendChild(col);
  }
  scrollEl.dataset.init = "1";
  scrollEl.scrollTop = prevScroll;
  renderPanel();
  renderWeekSummary();
}

function blockEl(b, col, blocks) {
  const isSel = S.selBlock === b.id;
  const live = b.entry ? b.entry.open : false;
  const el = document.createElement("div");
  el.className = "tl-block" + (b.kind === "break" ? " pc-break" : "") +
    (isSel ? " sel" : "") + (live ? " live" : "") + (b.draft ? " draft" : "");
  el.style.top = (b.f - H0) * PXH + "px";
  el.style.height = Math.max((b.t - b.f) * PXH - 3, 12) + "px";
  let dots = "";
  if (b.kind !== "break") {
    const cols = (b.p ? b.p.split("+") : [""]).map(colorOf);
    el.style.color = cols[0];
    el.style.borderColor = isSel ? cols[0] : tint(cols[0], 40);
    if (cols.length > 1) {
      // Harte Farbstops: jeder Projektanteil als eigener Streifen
      const n = cols.length;
      el.style.backgroundImage = `linear-gradient(135deg,${cols.map((c, j) => `${tint(c, 12)} ${j / n * 100}% ${(j + 1) / n * 100}%`).join(",")})`;
      dots = cols.map((c) => `<span class="pdot" style="background:${c}"></span>`).join("");
    } else {
      el.style.backgroundColor = tint(cols[0], 12);
    }
  }
  const time = b.entry
    ? `${b.contTop ? "↥ " : ""}${clock(b.entry.start)}–${live ? "…läuft" : clock(b.entry.end)}${b.contBottom ? " ↧" : ""}`
    : `${fmtH(b.f)}–${fmtH(b.t)}`;
  el.innerHTML = `<div class="tl-bl">${dots}${esc(b.kind === "break" ? "Pause" : b.p || "—")}</div>` +
    `<div class="tl-bt">${time}</div>`;
  el.onclick = (e) => { e.stopPropagation(); S.selBlock = b.id; renderTimeline(); };
  if (!b.draft) {
    if (!b.contTop) {
      const ht = document.createElement("div");
      ht.className = "tl-h tl-ht";
      ht.onpointerdown = startResize(b, "t", col, blocks);
      el.appendChild(ht);
    }
    if (!b.open && !b.contBottom) {
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
      $("#ep-note").focus();
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
      const r = { id: b.id, day: b.day, f: f0, t: t0 };
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

// Panel-Edit-Zustand neu vom Block übernehmen (bei Auswahlwechsel)
function epInit(b) {
  const parts = b.kind === "break" ? [] : (b.p ? b.p.split("+") : []);
  const known = S.projects.filter((p) => !p.archived).map((p) => p.name);
  S.ep = {
    forSel: S.selBlock,
    kind: b.kind,
    projects: parts,
    orig: (b.entry ? b.entry.projects || [] : parts).slice(),
    // Projekte des Blocks, die nicht (mehr) in der Projektliste stehen — bleiben wählbar
    extras: parts.filter((x) => !known.some((k) => k.toLowerCase() === x.toLowerCase())),
    taskId: b.entry ? b.entry.taskId || 0 : 0,
    // Label der gespeicherten Aufgabe — hält archivierte Aufgaben wählbar
    taskFallback: b.entry && b.entry.taskId ? { id: b.entry.taskId, label: b.entry.task || "#" + b.entry.taskId } : null,
  };
  $("#ep-week").checked = false;
  $("#ep-note").value = b.note;
}

// Chip-Änderung anwenden: Draft färbt sofort im Zeitstrahl, sonst nur Panel
function epSync() {
  if (S.selBlock === "draft" && S.draft) {
    S.draft.kind = S.ep.kind;
    S.draft.p = S.ep.projects.join("+");
    renderTimeline();
  } else {
    renderPanel();
  }
}

function renderChips() {
  const names = S.projects.filter((p) => !p.archived).map((p) => p.name);
  for (const x of S.ep.extras) {
    if (!names.some((n) => n.toLowerCase() === x.toLowerCase())) names.push(x);
  }
  const wrap = $("#ep-chips");
  wrap.innerHTML = "";
  for (const name of names) {
    const on = S.ep.kind !== "break" && S.ep.projects.some((p) => p.toLowerCase() === name.toLowerCase());
    const c = colorOf(name);
    const btn = document.createElement("button");
    btn.className = "pchip";
    btn.textContent = name;
    if (on) { btn.style.borderColor = c; btn.style.background = tint(c, 12); btn.style.color = c; }
    btn.onclick = () => {
      S.ep.projects = on
        ? S.ep.projects.filter((p) => p.toLowerCase() !== name.toLowerCase())
        : [...S.ep.projects, name];
      S.ep.kind = "work";
      epSync();
    };
    wrap.appendChild(btn);
  }
  const pause = document.createElement("button");
  pause.className = "pchip" + (S.ep.kind === "break" ? " pc-break" : "");
  pause.textContent = "Pause";
  pause.onclick = () => { S.ep.kind = "break"; S.ep.projects = []; epSync(); };
  wrap.appendChild(pause);
}

// Aufgaben-Auswahl: Aufgaben der gewählten Projekte, "keine" als Default.
// Wird das Projekt der gewählten Aufgabe abgewählt, springt sie auf 0 —
// spiegelt die Server-Validierung, statt sie beim Speichern auszulösen.
async function renderTaskSelect() {
  const sel = $("#ep-task"), ep = S.ep;
  if (!ep || ep.kind === "break" || !ep.projects.length) {
    sel.hidden = true;
    if (ep) ep.taskId = 0;
    return;
  }
  const pids = ep.projects
    .map((n) => (S.projects.find((p) => p.name.toLowerCase() === n.toLowerCase()) || {}).id)
    .filter(Boolean);
  let tasks = [];
  try { for (const pid of pids) tasks = tasks.concat(await loadTasks(pid)); } catch { /* unkritisch */ }
  if (S.ep !== ep) return; // Auswahl hat inzwischen gewechselt
  if (ep.taskId && !tasks.some((t) => t.id === ep.taskId)) {
    const key = (a) => a.map((x) => x.toLowerCase()).sort().join("+");
    if (ep.taskFallback && key(ep.projects) === key(ep.orig)) {
      tasks = tasks.concat({ id: ep.taskFallback.id, title: ep.taskFallback.label, jiraKey: "" });
    } else {
      ep.taskId = 0;
    }
  }
  sel.hidden = !tasks.length;
  if (!tasks.length) return;
  sel.innerHTML = '<option value="0">– keine Aufgabe –</option>' +
    tasks.map((t) => `<option value="${t.id}"${t.id === ep.taskId ? " selected" : ""}>${esc(taskLabel(t))}</option>`).join("");
  sel.onchange = () => { ep.taskId = Number(sel.value); };
}

function renderPanel() {
  const b = selectedBlock();
  $("#ep-empty").hidden = !!b;
  $("#ep-form").hidden = !b;
  if (!b) { S.ep = null; return; }
  if (!S.ep || S.ep.forSel !== S.selBlock) epInit(b);
  const dateIso = isoDate(addDays(S.monday, b.day));
  const live = b.entry ? b.entry.open : false;
  const hc = colorOf(S.ep.projects[0] || "");
  const head = S.ep.kind === "break"
    ? '<span class="chip pc-break">Pause</span>'
    : `<span class="chip" style="background:${tint(hc, 12)};color:${hc}">${esc(S.ep.projects.join("+") || "neu")}</span>`;
  $("#ep-head").innerHTML = `<b>${dayLabel(dateIso)}</b> · ` + head +
    (live ? ' · <span class="muted-c">läuft</span>' : "");
  $("#ep-from").value = b.entry ? clock(b.entry.start) : fmtH(b.f);
  $("#ep-to").value = b.entry ? (live ? "" : clock(b.entry.end)) : fmtH(b.t);
  renderChips();
  renderTaskSelect();
  const weekRow = $("#ep-week-row");
  weekRow.hidden = !!b.draft || !S.ep.orig.length;
  $("#ep-week-label").textContent = `Änderung auf alle ${S.ep.orig.join("+")}-Blöcke dieser Woche anwenden`;
  $("#ep-dup").hidden = $("#ep-del").hidden = !!b.draft;
}

$("#ep-save").onclick = async () => {
  const b = selectedBlock();
  if (!b || !S.ep) return;
  const dateIso = isoDate(addDays(S.monday, b.day));
  const fromV = $("#ep-from").value, toV = $("#ep-to").value;
  if (!fromV) { toast("Von-Zeit fehlt."); return; }
  const body = {
    kind: S.ep.kind,
    projects: S.ep.kind === "break" ? [] : S.ep.projects,
    note: $("#ep-note").value,
    taskId: S.ep.kind === "break" ? 0 : S.ep.taskId || 0,
    start: new Date(`${dateIso}T${fromV}:00`).toISOString(),
  };
  if (toV) {
    body.end = new Date(`${dateIso}T${toV}:00`).toISOString();
    if (body.end <= body.start) body.end = new Date(new Date(body.end).getTime() + 86400000).toISOString();
  } else if (!(b.entry && b.entry.open)) { toast("Bis-Zeit fehlt."); return; }
  try {
    if (b.draft) {
      const res = await api("POST", "/api/entries", body);
      S.draft = null;
      S.selBlock = res.id;
    } else {
      await api("PUT", "/api/entries/" + b.id, body);
      // Wochen-Anwenden: Projekt/Typ/Notiz auf alle Blöcke mit gleicher Projektmenge, Zeiten unverändert
      if ($("#ep-week").checked && S.ep.orig.length) {
        const key = (a) => (a || []).map((x) => x.toLowerCase()).sort().join("+");
        const k0 = key(S.ep.orig);
        for (const e of S.entries) {
          if (e.id === b.id || key(e.projects) !== k0) continue;
          await api("PUT", "/api/entries/" + e.id, { kind: body.kind, projects: body.projects, note: body.note });
        }
      }
    }
    S.ep = null;
    afterMutation();
  } catch (e) { toast(e.message); }
};

async function dupEntry(e) {
  if (e.open) { toast("Laufender Eintrag lässt sich nicht duplizieren."); return; }
  const dur = new Date(e.end) - new Date(e.start);
  try {
    const res = await api("POST", "/api/entries", {
      kind: e.kind, projects: e.projects || [], note: e.note || "", taskId: e.taskId || 0,
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
  $("#ep-note").focus();
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
    `<span class="proj-chips">${(e.projects || []).map(projChip).join(" ")}</span>` +
    `<span class="muted-c ellip">${e.task ? `<span class="chip chip-task">${esc(e.task)}</span> ` : ""}${esc(e.note || "")}</span>` +
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
// Zusammenführen: erster Eintrag wird auf die Gesamtspanne ausgedehnt
// (Projekte vereinigt, Notizen aneinandergehängt), die übrigen gelöscht.
$("#bulk-merge").onclick = async () => {
  const sel = S.entries.filter((e) => S.sel.has(e.id)).sort((a, b) => a.start.localeCompare(b.start));
  if (sel.length < 2) { toast("Mindestens zwei Einträge auswählen."); return; }
  if (sel.some((e) => e.open)) { toast("Laufender Eintrag lässt sich nicht zusammenführen."); return; }
  if (new Set(sel.map((e) => e.kind)).size > 1) { toast("Nur Einträge gleichen Typs zusammenführen."); return; }
  const first = sel[0], last = sel[sel.length - 1];
  const projects = [...new Set(sel.flatMap((e) => e.projects || []))];
  const note = [...new Set(sel.map((e) => e.note).filter(Boolean))].join(" · ");
  if (!confirm(`${sel.length} Einträge zu ${clock(first.start)}–${clock(last.end)} zusammenführen? Lücken dazwischen werden Teil des Eintrags.`)) return;
  try {
    for (const e of sel.slice(1)) await api("DELETE", "/api/entries/" + e.id);
    await api("PUT", "/api/entries/" + first.id, { end: last.end, projects, note });
  } catch (e) { toast(e.message); }
  S.sel.clear();
  afterMutation();
};
$("#bulk-del").onclick = async () => {
  if (!confirm(`${S.sel.size} Einträge löschen?`)) return;
  try { for (const id of S.sel) await api("DELETE", "/api/entries/" + id); }
  catch (e) { toast(e.message); }
  S.sel.clear();
  afterMutation();
};

// --- Projekte ---
async function loadProjectsTab() {
  const mon = startOfWeek(new Date());
  let rep;
  try {
    [S.projects, S.companies, rep] = await Promise.all([
      api("GET", "/api/projects"),
      api("GET", "/api/companies"),
      api("GET", `/api/report?from=${isoDate(mon)}&to=${isoDate(addDays(mon, 6))}`),
    ]);
  } catch (e) { toast(e.message); return; }
  S.tasksByProject = {}; // Aufgaben-Mutationen in diesem Tab invalidieren den Cache
  const week = Object.fromEntries((rep.projects || []).map((p) => [p.name.toLowerCase(), p.minutes]));
  const tbody = $("#projects-table tbody");
  tbody.innerHTML = "";
  const patch = async (id, body) => {
    try { await api("PUT", "/api/projects/" + id, body); } catch (e) { toast(e.message); }
    loadProjectsTab();
    loadProjects();
  };
  for (const p of S.projects) {
    const c = colorOf(p.name);
    const tr = document.createElement("tr");
    tr.innerHTML =
      `<td><span class="proj-name"><span class="proj-dot" style="background:${c}"></span>${esc(p.name)}</span></td>` +
      `<td><input class="proj-note" placeholder="Notiz"></td>` +
      `<td><select class="proj-comp"><option value="0">–</option>${(S.companies || []).map((co) =>
        `<option value="${co.id}"${p.companyId === co.id ? " selected" : ""}>${esc(co.name)}</option>`).join("")}</select></td>` +
      `<td><input class="proj-jira" placeholder="ABC" title="JIRA-Projekt-Key — Issues landen als Aufgaben"></td>` +
      `<td><span class="swatches">${PALETTE.map((col) =>
        `<button class="swatch${p.color === col ? " active" : ""}" style="background:${col}" data-c="${col}" title="${col}"></button>`).join("")}</span></td>` +
      `<td class="mono">${week[p.name.toLowerCase()] ? hm(week[p.name.toLowerCase()]) : "–"}</td>` +
      `<td><button class="rowbtn proj-tasks">Aufgaben${S.openTasks.has(p.id) ? " ▴" : " ▾"}</button></td>` +
      `<td><button class="rowbtn proj-arch">Archivieren</button></td>`;
    const noteEl = tr.querySelector(".proj-note");
    noteEl.value = p.note || "";
    noteEl.onchange = (ev) => patch(p.id, { note: ev.target.value });
    const jiraEl = tr.querySelector(".proj-jira");
    jiraEl.value = p.jiraKey || "";
    jiraEl.onchange = (ev) => patch(p.id, { jiraKey: ev.target.value.trim().toUpperCase() });
    tr.querySelector(".proj-comp").onchange = (ev) => patch(p.id, { companyId: Number(ev.target.value) });
    tr.querySelectorAll(".swatch").forEach((sw) => { sw.onclick = () => patch(p.id, { color: sw.dataset.c }); });
    tr.querySelector(".proj-tasks").onclick = () => {
      S.openTasks.has(p.id) ? S.openTasks.delete(p.id) : S.openTasks.add(p.id);
      loadProjectsTab();
    };
    tr.querySelector(".proj-arch").onclick = () => {
      if (confirm(`Projekt "${p.name}" archivieren?`)) patch(p.id, { archived: true });
    };
    tbody.appendChild(tr);
    if (S.openTasks.has(p.id)) tbody.appendChild(taskRowEl(p));
  }
  if (!S.projects.length) {
    tbody.innerHTML = '<tr><td colspan="8" class="muted-c">Noch keine Projekte — oben anlegen oder einfach einen Eintrag mit Projektnamen starten.</td></tr>';
  }
  renderCompanies();
}

// Aufgaben-Zeile unter einem Projekt: JIRA-Aufgaben read-only, lokale editierbar, Add-Input.
function taskRowEl(p) {
  const tr = document.createElement("tr");
  tr.className = "task-row";
  const td = document.createElement("td");
  td.colSpan = 8;
  td.innerHTML = '<span class="muted-c small">Lade Aufgaben…</span>';
  tr.appendChild(td);
  (async () => {
    let tasks;
    try { tasks = await api("GET", `/api/projects/${p.id}/tasks?includeArchived=true`); }
    catch (e) { td.innerHTML = `<span class="muted-c small">${esc(e.message)}</span>`; return; }
    td.innerHTML = "";
    if (!tasks.length) {
      td.insertAdjacentHTML("afterbegin",
        '<p class="muted-c small nomargin">Keine Aufgaben. JIRA-Key in der Zeile darüber setzen für den Import — oder unten eine eigene anlegen.</p>');
    }
    for (const t of tasks) {
      const row = document.createElement("div");
      row.className = "task-item" + (t.archived ? " task-archived" : "");
      if (t.jiraKey) {
        row.innerHTML = `<span class="chip chip-task">${esc(t.jiraKey)}</span>` +
          `<span class="ellip">${esc(t.title)}</span>` +
          `<span class="muted-c small">JIRA${t.archived ? " · archiviert" : ""}</span>`;
      } else {
        row.innerHTML = '<input class="task-title">' +
          `<button class="rowbtn task-arch">${t.archived ? "Reaktivieren" : "Archivieren"}</button>` +
          '<button class="rowbtn danger task-del" title="Löschen">×</button>';
        const inp = row.querySelector(".task-title");
        inp.value = t.title;
        inp.onchange = async () => {
          try { await api("PUT", "/api/tasks/" + t.id, { title: inp.value }); } catch (e) { toast(e.message); }
          loadProjectsTab();
        };
        row.querySelector(".task-arch").onclick = async () => {
          try { await api("PUT", "/api/tasks/" + t.id, { archived: !t.archived }); } catch (e) { toast(e.message); }
          loadProjectsTab();
        };
        row.querySelector(".task-del").onclick = async () => {
          if (!confirm(`Aufgabe "${t.title}" löschen?`)) return;
          try { await api("DELETE", "/api/tasks/" + t.id); } catch (e) { toast(e.message); }
          loadProjectsTab();
        };
      }
      td.appendChild(row);
    }
    const add = document.createElement("div");
    add.className = "task-add";
    add.innerHTML = '<input placeholder="Neue Aufgabe…"><button class="rowbtn">＋ Anlegen</button>';
    const inp = add.querySelector("input");
    const submit = async () => {
      if (!inp.value.trim()) return;
      try { await api("POST", `/api/projects/${p.id}/tasks`, { title: inp.value.trim() }); } catch (e) { toast(e.message); }
      loadProjectsTab();
    };
    add.querySelector("button").onclick = submit;
    inp.onkeydown = (ev) => { if (ev.key === "Enter") submit(); };
    td.appendChild(add);
  })();
  return tr;
}

// --- Unternehmen ---
function renderCompanies() {
  const list = $("#companies-list");
  list.innerHTML = "";
  for (const co of S.companies || []) {
    const used = S.projects.filter((p) => p.companyId === co.id).length;
    const row = document.createElement("div");
    row.className = "comp-row";
    row.innerHTML = `<b>${esc(co.name)}</b>` +
      `<span class="muted-c small">${used ? `${used} Projekt${used > 1 ? "e" : ""}` : "nicht zugewiesen"}</span>` +
      `<button class="rowbtn"${used ? " disabled" : ""}>Löschen</button>`;
    row.querySelector("button").onclick = async () => {
      if (!confirm(`Unternehmen "${co.name}" löschen?`)) return;
      try { await api("DELETE", "/api/companies/" + co.id); } catch (e) { toast(e.message); }
      loadProjectsTab();
    };
    list.appendChild(row);
  }
  if (!(S.companies || []).length) {
    list.innerHTML = '<p class="muted-c small nomargin">Noch keine Unternehmen.</p>';
  }
}
$("#comp-create").onclick = async () => {
  const name = $("#comp-new-name").value.trim();
  if (!name) { toast("Unternehmensname fehlt."); return; }
  try {
    await api("POST", "/api/companies", { name });
    $("#comp-new-name").value = "";
    loadProjectsTab();
  } catch (e) { toast(e.message); }
};
$("#comp-new-name").onkeydown = (ev) => { if (ev.key === "Enter") $("#comp-create").click(); };
$("#proj-create").onclick = async () => {
  const name = $("#proj-new-name").value.trim();
  if (!name) { toast("Projektname fehlt."); return; }
  try {
    await api("POST", "/api/projects", { name });
    $("#proj-new-name").value = "";
    loadProjectsTab();
    loadProjects();
  } catch (e) { toast(e.message); }
};
$("#proj-new-name").onkeydown = (ev) => { if (ev.key === "Enter") $("#proj-create").click(); };

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
  try {
    [rep, S.projects, S.companies] = await Promise.all([
      api("GET", `/api/report?from=${$("#report-from").value}&to=${$("#report-to").value}`),
      api("GET", "/api/projects"),
      api("GET", "/api/companies"),
    ]);
  } catch (e) { toast(e.message); return; }
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
    `<div class="bar-track"><div class="bar" style="width:${Math.max(p.percent, 1)}%;background:${colorOf(p.name)}"></div></div></div>`
  ).join("") || '<p class="muted-c">Keine Projektzeiten im Zeitraum.</p>';

  // Unternehmen: Projektanteile clientseitig über die Zuordnung aufsummieren
  const compOf = Object.fromEntries(S.projects.filter((p) => p.companyId).map((p) => [p.name.toLowerCase(), p.companyId]));
  const byComp = new Map();
  for (const p of rep.projects) {
    const cid = compOf[p.name.toLowerCase()];
    if (!cid) continue;
    const agg = byComp.get(cid) || { minutes: 0, projs: [] };
    agg.minutes += p.minutes;
    agg.projs.push(p.name);
    byComp.set(cid, agg);
  }
  const totalMin = rep.projects.reduce((a, p) => a + p.minutes, 0);
  const rows = [...byComp].map(([cid, agg]) => ({
    name: (S.companies.find((c) => c.id === cid) || { name: "?" }).name,
    ...agg, pct: totalMin ? agg.minutes / totalMin * 100 : 0,
  })).sort((a, b) => b.minutes - a.minutes);
  $("#report-companies-card").hidden = !rows.length;
  $("#report-companies").innerHTML = rows.map((c) =>
    `<div class="bar-row"><span class="ellip"><b>${esc(c.name)}</b><br><span class="muted-c small">${esc(c.projs.join(", "))}</span></span>` +
    `<span class="mono muted-c">${c.pct.toFixed(1)}%</span><span class="mono">${hm(c.minutes)}</span>` +
    `<div class="bar-track"><div class="bar" style="width:${Math.max(c.pct, 1)}%;background:var(--fg)"></div></div></div>`
  ).join("");
}

// --- Init ---
loadProjects();
refreshStatus();
loadWeekChart();
setInterval(refreshStatus, 2000);
setInterval(tickTimer, 1000);
