// Whetstone browser client.
//
// The server returns the entire workspace on every action, so this file never
// merges state: it replaces `state` and re-renders. That removes a whole class
// of desync bug and keeps the code short enough to read in one sitting.
//
// There are no browser prompt() dialogs. Everything you type — a point, a note,
// a reason for dismissing an objection — is typed into a real text box in place,
// because a modal that blocks the page is a bad place to compose a sentence and
// this tool is mostly about composing sentences.

"use strict";

let state = null;
let selected = null;   // {docId, sectionId} currently being read
let activeNode = null; // outline node id the side actions apply to

const $ = (sel) => document.querySelector(sel);

// NL is a named newline and NEWLINES splits on line endings of either flavour.
// Escape sequences in this file have been mangled more than once by the tooling
// that edits it; named constants built from char codes cannot be.
const NL = String.fromCharCode(10);
const NEWLINES = new RegExp(String.fromCharCode(13) + "?" + String.fromCharCode(10));

// --- transport ---

async function api(path, body) {
  const res = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Whetstone-Token": window.WHETSTONE_TOKEN,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({ error: res.statusText }));
  if (!res.ok) throw new Error(data.error || `request failed (${res.status})`);
  return data;
}

// act runs a mutating call, shows progress, and re-renders. Errors are shown
// rather than swallowed: "no provocations" and "the provider is down" must not
// look the same.
async function act(label, path, body) {
  if (label) setStatus(label + "…", "busy");
  try {
    state = await api(path, body ?? {});
    render();
    setStatus("");
    return true;
  } catch (err) {
    setStatus(err.message, "error");
    return false;
  }
}

function setStatus(msg, cls = "") {
  const el = $("#status");
  el.textContent = msg;
  el.className = cls;
}

// --- small DOM helpers ---

function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
}

function btn(text, onclick, cls) {
  const b = el("button", cls, text);
  b.onclick = onclick;
  return b;
}

function clip(s, n) {
  const flat = s.replace(/\s+/g, " ").trim();
  return flat.length <= n ? flat : flat.slice(0, n) + "…";
}

// --- render ---

function render() {
  if (!state) return;

  // Re-rendering replaces every text area, which would throw away the caret —
  // and, if you were mid-sentence, your place in the sentence. Remember where
  // the caret was and put it back afterwards.
  const focus = captureFocus();

  const qs = questionLines();
  const qBtn = $("#question");
  qBtn.textContent = qs.length
    ? (qs.length > 1 ? `${qs[0]}  (+${qs.length - 1} more)` : qs[0])
    : "no question set";
  qBtn.title = qs.length ? qs.join(NL) : "What are you trying to answer?";
  $("#path").textContent = state.path;
  renderEngagement();
  renderLensPicker();
  renderSources();
  renderReader();
  renderOutline();

  restoreFocus(focus);
}

// captureFocus records the focused editor and caret, keyed by the data-field
// attribute rather than by element identity, since the element itself is about
// to be replaced.
function captureFocus() {
  const a = document.activeElement;
  if (!a || !a.dataset || !a.dataset.field) return null;
  return { field: a.dataset.field, start: a.selectionStart, end: a.selectionEnd };
}

function restoreFocus(f) {
  if (!f) return;
  const next = document.querySelector(`[data-field="${f.field}"]`);
  if (!next) return;
  next.focus();
  try {
    next.setSelectionRange(f.start, f.end);
  } catch {
    // Some input types do not support selection ranges; focus alone is enough.
  }
}

// renderEngagement spells the figures out. "yours 71%" meant nothing without
// the sentence behind it, and a metric nobody can read is a metric nobody acts
// on. Each part is also omitted until there is something for it to measure.
function renderEngagement() {
  const e = state.engagement;
  const parts = [];

  if (e.sectionsTotal > 0) {
    parts.push(`${e.sectionsRead} of ${e.sectionsTotal} sections read`);
  }
  if (e.open > 0) {
    parts.push(`${e.open} objection${e.open === 1 ? "" : "s"} unanswered`);
  }
  const words = e.userWords + e.genWords;
  if (words > 0) {
    parts.push(`${Math.round(e.authorship * 100)}% of the words are yours`);
  }

  const box = $("#engagement");
  box.textContent = parts.join("  ·  ");
  box.title = words > 0
    ? `You have written ${e.userWords} words; ${e.genWords} were generated. ` + e.caveat
    : e.caveat;
}

