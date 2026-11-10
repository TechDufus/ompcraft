// ompcraft site: HUD behaviour. No dependencies.
(() => {
  "use strict";
  const $ = (s, r = document) => r.querySelector(s);
  const $$ = (s, r = document) => [...r.querySelectorAll(s)];
  const params = new URLSearchParams(location.search);
  const still = params.has("still");
  const reduced = still || matchMedia("(prefers-reduced-motion: reduce)").matches;
  if (still) document.documentElement.classList.add("still");

  // ---- centre-screen message (the game's UIErrorsFrame) -----------------
  const uierror = $(".uierror");
  let uiTimer;
  function say(text) {
    uierror.textContent = text;
    uierror.classList.add("is-on");
    clearTimeout(uiTimer);
    uiTimer = setTimeout(() => uierror.classList.remove("is-on"), 2600);
  }

  async function copy(text) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      const ta = Object.assign(document.createElement("textarea"), { value: text });
      ta.setAttribute("readonly", "");
      ta.style.cssText = "position:fixed;opacity:0";
      document.body.append(ta);
      ta.select();
      const ok = document.execCommand("copy");
      ta.remove();
      return ok;
    }
  }

  const installCmd = $("#install-cmd").textContent.trim();

  $$("[data-copy]").forEach((btn) =>
    btn.addEventListener("click", async () => {
      const ok = await copy($(btn.dataset.copy).textContent.trim());
      say(ok ? "Install command copied. Paste it in a terminal." : "Couldn't copy. Select the command and copy it.");
    })
  );

  // ---- action bar ----------------------------------------------------------
  const slots = Object.fromEntries($$(".slot").map((s) => [s.dataset.action, s]));
  const tips = $$(".tooltip--slot");

  function cooldown(slot) {
    slot.classList.remove("is-cd");
    void slot.offsetWidth; // restart the sweep
    slot.classList.add("is-cd");
    slot.classList.add("is-pressed");
    setTimeout(() => slot.classList.remove("is-pressed"), 120);
  }

  const actions = {
    async install() {
      const ok = await copy(installCmd);
      say(ok ? "Install command copied. Paste it in a terminal." : "Couldn't copy. Scroll down for the command.");
    },
    star(fromKey) {
      if (fromKey) window.open(slots.star.href, "_blank", "noopener");
      say("Opening ompcraft on GitHub.");
    },
    async share() {
      const data = { title: "ompcraft", text: "Your AI coding agents, in your WoW quest log.", url: location.origin + location.pathname };
      if (navigator.share) {
        try { await navigator.share(data); return; } catch (e) { if (e && e.name === "AbortError") return; }
      }
      const ok = await copy(data.url);
      say(ok ? "Link copied. Send it to your guild." : "Couldn't copy the link.");
    },
  };

  Object.entries(slots).forEach(([name, slot]) => {
    slot.addEventListener("click", () => {
      cooldown(slot);
      actions[name](false);
    });
    const tip = $("#" + slot.getAttribute("aria-describedby"));
    const show = () => { tips.forEach((t) => t.classList.toggle("is-on", t === tip)); };
    const hide = () => tip.classList.remove("is-on");
    slot.addEventListener("mouseenter", show);
    slot.addEventListener("focus", show);
    slot.addEventListener("mouseleave", hide);
    slot.addEventListener("blur", hide);
  });

  addEventListener("keydown", (e) => {
    if (e.altKey || e.ctrlKey || e.metaKey || e.repeat) return;
    if (e.target.closest("input, textarea, select, [contenteditable]")) return;
    const name = { 1: "install", 2: "star", 3: "share" }[e.key];
    if (!name) return;
    e.preventDefault();
    cooldown(slots[name]);
    actions[name](true);
  });

  // ---- tracker: collapse, live rows, current section -----------------------
  const tracker = $(".tracker");
  const toggle = $(".tracker__toggle");
  function setCollapsed(c) {
    tracker.classList.toggle("is-collapsed", c);
    toggle.setAttribute("aria-expanded", String(!c));
    toggle.title = c ? "Expand" : "Collapse";
    $(".sr", toggle).textContent = c ? "Expand the tracker" : "Collapse the tracker";
  }
  toggle.addEventListener("click", () => setCollapsed(!tracker.classList.contains("is-collapsed")));
  const narrow = matchMedia("(max-width: 1180px)");
  setCollapsed(narrow.matches && !still);
  narrow.addEventListener("change", (e) => setCollapsed(e.matches));
  $$(".tracker__rows a, .tracker__idle a").forEach((a) =>
    a.addEventListener("click", () => { if (narrow.matches) setCollapsed(true); })
  );

  const fmt = (s) => (s < 60 ? s + "s" : Math.floor(s / 60) + "m");
  if (!still) {
    const ticks = $$("[data-tick]").map((el) => ({ el, s: +el.dataset.tick }));
    setInterval(() => ticks.forEach((t) => { t.s += 1; t.el.textContent = fmt(t.s); }), 1000);

    $$("[data-cycle]").forEach((el, i) => {
      const lines = el.dataset.cycle.split("|");
      let n = 0;
      setInterval(() => {
        n = (n + 1) % lines.length;
        if (reduced) { el.textContent = "- " + lines[n]; return; }
        el.classList.add("is-swapping");
        setTimeout(() => { el.textContent = "- " + lines[n]; el.classList.remove("is-swapping"); }, 350);
      }, 4200 + i * 900);
    });
  }

  // ---- the hero pop-ups: real in-game plates, a deterministic loop ---------
  const alerts = $(".alerts");
  const plate = $("[data-plate]", alerts);
  const script = ["finished", "needs", "failed", "exited"];
  const sizes = { finished: [302, 92], needs: [248, 75], failed: [248, 75], exited: [248, 75] };
  const plateSrc = (kind) => "img/popups/" + kind + ".png";
  script.forEach((k) => { new Image().src = plateSrc(k); });
  function paint(kind) {
    const [w, h] = sizes[kind];
    plate.src = plateSrc(kind);
    plate.width = w; plate.height = h;
    alerts.style.setProperty("--w", w + "px");
    alerts.style.setProperty("--ar", w + " / " + h);
    alerts.style.setProperty("--plate", `url(${plateSrc(kind)})`);
    plate.style.setProperty("--w", w + "px");
    if (!reduced) { alerts.classList.remove("is-shine"); void alerts.offsetWidth; alerts.classList.add("is-shine"); }
  }
  paint("finished");
  // ---- light motes drifting through the hero's sunlight ---------------------
  const canvas = $(".motes");
  if (canvas) {
    const ctx = canvas.getContext("2d");
    let seed = 7;
    const rand = () => ((seed = (seed * 16807) % 2147483647) / 2147483647); // deterministic
    const motes = Array.from({ length: 90 }, () => ({
      x: rand(), y: rand(), r: 0.6 + rand() * 2.2, v: 0.004 + rand() * 0.012,
      drift: rand() * Math.PI * 2, tw: rand() * Math.PI * 2,
    }));
    let w = 0, h = 0, dpr = 1, visible = true, last = 0;
    function size() {
      dpr = Math.min(devicePixelRatio || 1, 2);
      w = canvas.clientWidth; h = canvas.clientHeight;
      canvas.width = Math.round(w * dpr); canvas.height = Math.round(h * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    }
    function draw(t) {
      ctx.clearRect(0, 0, w, h);
      for (const m of motes) {
        const x = (m.x + Math.sin(t / 4000 + m.drift) * 0.012) * w;
        const y = m.y * h;
        const a = 0.25 + 0.55 * (0.5 + 0.5 * Math.sin(t / 900 + m.tw));
        const g = ctx.createRadialGradient(x, y, 0, x, y, m.r * 4);
        g.addColorStop(0, `rgba(255, 236, 170, ${a})`);
        g.addColorStop(0.35, `rgba(255, 196, 80, ${a * 0.45})`);
        g.addColorStop(1, "rgba(255, 170, 40, 0)");
        ctx.fillStyle = g;
        ctx.beginPath(); ctx.arc(x, y, m.r * 4, 0, Math.PI * 2); ctx.fill();
      }
    }
    function frame(t) {
      const dt = last ? Math.min(t - last, 64) : 16;
      last = t;
      for (const m of motes) {
        m.y -= m.v * dt / 1000;
        if (m.y < -0.02) { m.y = 1.02; m.x = rand(); }
      }
      draw(t);
      if (visible) requestAnimationFrame(frame);
    }
    size();
    addEventListener("resize", size);
    if (reduced) {
      draw(0);
    } else {
      new IntersectionObserver(([en]) => {
        const was = visible;
        visible = en.isIntersecting;
        if (visible && !was) { last = 0; requestAnimationFrame(frame); }
      }).observe(canvas);
      requestAnimationFrame(frame);
    }
  }

  const pick = params.get("toast");
  if (pick && script.includes(pick)) {
    paint(pick);
    plate.classList.add("is-in");
  } else if (!still) {
    let i = 0;
    setInterval(() => {
      plate.classList.add("is-out");
      setTimeout(() => {
        i = (i + 1) % script.length;
        paint(script[i]);
        plate.classList.remove("is-in", "is-out");
        void plate.offsetWidth;
        plate.classList.add("is-in");
      }, reduced ? 0 : 480);
    }, 5200);
  }

  // ---- zoning: the world changes as you travel -----------------------------
  // Two layers: the next zone loads into the hidden one, then they swap.
  const layers = $$(".world__img");
  let front = 0;
  const plaque = $(".plaque");
  const plaqueName = $("[data-zone-name]", plaque);
  const rows = Object.fromEntries($$(".tracker__rows a").map((a) => [a.dataset.row, a]));
  let current = "mulgore";
  function enter(section) {
    const zone = section.dataset.zone;
    const name = section.dataset.zoneName || "Mulgore";
    Object.values(rows).forEach((a) => a.removeAttribute("aria-current"));
    if (rows[section.id]) rows[section.id].setAttribute("aria-current", "true");
    if (zone === current) return;
    current = zone;
    document.body.dataset.zone = zone;
    const back = layers[1 - front];
    const show = () => {
      back.onload = null;
      if (current !== zone) return; // the visitor moved on while it loaded
      back.classList.add("is-on");
      layers[front].classList.remove("is-on");
      front = 1 - front;
    };
    back.onload = show;
    back.src = "img/world/" + zone + ".jpg";
    if (back.complete && back.naturalWidth) show();
    plaque.classList.add("is-swapping");
    setTimeout(() => { plaqueName.textContent = name; plaque.classList.remove("is-swapping"); }, reduced ? 0 : 260);
  }
  document.body.dataset.zone = current;
  const io = new IntersectionObserver(
    (entries) => entries.forEach((en) => en.isIntersecting && enter(en.target)),
    { rootMargin: "-45% 0px -45% 0px" }
  );
  $$("main > section").forEach((s) => io.observe(s));
})();
