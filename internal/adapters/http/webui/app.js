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
  const stateEl = $("#status-state");
  const detail = $("#status-detail");
  const working = st.state === "working", paused = st.state === "paused";
  if (working) {
    stateEl.textContent = "▶ Am Arbeiten" + (st.project ? `: ${st.project}` : "");
    detail.textContent = `seit ${clock(st.since)} (${hm(st.openMinutes)})`;
  } else if (paused) {
    stateEl.textContent = "⏸ Pause";
    detail.textContent = `seit ${clock(st.since)} (${hm(st.openMinutes)})` +
      (st.openMinutes < 15 ? " — zählt erst ab 15 Min. als Pause" : "");
  } else {
    stateEl.textContent = "■ Nichts läuft";
    detail.textContent = "";
  }
  $("#btn-start").disabled = working || paused;
  $("#btn-switch").disabled = !working;
  $("#btn-pause").disabled = !working;
  $("#btn-resume").disabled = !paused;
  $("#btn-stop").disabled = !working && !paused;

  const t = st.todaySummary;
  $("#today-summary").innerHTML =
    `Ist <b>${hm(t.workedMinutes)}</b> · Pause ${hm(t.breakMinutes)} · Soll ${hm(t.targetMinutes)} · ` +
    `<b class="${saldoCls(t.diffMinutes)}">${saldoFmt(t.diffMinutes)}</b>` +
    (t.holidayName ? ` · ${esc(t.holidayName)}` : "") +
    (t.absenceType ? ` · ${esc(absLabel(t.absenceType, t.fraction))}` : "");
  const warns = [...t.warnings];
  if (st.longSession) warns.unshift("Sitzung läuft seit über 12 Stunden — vergessen zu stoppen?");
  $("#status-warnings").innerHTML = warns.map((w) => `<div class="warn">⚠ ${esc(w)}</div>`).join("");
  const saldoEl = $("#saldo");
  saldoEl.textContent = saldoFmt(st.saldoMinutes) + " Std.";
  saldoEl.className = "big " + saldoCls(st.saldoMinutes);
}

async function track(action, body) {
  try {
    const res = await api("POST", "/api/tracking/" + action, body);
    if (res && res.warnings) res.warnings.forEach((w) => toast("⚠ " + w));
    refreshStatus();
    loadProjects();
  } catch (e) { toast(e.message); }
}
$("#btn-start").onclick = () => track("start", { project: $("#start-project").value });
$("#btn-switch").onclick = () => {
  const p = $("#start-project").value;
  if (!p) { toast("Projektname ins Feld eingeben, dann Wechseln."); return; }
  track("switch", { project: p });
};
$("#btn-pause").onclick = () => track("pause");
$("#btn-resume").onclick = () => track("resume");
$("#btn-stop").onclick = () => track("stop");

async function loadProjects() {
  try {
    const ps = await api("GET", "/api/projects");
    $("#project-list").innerHTML = ps.map((p) => `<option value="${esc(p.name)}">`).join("");
  } catch { /* unkritisch */ }
}

// --- Einträge ---
function weekRange() {
  const now = new Date();
  const monday = new Date(now);
  monday.setDate(now.getDate() - ((now.getDay() + 6) % 7));
  const sunday = new Date(monday);
  sunday.setDate(monday.getDate() + 6);
  return [isoDate(monday), isoDate(sunday)];
}

let editingId = null;

async function loadEntries() {
  if (!$("#entries-from").value) {
    const [f, t] = weekRange();
    $("#entries-from").value = f;
    $("#entries-to").value = t;
  }
  let entries;
  try {
    entries = await api("GET", `/api/entries?from=${$("#entries-from").value}&to=${$("#entries-to").value}`);
  } catch (e) { toast(e.message); return; }
  const tbody = $("#entries-table tbody");
  tbody.innerHTML = "";
  for (const e of entries) {
    const endTxt = e.open ? "…läuft" : clock(e.end);
    const dur = e.open ? "" : hm((new Date(e.end) - new Date(e.start)) / 60000);
    const tr = document.createElement("tr");
    tr.innerHTML =
      `<td>${dayLabel(isoDate(new Date(e.start)))}</td>` +
      `<td>${clock(e.start)}–${endTxt}</td><td>${dur}</td>` +
      `<td>${e.kind === "break" ? "Pause" : "Arbeit"}</td>` +
      `<td>${esc(e.project)}</td><td>${esc(e.note)}</td>` +
      `<td><button class="icon" data-edit="${e.id}">✏️</button>` +
      `<button class="icon" data-del="${e.id}">🗑</button></td>`;
    tr.querySelector("[data-edit]").onclick = () => openEntryDialog(e);
    tr.querySelector("[data-del]").onclick = async () => {
      if (!confirm("Eintrag löschen?")) return;
      try { await api("DELETE", "/api/entries/" + e.id); loadEntries(); refreshStatus(); }
      catch (err) { toast(err.message); }
    };
    tbody.appendChild(tr);
  }
}
$("#entries-reload").onclick = loadEntries;
$("#entry-new").onclick = () => openEntryDialog(null);