function renderLensPicker() {
  const sel = $("#lens");
  if (sel.dataset.filled !== "1") {
    for (const l of state.lenses) {
      const opt = el("option", null, l.name);
      opt.value = l.id;
      opt.title = l.description;
      sel.append(opt);
    }
    sel.dataset.filled = "1";
  }
  sel.value = state.activeLens;
}

function renderSources() {
  const list = $("#doc-list");
  list.innerHTML = "";

  for (const doc of state.documents) {
    const head = el("div", "doc-title");
    head.append(document.createTextNode(doc.title));
    head.append(btn("×", () => act("removing", "/api/documents/delete", { docId: doc.id }), "quiet"));
    list.append(head);

    for (const sec of doc.sections) {
      const open = sec.provocations.filter((p) => p.status === "open").length;
      const row = el("div", "section-row" + (sec.read ? " read" : ""));
      if (selected && selected.docId === doc.id && selected.sectionId === sec.id) {
        row.classList.add("active");
      }
      row.append(el("span", "mark", sec.read ? "●" : "○"));
      row.append(el("span", "flag", open ? "!" : ""));
      // A real markdown heading is a title; a chunk of prose only has a title
      // we synthesised from its opening words. Showing them identically makes
      // the sidebar unreadable, so prose chunks are quoted and indented.
      const label = el("span", "label " + (sec.heading ? "is-heading" : "is-prose"),
        sec.heading ? sec.title : "“" + sec.title);
      label.title = sec.heading ? sec.title : "prose — no heading in the source";
      if (sec.heading && sec.level > 1) label.style.paddingLeft = (sec.level - 1) * 10 + "px";
      row.append(label);
      row.onclick = () => openSection(doc.id, sec.id);
      list.append(row);
    }
  }

  if (!state.documents.length) {
    list.append(el("p", "dim", "Nothing yet."));
  }
}

// questionLines splits the question field into the separate questions it holds.
function questionLines() {
  return String(state.question || "")
    .split(NEWLINES)
    .map((q) => q.trim())
    .filter(Boolean);
}

function currentSection() {
  if (!selected) return null;
  const doc = state.documents.find((d) => d.id === selected.docId);
  if (!doc) return null;
  const sec = doc.sections.find((s) => s.id === selected.sectionId);
  return sec ? { doc, sec } : null;
}

function currentNode() {
  if (!state.outline.length) return null;
  return state.outline.find((n) => n.id === activeNode) || state.outline[0];
}

function renderReader() {
  const found = currentSection();
  $("#reader-empty").hidden = !!found;
  $("#reader-body").hidden = !found;
  $("#selection-bar").hidden = true;
  if (!found) return;

  const { doc, sec } = found;
  $("#reader-title").textContent = sec.title;
  $("#reader-meta").textContent = `${doc.title} · ${sec.words} words`;

  const lensName = (state.lenses.find((l) => l.id === state.activeLens) || {}).name || "lens";

  const lensBox = $("#reader-lens");
  lensBox.innerHTML = "";
  if (sec.summary && sec.summary.text) {
    lensBox.append(el("div", null, sec.summary.text));
    if (sec.summary.keyPoints && sec.summary.keyPoints.length) {
      const ul = el("ul");
      for (const kp of sec.summary.keyPoints) ul.append(el("li", null, kp));
      lensBox.append(ul);
    }
  } else {
    lensBox.append(el("span", "dim", `No ${lensName} reading of this section yet.`));
  }

  const bar = $("#reader-actions");
  bar.innerHTML = "";
  bar.append(
    btn(`Apply ${lensName} lens`, () =>
      act("applying lens", "/api/sections/lens", { docId: doc.id, sectionId: sec.id })),
    btn("Provoke", () =>
      act("generating provocations", "/api/provoke", { docId: doc.id, sectionId: sec.id })),
    btn("Edit text", () => startEditing(doc, sec)),
    btn("Delete section", () => {
      if (!confirm(`Delete "${sec.title}"? Citations to it will be marked stale.`)) return;
      selected = null;
      act("deleting section", "/api/sections/delete", { docId: doc.id, sectionId: sec.id });
    }),
  );

  $("#reader-text").hidden = false;
  $("#reader-text").textContent = sec.body;
  const editor = $("#reader-edit");
  editor.hidden = true;
  editor.innerHTML = "";

  const provBox = $("#reader-provocations");
  provBox.innerHTML = "";
  for (const p of sec.provocations) provBox.append(provocationEl(p));
}

