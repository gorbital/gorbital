// API reference and docs pages: theme toggle, code tabs and copy, search,
// the sidebar menu and filter, the table of contents and "Try it". No
// dependencies; pages read fine without it.
(() => {
  const root = document.documentElement;
  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];
  const store = {
    get(k) { try { return localStorage.getItem(k); } catch { return null; } },
    set(k, v) { try { localStorage.setItem(k, v); } catch { /* private mode */ } },
  };

  // Theme: follows the system until the reader picks one.
  $$("[data-theme-toggle]").forEach((btn) => btn.addEventListener("click", () => {
    const current = root.dataset.theme || (matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark");
    const next = current === "dark" ? "light" : "dark";
    root.dataset.theme = next;
    store.set("theme", next);
  }));

  $$("[data-origin]").forEach((el) => { el.textContent = location.origin; });

  function flash(btn, text) {
    if (!btn.dataset.label) btn.dataset.label = btn.textContent;
    btn.textContent = text;
    clearTimeout(btn._flash);
    btn._flash = setTimeout(() => { btn.textContent = btn.dataset.label; }, 1500);
  }
  function copyText(text, btn) {
    if (!navigator.clipboard) { flash(btn, "Select to copy"); return; }
    navigator.clipboard.writeText(text).then(() => flash(btn, "Copied"), () => flash(btn, "Select to copy"));
  }
  const codeText = (pre) => pre.textContent.replace(/\n$/, "").replace(/^\$ /gm, "");

  // Consecutive code blocks inside .code-group become tabs.
  $$(".code-group").forEach((group) => {
    const blocks = $$(":scope > .code", group);
    if (blocks.length < 2) return;
    const head = document.createElement("div");
    head.className = "code-head";
    head.setAttribute("role", "tablist");
    blocks.forEach((block, i) => {
      const tab = document.createElement("button");
      tab.type = "button";
      tab.className = "tab";
      tab.setAttribute("role", "tab");
      tab.setAttribute("aria-selected", String(i === 0));
      tab.textContent = $(".fname", block)?.textContent || "Tab " + (i + 1);
      tab.addEventListener("click", () => {
        $$(".tab", head).forEach((t) => t.setAttribute("aria-selected", String(t === tab)));
        blocks.forEach((b) => { b.hidden = b !== block; });
      });
      head.append(tab);
      $(".code-head", block)?.remove();
      block.hidden = i !== 0;
    });
    const copy = document.createElement("button");
    copy.type = "button";
    copy.className = "copy";
    copy.textContent = "Copy";
    head.append(copy);
    group.prepend(head);
  });

  function copyPage(btn) {
    const text = fetch(btn.dataset.copyPage).then((r) => {
      if (!r.ok) throw new Error(String(r.status));
      return r.text();
    });
    if (window.ClipboardItem && navigator.clipboard?.write) {
      const item = new ClipboardItem({ "text/plain": text.then((s) => new Blob([s], { type: "text/plain" })) });
      navigator.clipboard.write([item]).then(() => flash(btn, "Copied"), () => flash(btn, "Couldn't copy"));
    } else {
      text.then((s) => copyText(s, btn), () => flash(btn, "Couldn't copy"));
    }
  }

  document.addEventListener("click", (e) => {
    const t = e.target;

    const tab = t.closest("[data-tabs] .tab[data-tab]");
    if (tab) {
      const box = tab.closest("[data-tabs]");
      $$(".tab", box).forEach((x) => x.setAttribute("aria-selected", String(x === tab)));
      $$("[data-pane]", box).forEach((p) => { p.hidden = p.dataset.pane !== tab.dataset.tab; });
      return;
    }

    const copy = t.closest("button.copy:not([data-try-close])");
    if (copy) {
      if (copy.dataset.copyText) { copyText(copy.dataset.copyText, copy); return; }
      const group = copy.closest(".code-group");
      const box = group ? $$(":scope > .code", group).find((b) => !b.hidden) : copy.closest(".code");
      const pre = box && $$("pre", box).find((p) => !p.hidden);
      if (pre) copyText(codeText(pre), copy);
      return;
    }

    const page = t.closest("[data-copy-page]");
    if (page) { copyPage(page); return; }

    const menu = t.closest("[data-menu]");
    if (menu) {
      const side = document.getElementById(menu.getAttribute("aria-controls"));
      const open = side.classList.toggle("open");
      menu.setAttribute("aria-expanded", String(open));
      menu.textContent = open ? "Close" : "Menu";
      return;
    }

    if (t.closest("[data-search]")) { openSearch(); return; }

    const helpful = t.closest("[data-helpful]");
    if (helpful) {
      const box = helpful.closest("[data-feedback]");
      $(".btns", box).hidden = true;
      if (helpful.dataset.helpful === "yes") {
        $("[data-feedback-q]", box).textContent = "Thanks. That tells us to keep this page as it is.";
      } else {
        $("[data-feedback-q]", box).textContent = "Thanks. Tell us what was missing or wrong:";
        $("[data-feedback-issue]", box).hidden = false;
      }
    }
  });

  // Keep the current sidebar entry in view.
  const side = $(".dside");
  const current = side && $("[aria-current='page']", side);
  if (current) side.scrollTop = current.offsetTop - side.clientHeight / 2;

  // Endpoint filter.
  const filter = $("[data-nav-filter]");
  filter?.addEventListener("input", () => {
    const q = filter.value.trim().toLowerCase();
    $$("[data-nav-group]").forEach((g) => {
      let any = false;
      $$("li", g).forEach((li) => {
        const hit = !q || li.textContent.toLowerCase().includes(q) || li.querySelector("a").pathname.includes(q);
        li.hidden = !hit;
        any ||= hit;
      });
      g.hidden = !any;
    });
  });

  // Table of contents follows the reading position.
  const tocLinks = $$(".dtoc a[href^='#']");
  if (tocLinks.length && "IntersectionObserver" in window) {
    const byId = new Map(tocLinks.map((a) => [decodeURIComponent(a.hash.slice(1)), a]));
    const observer = new IntersectionObserver((entries) => {
      for (const en of entries) {
        if (en.isIntersecting) tocLinks.forEach((a) => a.classList.toggle("on", a === byId.get(en.target.id)));
      }
    }, { rootMargin: "-110px 0px -70% 0px" });
    byId.forEach((_, id) => { const h = document.getElementById(id); if (h) observer.observe(h); });
  }

  // Search: loads the index on first use.
  const dlg = $("#search");
  const input = $("#search-input");
  const list = $("#search-list");
  let index = null;
  let results = [];
  let sel = 0;

  async function loadIndex() {
    if (index) return index;
    try {
      const r = await fetch(dlg.dataset.index || "/search.json");
      index = await r.json();
      for (const e of index) { e._t = e.t.toLowerCase(); e._x = e.x.toLowerCase(); e._p = (e.m ? e.m + " " + e.p : "").toLowerCase(); }
    } catch {
      index = [];
    }
    return index;
  }

  function find(q) {
    q = q.trim().toLowerCase();
    if (!q) return index.slice(0, 8).map((e) => ({ e }));
    const words = q.split(/\s+/);
    const out = [];
    for (const e of index) {
      let score = 0;
      let head = null;
      if (e._t === q) score = 100;
      else if (e._t.startsWith(q)) score = 80;
      else if (words.every((w) => e._t.includes(w))) score = 60;
      else if (e._p && e._p.includes(q)) score = 55;
      if (score < 45 && e.h) {
        const h = e.h.find(([, text]) => words.every((w) => text.toLowerCase().includes(w)));
        if (h) { score = 45; head = h; }
      }
      if (!score && words.every((w) => e._x.includes(w))) score = 10;
      if (score) out.push({ e, head, score });
    }
    return out.sort((a, b) => b.score - a.score).slice(0, 30);
  }

  function render() {
    results = find(input.value);
    sel = Math.min(sel, Math.max(results.length - 1, 0));
    list.replaceChildren();
    if (!results.length) {
      const li = document.createElement("li");
      li.className = "sd-empty";
      li.textContent = "Nothing matches “" + input.value.trim() + "”. Try a path, a field or an error code.";
      list.append(li);
      return;
    }
    let group = "";
    results.forEach((r, i) => {
      const section = r.e.s.split(" · ")[0];
      if (section !== group) {
        group = section;
        const g = document.createElement("li");
        g.className = "grp";
        g.textContent = group;
        list.append(g);
      }
      const li = document.createElement("li");
      const a = document.createElement("a");
      a.href = r.e.u + (r.head ? "#" + r.head[0] : "");
      if (i === sel) a.className = "sel";
      const title = document.createElement("span");
      title.textContent = r.e.t;
      if (r.head) {
        const small = document.createElement("small");
        small.textContent = " › " + r.head[1];
        title.append(small);
      }
      const hint = document.createElement("code");
      hint.textContent = r.e.m ? r.e.m + " " + r.e.p : r.e.s;
      a.append(title, hint);
      li.append(a);
      list.append(li);
    });
    $("a.sel", list)?.scrollIntoView({ block: "nearest" });
  }

  async function openSearch() {
    if (!dlg || dlg.open) return;
    dlg.showModal();
    input.value = "";
    sel = 0;
    await loadIndex();
    render();
    input.focus();
  }

  if (dlg) {
    input.addEventListener("input", () => { sel = 0; render(); });
    input.addEventListener("keydown", (e) => {
      if (e.key === "ArrowDown") { e.preventDefault(); sel = Math.min(sel + 1, results.length - 1); render(); }
      if (e.key === "ArrowUp") { e.preventDefault(); sel = Math.max(sel - 1, 0); render(); }
      if (e.key === "Enter") { const a = $$("a", list)[sel]; if (a) { e.preventDefault(); location.href = a.href; } }
    });
    dlg.addEventListener("click", (e) => { if (e.target === dlg) dlg.close(); });
    document.addEventListener("keydown", (e) => {
      const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement?.tagName) || document.activeElement?.isContentEditable;
      if (((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") || (e.key === "/" && !typing)) {
        e.preventDefault();
        openSearch();
      }
    });
  }

  // Try it: sends the request from the reader's browser.
  const form = $("#try");
  if (form) {
    const sameOrigin = form.hasAttribute("data-same-origin");
    const toggle = $("[data-try-toggle]");
    const saved = store.get("try-server");
    if (saved && form.elements.server) form.elements.server.value = saved;
    // A token saved from an earlier "Try it" call (this page or another
    // endpoint's) carries over, so signing in once fills every other
    // endpoint's Bearer token field too. It never leaves this browser.
    const savedToken = store.get("try-token");
    if (savedToken && form.elements.token) form.elements.token.value = savedToken;
    if (form.elements.token) {
      form.elements.token.addEventListener("input", () => {
        store.set("try-token", form.elements.token.value.trim());
      });
    }
    $("[data-try-clear-token]", form)?.addEventListener("click", () => {
      store.set("try-token", "");
      if (form.elements.token) form.elements.token.value = "";
    });
    const setOpen = (open) => {
      form.hidden = !open;
      toggle.setAttribute("aria-expanded", String(open));
      if (open) {
        form.scrollIntoView({ block: "nearest" });
        $("input, textarea", form)?.focus();
      }
    };
    toggle.addEventListener("click", () => setOpen(form.hidden));
    $("[data-try-close]", form).addEventListener("click", () => setOpen(false));
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      let server = location.origin;
      if (form.elements.server) {
        server = form.elements.server.value.trim().replace(/\/+$/, "");
        store.set("try-server", server);
      }
      let path = form.dataset.path;
      const query = new URLSearchParams();
      const headers = {};
      $$("[data-in]", form).forEach((el) => {
        const v = el.value.trim();
        if (!v) return;
        if (el.dataset.in === "path") path = path.replace("{" + el.dataset.name + "}", encodeURIComponent(v));
        else if (el.dataset.in === "query") query.set(el.dataset.name, v);
        else if (el.dataset.in === "header") headers[el.dataset.name] = v;
      });
      const token = form.elements.token?.value.trim();
      if (token) headers.Authorization = "Bearer " + token;
      const body = form.elements.body?.value.trim();
      if (body) headers["Content-Type"] = "application/json";
      const qs = query.toString();
      const url = server + path + (qs ? "?" + qs : "");
      const note = $("[data-try-note]", form);
      const result = $("[data-try-result]", form);
      note.textContent = "Sending " + form.dataset.method + " " + url;
      const started = performance.now();
      try {
        const res = await fetch(url, { method: form.dataset.method, headers, body: body || undefined, credentials: sameOrigin ? "same-origin" : "omit" });
        const text = await res.text();
        let shown = text;
        let parsed;
        try { parsed = JSON.parse(text); shown = JSON.stringify(parsed, null, 2); } catch { /* not JSON */ }
        // A response with a top-level "token" (login with transport:
        // "bearer", or a refresh) fills the Bearer token field here and on
        // every other endpoint's "Try it" panel, so signing in once is
        // enough even when testing bearer auth instead of the cookie.
        if (parsed && typeof parsed.token === "string" && parsed.token) {
          store.set("try-token", parsed.token);
          if (form.elements.token) form.elements.token.value = parsed.token;
        }
        $("[data-try-status]", form).textContent = res.status + " " + res.statusText + " · " + Math.round(performance.now() - started) + " ms";
        $("[data-try-body]", form).textContent = shown || "(empty body)";
        result.hidden = false;
        note.textContent = form.dataset.method + " " + url;
      } catch {
        result.hidden = true;
        note.textContent = sameOrigin
          ? "The request failed before a response arrived. Check the API is still running."
          : "Couldn't reach " + server + ". Check the API is running and allows " + location.origin + " for CORS.";
      }
    });
  }
})();