function openEntryDialog(e) {
  editingId = e ? e.id : null;
  $("#entry-dialog-title").textContent = e ? `Eintrag #${e.id} bearbeiten` : "Neuer Eintrag";
  const start = e ? new Date(e.start) : new Date();
  $("#ed-date").value = isoDate(start);
  $("#ed-from").value = e ? clock(e.start) : "";
  $("#ed-to").value = e && !e.open ? clock(e.end) : "";
  $("#ed-to").required = !(e && e.open);
  $("#ed-kind").value = e ? e.kind : "work";
  $("#ed-project").value = e ? e.project || "" : "";
  $("#ed-note").value = e ? e.note || "" : "";
  $("#entry-dialog").showModal();
}

$("#entry-dialog-form").onsubmit = async (ev) => {
  if (ev.submitter && ev.submitter.value === "cancel") return;
  ev.preventDefault();
  const date = $("#ed-date").value;
  const mk = (t) => new Date(`${date}T${t}:00`).toISOString();
  const body = {
    kind: $("#ed-kind").value,
    project: $("#ed-project").value,
    note: $("#ed-note").value,
    start: mk($("#ed-from").value),
  };
  if ($("#ed-to").value) {
    body.end = mk($("#ed-to").value);
    if (body.end <= body.start) body.end = new Date(new Date(body.end).getTime() + 86400000).toISOString();
  }
  try {
    if (editingId === null) {
      await api("POST", "/api/entries", body);
    } else {
      await api("PUT", "/api/entries/" + editingId, body);
    }
    $("#entry-dialog").close();
    loadEntries();
    refreshStatus();
  } catch (e) { toast(e.message); }
};

// --- Abwesenheiten ---
async function loadAbsences() {
  if (!$("#absences-year").value) $("#absences-year").value = new Date().getFullYear();
  let list;
  try { list = await api("GET", "/api/absences?year=" + $("#absences-year").value); }
  catch (e) { toast(e.message); return; }
  const tbody = $("#absences-table tbody");
  tbody.innerHTML = "";
  let vacation = 0;
  for (const a of list) {
    if (a.type === "urlaub") vacation += a.fraction;
    const tr = document.createElement("tr");
    tr.innerHTML = `<td>${dayLabel(a.date)} ${a.date}</td><td>${absLabel(a.type, a.fraction)}</td>` +
      `<td>${esc(a.note)}</td><td><button class="icon" data-del>🗑</button></td>`;
    tr.querySelector("[data-del]").onclick = async () => {
      try { await api("DELETE", "/api/absences/" + a.id); loadAbsences(); refreshStatus(); }
      catch (err) { toast(err.message); }
    };
    tbody.appendChild(tr);
  }
  $("#vacation-count").textContent = `Urlaubstage: ${vacation}`;
}
$("#absences-reload").onclick = loadAbsences;
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
  loadReport();
}
document.querySelectorAll("[data-range]").forEach((b) => { b.onclick = () => setRange(b.dataset.range); });
$("#report-reload").onclick = loadReport;

async function loadReport() {
  if (!$("#report-from").value) {
    const [f, t] = weekRange();
    $("#report-from").value = f;
    $("#report-to").value = t;
  }
  let rep;
  try { rep = await api("GET", `/api/report?from=${$("#report-from").value}&to=${$("#report-to").value}`); }
  catch (e) { toast(e.message); return; }
  $("#report-totals").innerHTML =
    `Ist <b>${hm(rep.totalWorkedMinutes)}</b> · Soll ${hm(rep.totalTargetMinutes)} · ` +
    `Gutschrift ${hm(rep.totalCreditMinutes)} · Saldo <b class="${saldoCls(rep.saldoMinutes)}">${saldoFmt(rep.saldoMinutes)}</b>`;
  const tbody = $("#report-table tbody");
  tbody.innerHTML = "";
  for (const d of rep.days) {
    if (!d.workedMinutes && !d.targetMinutes && !d.absenceType && !d.holidayName) continue;
    const extra = [d.holidayName, d.absenceType && absLabel(d.absenceType, d.fraction)].filter(Boolean).join(" · ");
    const tr = document.createElement("tr");
    tr.innerHTML =
      `<td>${dayLabel(d.date)}</td><td>${hm(d.workedMinutes)}</td><td>${hm(d.breakMinutes)}</td>` +
      `<td>${hm(d.targetMinutes)}</td><td class="${saldoCls(d.diffMinutes)}">${saldoFmt(d.diffMinutes)}</td>` +
      `<td class="muted">${esc(extra)}${d.warnings.length ? " ⚠ " + esc(d.warnings.join(" · ")) : ""}</td>`;
    tbody.appendChild(tr);
  }
  const projects = [...rep.projects].sort((a, b) => b.percent - a.percent);
  $("#report-projects").innerHTML = projects.map((p) =>
    `<div class="bar-row"><span>${esc(p.name)}</span><span>${p.percent.toFixed(1)}%</span>` +
    `<span>${hm(p.minutes)}</span><div class="bar" style="width:${Math.max(p.percent, 1)}%"></div></div>`
  ).join("") || '<p class="muted">Keine Projektzeiten im Zeitraum.</p>';
}

// --- Init ---
loadProjects();
refreshStatus();
setInterval(refreshStatus, 2000);