// startEditing swaps the passage for a text area. Source material is normally
// read rather than written, but a pasted document often arrives with junk in
// it, and being unable to fix a mangled line is worse than the risk of editing
// evidence. Citations survive: their IDs never move, and any whose quoted text
// no longer appears is flagged stale rather than silently broken.
function startEditing(doc, sec) {
  const box = $("#reader-edit");
  box.innerHTML = "";
  box.hidden = false;
  $("#reader-text").hidden = true;

  box.append(el("div", "field-label", "Section title"));
  const title = el("input");
  title.value = sec.title;
  box.append(title);

  box.append(el("div", "field-label", "Text — edit, rewrite, or delete lines"));
  const ta = el("textarea", "source-edit");
  ta.value = sec.body;
  // Size the box to the passage so editing does not happen through a slot.
  ta.rows = Math.min(Math.max(countLines(sec.body) + 2, 10), 30);
  box.append(ta);

  const row = el("div", "toolbar");
  const save = () =>
    act("saving section", "/api/sections/update", {
      docId: doc.id, sectionId: sec.id, title: title.value, body: ta.value,
    });
  row.append(
    btn("Save", save),
    btn("Cancel", () => { box.hidden = true; $("#reader-text").hidden = false; }),
    btn("Say it differently…", () =>
      showToneMenu(selectedWithin(ta) || ta.value, (alt) => {
        const sel = selectedWithin(ta);
        ta.value = sel
          ? ta.value.slice(0, ta.selectionStart) + alt + ta.value.slice(ta.selectionEnd)
          : alt;
      })),
  );
  box.append(row);
  box.append(el("div", "dim",
    "Cmd/Ctrl+Enter to save. Citations quoting text you remove will be marked stale."));

  ta.onkeydown = (ev) => {
    if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) { ev.preventDefault(); save(); }
  };
  ta.focus();
}

// selectedWithin returns the text selected inside a textarea, if any.
function selectedWithin(ta) {
  if (ta.selectionStart === ta.selectionEnd) return "";
  return ta.value.slice(ta.selectionStart, ta.selectionEnd).trim();
}

function countLines(s) {
  return s ? s.split(/\r?\n/).length : 1;
}

// --- selection ---
//
// Selecting a line is the main path from reading to arguing: it can become a new
// point, evidence for an existing one, or a quote in your notes.

// selectionInReader returns the selected text, but only when the whole selection
// lies inside the passage. A selection straying into the sidebar or a
// provocation is not evidence.
function selectionInReader() {
  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || sel.rangeCount === 0) return "";
  const text = sel.toString().trim();
  if (!text) return "";
  const body = $("#reader-text");
  if (!body.contains(sel.getRangeAt(0).commonAncestorContainer)) return "";
  return text;
}

function updateSelectionBar() {
  const bar = $("#selection-bar");
  const text = selectionInReader();
  const found = currentSection();
  if (!text || !found) {
    bar.hidden = true;
    return;
  }

  bar.hidden = false;
  $("#sel-preview").textContent = `"${clip(text, 60)}"`;

  const node = currentNode();
  const target = node ? `"${clip(node.title, 24)}"` : "—";
  $("#sel-cite").textContent = `cite into ${target}`;
  $("#sel-note").textContent = `quote in notes of ${target}`;
  $("#sel-cite").disabled = !node;
  $("#sel-note").disabled = !node;

  $("#sel-point").onclick = () => {
    clearSelection();
    showPointForm({
      title: clip(text, 120),
      docId: found.doc.id,
      sectionId: found.sec.id,
      excerpt: text,
    });
  };
  $("#sel-cite").onclick = () => {
    if (!node) return;
    clearSelection();
    act("citing", "/api/outline/cite", {
      nodeId: node.id, docId: found.doc.id, sectionId: found.sec.id, excerpt: text,
    });
  };
  $("#sel-note").onclick = () => {
    if (!node) return;
    clearSelection();
    quoteIntoNotes(node, text);
  };
  $("#sel-tone").onclick = () => showToneMenu(text, null);
}

// --- tone dimensions ---
//
// Pick a dimension, see several re-voicings side by side, choose one or keep
// what you had. Nothing is applied automatically: choosing between concrete
// options is a judgement you make.

function showToneMenu(text, apply) {
  closeOverlay();
  const box = el("div", "overlay");
  box.append(el("div", "field-label", "Say this differently"));
  box.append(el("div", "dim", `"${clip(text, 90)}"`));

  const grid = el("div", "dim-grid");
  for (const d of state.dimensions) {
    const b = btn(d.name, () => fetchAlternatives(text, d, apply));
    b.title = d.description;
    grid.append(b);
  }
  box.append(grid);
  const foot = el("div", "toolbar");
  foot.append(btn("Cancel", closeOverlay));
  box.append(foot);
  document.body.append(box);
}

