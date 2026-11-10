---
version: 1
slug: "site-index-html"
primary_target: "site/index.html"
related_targets: []
---

## Scope

Marketing landing page for ompcraft (`site/index.html`), mode Persuade. Also the source for the dressed-up README images, which are captured from the page itself (`?still` freezes motion, `?toast=<kind>` pins a pop-up).

## Audience, job, action

- **Audience:** omp users who play WoW: Forever, AI-curious devs and gamers who share cool hacks, and WoW addon players and authors.
- **Primary outcome:** wowed in one screen, then share. Secondary actions: copy the install line, star the repo.

## Proof and content

- Real in-game captures with demo data: `docs/media/{hero,detail,toast,settings}.png`.
- Official Blizzard Forever zone screenshots as unaltered world backdrops, with a © notice.
- Live HTML recreations of the tracker and pop-ups, using demo data labeled as demo.
- The unofficial / not-approved disclosure and the trademark footer must stay.

## Direction contract

**THESIS:** The website is a WoW screen. It refuses the dev-tool hero (headline, install box, framed screenshot, feature grid). Instead the visitor is standing in Azeroth, and ompcraft is already running.

**OWN-WORLD:**
- The ground is a full-bleed in-game world (official Forever zone shot, Mulgore at sunset in the first viewport). Over it sits recreated WoW HUD chrome: gold Friz-like display caps, the objective tracker's gold headers on a soft dark wash, achievement pop-ups in bronze and gold, GameTooltip panels (deep navy, 1px light border, gold title), and a parchment quest-log panel.
- Palette: tracker gold #ffd100, quest yellow #f5b400, tooltip navy #090c1a, parchment #e8d5a9, ink #2b1a0b, error red #ff2020.
- No cards, no gradients-as-decoration.

**STORY:**
1. You arrive like entering a zone: the "ompcraft" zone text fades in, with "Your AI coding agents, in your quest log" beneath it.
2. A live tracker shows agents working, and a pop-up announces that one finished.
3. Scrolling: zone captures carry the tracker, agent page and pop-ups, each explained in tooltips. "How it works" is a quest-log entry. Safety appears as the red-text tooltip truths.
4. The action bar closes the page.

**FIRST VIEWPORT:**
- Mulgore sunset, full bleed.
- Top-left: the live ompcraft tracker (working nav: each site section is an agent row with a quest marker).
- Upper center: the zone-text title, about 5.5rem.
- Right of center: an achievement pop-up that slides in on a loop.
- Bottom center: the action bar. Slot 1 Install (copies `curl … | sh`), 2 Star, 3 Share. Keys 1/2/3 work.
- The disclaimer line sits under the bar like the game's version string.

**FORM:** The WoW HUD as an interface language (pick card, seed:12eec2ae05198c21). Native motion: zone-text fade, pop-up slide with a shine sweep, tracker rows updating. Damped, nothing snaps.

**SIGNATURE:** Pressing 1/2/3 (or clicking) fires the action bar's cooldown sweep. Install copies and shows a yellow UI error-frame style "Copied to clipboard" message mid-screen, like the game's UIErrorsFrame.

## Unresolved

- Blizzard images are used unaltered under the fan-site FAQ's non-commercial license. The user is informed; replace them with the user's own captures if wanted.