async function fetchAlternatives(text, d, apply) {
  closeOverlay();
  setStatus(`rewriting: ${d.name}…`, "busy");
  let alts;
  try {
    const res = await api("/api/rewrite", { text, dimensionId: d.id, count: 3 });
    alts = res.alternatives || [];
  } catch (err) {
    setStatus(err.message, "error");
    return;
  }
  setStatus("");
  if (!alts.length) {
    setStatus("no alternatives came back — the passage may already fit", "error");
    return;
  }

  const box = el("div", "overlay wide");
  box.append(el("div", "field-label", d.name));
  box.append(el("div", "dim", d.description));

  const keep = el("div", "alt original");
  keep.append(el("div", "field-label", "What you wrote"));
  keep.append(el("div", null, text));
  box.append(keep);

  for (const alt of alts) {
    const card = el("div", "alt");
    card.append(el("div", null, alt));
    const row = el("div", "toolbar");
    row.append(btn("Use this", () => {
      closeOverlay();
      if (apply) apply(alt);
      else copyToClipboard(alt);
    }));
    card.append(row);
    box.append(card);
  }

  const foot = el("div", "toolbar");
  foot.append(btn("Keep mine", closeOverlay));
  box.append(foot);
  document.body.append(box);
}

// copyToClipboard is the fallback when a rewrite has no field to write back to
// — a selection in the source, which is evidence and must not be edited.
function copyToClipboard(text) {
  navigator.clipboard.writeText(text).then(
    () => setStatus("copied — paste it into your notes"),
    () => setStatus("could not copy; select the text and copy manually", "error"),
  );
}

function closeOverlay() {
  const o = document.querySelector(".overlay");
  if (o) o.remove();
}

document.addEventListener("keydown", (ev) => {
  if (ev.key === "Escape") closeOverlay();
});

function clearSelection() {
  window.getSelection().removeAllRanges();
  $("#selection-bar").hidden = true;
}

// quoteIntoNotes appends the selection to a point's notes as an indented quote,
// leaving a blank line under it for the response. The quote is the evidence; the
// line you write under it is the thinking.
function quoteIntoNotes(node, text) {
  const quote = "> " + text.replace(/\s+/g, " ").trim();
  const notes = node.notes ? node.notes.replace(/\s*$/, "") + "\n\n" + quote + "\n\n" : quote + "\n\n";
  act("adding quote", "/api/outline/update", { id: node.id, notes }).then((ok) => {
    if (!ok) return;
    activeNode = node.id;
    render();
    focusNotes(node.id);
  });
}

// focusNotes puts the caret at the end of a point's notes so you can keep typing
// straight after a quote lands.
function focusNotes(nodeId) {
  const ta = document.querySelector(`textarea[data-field="notes:${nodeId}"]`);
  if (!ta) return;
  ta.focus();
  ta.selectionStart = ta.selectionEnd = ta.value.length;
  ta.scrollIntoView({ block: "nearest" });
}

document.addEventListener("selectionchange", updateSelectionBar);

// --- provocations ---

function provocationEl(p) {
  const root = el("div", "provocation" + (p.status === "open" ? "" : " resolved"));

  const head = el("div", "prov-head");
  head.append(el("span", "kind", p.label), el("span", "status", p.status));
  root.append(head);
  root.append(el("div", "prov-text", p.text));

  if (p.response) root.append(el("div", "prov-response dim", "you: " + p.response));

  if (p.status === "open") {
    const actions = el("div", "prov-actions");
    actions.append(
      btn("Engage", () => showResolveForm(root, p, true)),
      btn("Dismiss", () => showResolveForm(root, p, false)),
    );
    root.append(actions);
  }
  return root;
}

// showResolveForm replaces the buttons with a text box in place. The reason is
// mandatory — the server refuses a blank one — so it gets a real place to write.
function showResolveForm(card, p, engaged) {
  const existing = card.querySelector(".inline-form");
  if (existing) existing.remove();

  const form = el("div", "inline-form");
  form.append(el("div", "field-label",
    engaged ? "What did you change or decide?" : "Why does this objection not apply?"));

  const ta = el("textarea");
  ta.rows = 3;
  ta.placeholder = engaged
    ? "e.g. added a caveat that the figure is stated preference"
    : "e.g. our panel is the buying segment, checked against 2024 POS data";
  form.append(ta);

  const row = el("div", "toolbar");
  const submit = async () => {
    if (!ta.value.trim()) {
      setStatus("a reason is required — that is the point", "error");
      ta.focus();
      return;
    }
    await act("recording", "/api/provocations/resolve", {
      id: p.id, engaged, response: ta.value,
    });
  };
  row.append(btn(engaged ? "Record" : "Dismiss", submit), btn("Cancel", () => form.remove()));
  form.append(row);

  // Ctrl/Cmd+Enter submits; Esc backs out.
  ta.onkeydown = (ev) => {
    if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) { ev.preventDefault(); submit(); }
    if (ev.key === "Escape") form.remove();
  };

  card.append(form);
  ta.focus();
}

// --- outline ---

function renderOutline() {
  const box = $("#outline");
  box.innerHTML = "";

  if (!state.outline.length) {
    box.append(el("p", "dim",
      "Empty. Add the first point of your argument, or select a line in a " +
      "passage and turn it into one. Nothing here is written by a model."));
    return;
  }

  for (const n of state.outline) {
    const card = el("div", "node" + (n.id === activeNode ? " active" : ""));
    card.style.marginLeft = n.depth * 14 + "px";
    card.onclick = (ev) => {
      if (ev.target.closest("button, textarea, input")) return;
      activeNode = n.id;
      render();
    };

    const head = el("div", "node-head");
    head.append(el("div", "node-title", n.title));
    const badges = el("div", "node-badges");
    const open = n.provocations.filter((p) => p.status === "open").length;
    if (n.grounding.length) badges.append(el("span", null, `${n.grounding.length} cited`));
    if (open) badges.append(el("span", null, `${open} open`));
    head.append(badges);
    card.append(head);

    card.append(el("div", "field-label", "Your notes"));
    const notes = autosaveArea(n.notes, "", (v) =>
      api("/api/outline/update", { id: n.id, notes: v }));
    notes.dataset.field = "notes:" + n.id;
    notes.placeholder = "Your reasoning, in your words. Drafts are built from this.";
    card.append(notes);

    if (n.grounding.length) {
      card.append(el("div", "field-label", "Cited from your sources"));
      const cites = el("div", "cites");
      for (const g of n.grounding) {
        // Clicking a citation opens the section it came from and highlights the
        // exact sentence, so evidence is always one click from the claim.
        const c = el("div", "cite" + (g.stale ? " stale" : ""),
          (g.stale ? "⚠ " : "") + `§${g.sectionId}  ${g.excerpt}`);
        c.title = g.stale
          ? "The source no longer contains this text — it was edited or deleted"
          : "Go to this passage";
        c.onclick = () => jumpToCitation(g);
        cites.append(c);
      }
      card.append(cites);
    }

    if (n.draft) {
      card.append(el("div", "field-label",
        n.draftEdited
          ? "Draft — edited by you, counts as half yours"
          : "Draft — generated. Edit it: it is not yours until you do."));
      const draft = autosaveArea(n.draft, "draft", (v) =>
        api("/api/outline/update", { id: n.id, draft: v }));
      draft.dataset.field = "draft:" + n.id;
      card.append(draft);
    }

    const bar = el("div", "toolbar");
    bar.append(
      btn("Draft", () => act("drafting", "/api/outline/draft", { id: n.id })),
      btn("Compose…", () => showComposeForm(card, n)),
      btn("Review my writing", () =>
        act("looking for fallacies in your argument", "/api/provoke", { nodeId: n.id })),
      btn("Sub-point", () => showPointForm({ parentId: n.id })),
      btn("Delete", () => act("deleting", "/api/outline/delete", { id: n.id })),
    );
    if (n.draft) {
      bar.append(btn("Say it differently…", () =>
        showToneMenu(n.draft, (alt) =>
          act("applying", "/api/outline/update", { id: n.id, draft: alt }))));
    }
    if (n.notes) {
      bar.append(btn("Reword notes…", () =>
        showToneMenu(n.notes, (alt) =>
          act("applying", "/api/outline/update", { id: n.id, notes: alt }))));
    }
    card.append(bar);

    for (const p of n.provocations) card.append(provocationEl(p));
    box.append(card);
  }
}

// showPointForm renders the new-point editor inline at the top of the argument
// panel: a title, your notes, and the citation it will carry.
function showPointForm(opts = {}) {
  const box = $("#outline");
  const old = box.querySelector(".point-form");
  if (old) old.remove();

  const form = el("div", "node point-form");
  form.append(el("div", "field-label", opts.parentId ? "New sub-point" : "New point"));

  const title = el("input");
  title.placeholder = "Your claim, in your words";
  title.value = opts.title || "";
  form.append(title);

  form.append(el("div", "field-label", "Your notes (optional)"));
  const notes = el("textarea");
  notes.rows = 3;
  notes.placeholder = "Why you believe it. You can add this later.";
  form.append(notes);

  if (opts.excerpt) {
    const cite = el("div", "cites");
    cite.append(el("div", null, `will cite: ${clip(opts.excerpt, 140)}`));
    form.append(cite);
  }

  const submit = async () => {
    if (!title.value.trim()) {
      setStatus("a point needs a claim", "error");
      title.focus();
      return;
    }
    const ok = await act("adding point", "/api/outline/add", {
      parentId: opts.parentId || "",
      title: title.value,
      docId: opts.docId || "",
      sectionId: opts.sectionId || 0,
      excerpt: opts.excerpt || "",
    });
    if (!ok) return;
    const added = state.outline[state.outline.length - 1];
    if (added) {
      activeNode = added.id;
      if (notes.value.trim()) {
        await act("", "/api/outline/update", { id: added.id, notes: notes.value });
      }
      render();
      focusNotes(added.id);
    }
  };

  const row = el("div", "toolbar");
  row.append(btn("Add point", submit), btn("Cancel", () => form.remove()));
  form.append(row);

  const onKey = (ev) => {
    if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) { ev.preventDefault(); submit(); }
    if (ev.key === "Escape") form.remove();
  };
  title.onkeydown = (ev) => {
    if (ev.key === "Enter") { ev.preventDefault(); notes.focus(); }
    onKey(ev);
  };
  notes.onkeydown = onKey;

  box.prepend(form);
  title.focus();
  title.setSelectionRange(title.value.length, title.value.length);
}

// showComposeForm asks what this paragraph should do. The instruction steers;
// your notes and citations remain the only source material, so this is a
// steering wheel rather than a prompt box.
function showComposeForm(card, n) {
  const existing = card.querySelector(".compose-form");
  if (existing) { existing.remove(); return; }

  const form = el("div", "inline-form compose-form");
  form.append(el("div", "field-label", "What should this paragraph do?"));

  const ta = el("textarea");
  ta.rows = 2;
  ta.placeholder = "e.g. lead with the cost objection, then answer it with the benchmark";
  form.append(ta);
  form.append(el("div", "dim",
    "Written from your notes and citations only. It will not invent evidence."));

  const submit = async () => {
    if (!ta.value.trim()) {
      setStatus("say what you want this paragraph to do", "error");
      ta.focus();
      return;
    }
    await act("composing", "/api/outline/compose", { id: n.id, instruction: ta.value });
  };
  const row = el("div", "toolbar");
  row.append(btn("Compose", submit), btn("Cancel", () => form.remove()));
  form.append(row);
  ta.onkeydown = (ev) => {
    if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) { ev.preventDefault(); submit(); }
    if (ev.key === "Escape") form.remove();
  };

  card.append(form);
  ta.focus();
}

// jumpToCitation opens the cited section and flashes the quoted sentence.
function jumpToCitation(g) {
  selected = { docId: g.docId, sectionId: g.sectionId };
  render();
  const pre = $("#reader-text");
  pre.scrollIntoView({ block: "start", behavior: "smooth" });
  highlightIn(pre, g.excerpt);
}

// highlightIn finds the excerpt in the rendered passage and marks it. The
// excerpt was whitespace-collapsed when stored, so the search is too.
function highlightIn(pre, excerpt) {
  const needle = excerpt.replace(/\s+/g, " ").replace(/…$/, "").trim();
  if (!needle) return;
  const hay = pre.textContent;
  const flatHay = hay.replace(/\s+/g, " ");
  const at = flatHay.indexOf(needle.slice(0, 60));
  if (at < 0) {
    setStatus("that passage has changed since you cited it", "error");
    return;
  }
  // Map the flattened offset back onto the original text.
  let orig = 0, flat = 0;
  while (flat < at && orig < hay.length) {
    if (/\s/.test(hay[orig])) {
      while (orig < hay.length && /\s/.test(hay[orig])) orig++;
      flat++;
    } else {
      orig++;
      flat++;
    }
  }
  const end = Math.min(orig + needle.length + 40, hay.length);

  pre.textContent = "";
  pre.append(document.createTextNode(hay.slice(0, orig)));
  const mark = el("mark", null, hay.slice(orig, end));
  pre.append(mark);
  pre.append(document.createTextNode(hay.slice(end)));
  mark.scrollIntoView({ block: "center", behavior: "smooth" });
}

// autosaveArea debounces so a paragraph is not one request per keystroke.
function autosaveArea(value, cls, save) {
  const ta = el("textarea", cls);
  ta.value = value;
  let timer = null;
  ta.oninput = () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      try {
        state = await save(ta.value);
        setStatus("saved");
        // Deliberately not re-rendering the whole page: that would steal focus
        // mid-sentence. Only the figures need refreshing.
        renderEngagement();
      } catch (err) {
        setStatus(err.message, "error");
      }
    }, 600);
  };
  return ta;
}

// --- documents ---

function openSection(docId, sectionId) {
  selected = { docId, sectionId };
  render();
  act("", "/api/sections/read", { docId, sectionId });
}

function showPasteForm() {
  const panel = $("#sources");
  const old = panel.querySelector(".paste-form");
  if (old) { old.remove(); return; }

  const form = el("div", "paste-form");
  form.append(el("div", "field-label", "Paste a document"));

  const title = el("input");
  title.placeholder = "Title (optional)";
  form.append(title);

  const text = el("textarea");
  text.rows = 10;
  text.placeholder = "Paste markdown or plain text here…";
  form.append(text);

  const submit = async () => {
    if (!text.value.trim()) { form.remove(); return; }
    const ok = await act("parsing", "/api/documents", {
      title: title.value, text: text.value,
    });
    if (!ok) return;
    form.remove();
    const last = state.documents[state.documents.length - 1];
    if (last && last.sections.length) openSection(last.id, last.sections[0].id);
  };

  const row = el("div", "toolbar");
  row.append(btn("Add document", submit), btn("Cancel", () => form.remove()));
  form.append(row);

  text.onkeydown = (ev) => {
    if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) { ev.preventDefault(); submit(); }
    if (ev.key === "Escape") form.remove();
  };

  panel.querySelector("h2").after(form);
  text.focus();
}

function showQuestionForm() {
  const header = $("#question");
  const old = $("#question-form");
  if (old) { old.remove(); return; }

  const form = el("div", "inline-form");
  form.id = "question-form";
  form.append(el("div", "field-label", "What are you trying to answer?"));

  // A text area rather than a single line: one question per line, so a main
  // question can carry the sub-questions it breaks into. All of it is given to
  // the critic as context, so more specificity here means sharper objections.
  const input = el("textarea");
  input.rows = 4;
  input.placeholder =
    [
      "One per line, most important first. For example:",
      "Should we adopt the new scheduler?",
      "What does it cost us to run?",
      "Does the deprecation timeline force our hand?",
    ].join(NL);
  input.value = state.question || "";
  form.append(input);
  form.append(el("div", "dim",
    "Genuinely separate work belongs in its own workspace: whetstone -web -w other.json"));

  const submit = async () => {
    await act("saving question", "/api/question", { question: input.value });
    form.remove();
  };
  const row = el("div", "toolbar");
  row.append(btn("Save", submit), btn("Cancel", () => form.remove()));
  form.append(row);

  input.onkeydown = (ev) => {
    if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) { ev.preventDefault(); submit(); }
    if (ev.key === "Escape") form.remove();
  };

  header.after(form);
  input.focus();
  input.setSelectionRange(input.value.length, input.value.length);
}


// --- layout ---
//
// The three columns are resizable and the widths persist, because how much room
// reading deserves versus writing is a per-person, per-document judgement and
// not one this program should be making.

const LAYOUT_KEY = "whetstone.layout";

function loadLayout() {
  let saved = {};
  try {
    saved = JSON.parse(localStorage.getItem(LAYOUT_KEY) || "{}");
  } catch {
    // Corrupt or unavailable storage is not worth failing a page load over.
  }
  if (saved.left) document.body.style.setProperty("--col-left", saved.left);
  if (saved.right) document.body.style.setProperty("--col-right", saved.right);
}

function saveLayout() {
  const style = document.body.style;
  try {
    localStorage.setItem(LAYOUT_KEY, JSON.stringify({
      left: style.getPropertyValue("--col-left"),
      right: style.getPropertyValue("--col-right"),
    }));
  } catch {
    // Ignore: losing a pane width is not worth an error message.
  }
}

// wireResizer makes one handle drag one column. `side` says which edge the
// column is anchored to, which decides the sign of the delta.
function wireResizer(handleId, prop, side) {
  const handle = document.getElementById(handleId);
  if (!handle) return;

  handle.addEventListener("pointerdown", (ev) => {
    ev.preventDefault();
    handle.setPointerCapture(ev.pointerId);
    handle.classList.add("dragging");
    document.body.classList.add("resizing");

    const startX = ev.clientX;
    const startWidth = parseInt(
      getComputedStyle(document.body).getPropertyValue(prop) || "0", 10);

    const onMove = (m) => {
      const delta = side === "left" ? m.clientX - startX : startX - m.clientX;
      // Floor keeps a panel from vanishing into an un-grabbable sliver;
      // ceiling keeps the reader from being squeezed out.
      const next = Math.max(150, Math.min(startWidth + delta, window.innerWidth - 420));
      document.body.style.setProperty(prop, next + "px");
    };
    const onUp = () => {
      handle.classList.remove("dragging");
      document.body.classList.remove("resizing");
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      saveLayout();
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  });

  // Double-click restores the default, so a mis-drag is one gesture to undo.
  handle.addEventListener("dblclick", () => {
    document.body.style.removeProperty(prop);
    saveLayout();
  });
}

// toggleWide hides the side panels so the passage gets the whole window. Useful
// on a long section; the sidebar and argument panel are one click away again.
function toggleWide() {
  const hidden = document.body.classList.toggle("wide");
  $("#wide").textContent = hidden ? "show panels" : "hide panels";
}

// --- export ---

// printDocument renders the argument into a print-only container and hands off
// to the browser, whose "Save as PDF" is a better PDF writer than anything
// worth hand-rolling here.
function printDocument() {
  const view = $("#print-view");
  view.innerHTML = "";
  view.hidden = false;

  const h1 = el("h1", null, state.question || "Whetstone");
  view.append(h1);

  for (const n of state.outline) {
    view.append(el(n.depth === 0 ? "h2" : "h3", null, n.title));

    const prose = n.draft || n.notes;
    if (prose) {
      for (const para of splitParagraphs(prose)) {
        view.append(el("p", null, para));
      }
    }
    for (const g of n.grounding) {
      view.append(el("blockquote", null, `${g.excerpt} — §${g.sectionId}`));
    }
  }

  const open = [];
  for (const n of state.outline) {
    for (const p of n.provocations) if (p.status === "open") open.push(p);
  }
  for (const d of state.documents) {
    for (const sec of d.sections) {
      for (const p of sec.provocations) if (p.status === "open") open.push(p);
    }
  }
  if (open.length) {
    view.append(el("h2", null, "Objections still open"));
    for (const p of open) view.append(el("p", null, `${p.label}: ${p.text}`));
  }

  const e = state.engagement;
  const meta = el("div", "meta");
  meta.append(el("p", null,
    `Sections opened ${e.sectionsRead} of ${e.sectionsTotal} · ` +
    `objections ${e.resolved} resolved, ${e.open} open · ` +
    `${Math.round(e.authorship * 100)}% of the words are the author's.`));
  meta.append(el("p", null, e.caveat));
  view.append(meta);

  window.print();
  view.hidden = true;
}

// splitParagraphs breaks prose on blank lines, dropping empties.
function splitParagraphs(text) {
  return String(text)
    .split(/\r?\n\s*\r?\n/)
    .map((p) => p.trim())
    .filter(Boolean);
}

function exportScope() {
  const sel = document.getElementById("export-scope");
  return sel ? sel.value : "all";
}

function downloadDocx() {
  // A navigation, not a fetch: only a real request triggers the save dialog.
  window.location.href = "/export.docx" +
    "?t=" + encodeURIComponent(window.WHETSTONE_TOKEN) +
    "&scope=" + encodeURIComponent(exportScope());
}

// --- wiring ---

$("#add-doc").onclick = showPasteForm;
$("#add-point").onclick = () => showPointForm({});
$("#lens").onchange = (ev) => act("", "/api/lens", { lensId: ev.target.value });
$("#question").onclick = showQuestionForm;
$("#export-docx").onclick = downloadDocx;
$("#export-pdf").onclick = printDocument;
$("#wide").onclick = toggleWide;

loadLayout();
wireResizer("resize-left", "--col-left", "left");
wireResizer("resize-right", "--col-right", "right");

(async function start() {
  try {
    state = await api("/api/state");
    render();
  } catch (err) {
    setStatus(err.message, "error");
  }
})();
