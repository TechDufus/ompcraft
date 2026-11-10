---
name: ompcraft
description: "The marketing site as a World of Warcraft screen: recreated game HUD chrome over a fixed full-bleed zone backdrop."
colors:
  quest-gold: "#ffd100"
  gold-deep: "#c98a00"
  bronze: "#8a6a2c"
  bronze-bright: "#e2bd6a"
  objective-yellow: "#f0c800"
  link-gold: "#ffe27a"
  night: "#06080b"
  tooltip-navy: "#080b18ed"
  tooltip-edge: "#a7a9b0"
  parchment: "#ead8ad"
  parchment-dark: "#cdb07a"
  ink: "#2b1a0b"
  ink-heading: "#3a2306"
  text: "#f3ead6"
  text-dim: "#c9c2b2"
  use-green: "#22ff1a"
  alert-red: "#ff4a3d"
  chat-system: "#ff9b3d"
  chat-whisper: "#f2a6ff"
  chat-link: "#71d5ff"
  quest-link: "#7b2b06"
  objective-done: "#1d5a07"
typography:
  display:
    fontFamily: "Marcellus, Iowan Old Style, Georgia, serif"
    fontSize: "clamp(3.6rem, 9vw, 6rem)"
    fontWeight: 400
    lineHeight: 1
    letterSpacing: "0.015em"
  headline:
    fontFamily: "Marcellus, Iowan Old Style, Georgia, serif"
    fontSize: "clamp(2.1rem, 3.8vw, 3.2rem)"
    fontWeight: 400
    lineHeight: 1.08
  title:
    fontFamily: "Marcellus, Iowan Old Style, Georgia, serif"
    fontSize: "clamp(1.25rem, 2.6vw, 1.7rem)"
    fontWeight: 400
    lineHeight: 1.25
  body:
    fontFamily: "Alegreya Sans, Gill Sans, Segoe UI, sans-serif"
    fontSize: "1.125rem"
    fontWeight: 400
    lineHeight: 1.6
  tooltip-text:
    fontFamily: "Alegreya Sans, Gill Sans, Segoe UI, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.45
  label:
    fontFamily: "Alegreya Sans, Gill Sans, Segoe UI, sans-serif"
    fontSize: "0.98rem"
    fontWeight: 500
    lineHeight: 1.35
    letterSpacing: "0.01em"
  caption:
    fontFamily: "Alegreya Sans, Gill Sans, Segoe UI, sans-serif"
    fontSize: "0.9rem"
    fontWeight: 400
    lineHeight: 1.35
  code:
    fontFamily: "JetBrains Mono, ui-monospace, monospace"
    fontSize: "0.88em"
    fontWeight: 500
rounded:
  hairline: "3px"
  sm: "4px"
  slot: "5px"
  panel: "6px"
  bar: "10px"
spacing:
  xs: "4px"
  sm: "6px"
  md: "10px"
  lg: "16px"
  hud: "20px"
  hud-narrow: "12px"
  stack: "22px"
  tracker-width: "300px"
components:
  tracker-row:
    textColor: "{colors.objective-yellow}"
    typography: "{typography.label}"
    rounded: "{rounded.hairline}"
    padding: "4px 6px 5px"
  tracker-row-active:
    textColor: "#fff1a6"
    backgroundColor: "#ffd1001a"
  tracker-section-header:
    textColor: "{colors.quest-gold}"
    typography: "{typography.label}"
  zone-plaque:
    backgroundColor: "#080603c7"
    textColor: "{colors.quest-gold}"
    rounded: "{rounded.hairline}"
    padding: "4px 16px 5px"
  tooltip:
    backgroundColor: "{colors.tooltip-navy}"
    textColor: "{colors.text}"
    typography: "{typography.tooltip-text}"
    rounded: "{rounded.panel}"
    padding: "12px 16px 14px"
  tooltip-item:
    backgroundColor: "{colors.tooltip-navy}"
    textColor: "{colors.text}"
    rounded: "{rounded.panel}"
    padding: "22px 26px"
  action-slot:
    backgroundColor: "#1d1206"
    textColor: "#ffffff"
    rounded: "{rounded.slot}"
    size: "62px"
  action-slot-hover:
    backgroundColor: "#3b2a14"
    textColor: "#ffffff"
  action-bar:
    backgroundColor: "#18120ae6"
    rounded: "{rounded.bar}"
    padding: "12px 18px 10px"
  chat-panel:
    backgroundColor: "#00000075"
    textColor: "#ffef7a"
    rounded: "{rounded.sm}"
    padding: "8px 12px"
  chat-box:
    backgroundColor: "#020305c7"
    textColor: "#ffffff"
    typography: "{typography.code}"
    rounded: "{rounded.sm}"
    padding: "8px 8px 8px 12px"
  copy-button:
    backgroundColor: "#8d1408"
    textColor: "{colors.quest-gold}"
    typography: "{typography.label}"
    rounded: "{rounded.sm}"
    padding: "5px 12px"
  copy-button-hover:
    backgroundColor: "#8d1408"
    textColor: "{colors.quest-gold}"
  quest-log:
    backgroundColor: "{colors.parchment}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    padding: "46px 60px 40px"
  ui-error-line:
    textColor: "{colors.quest-gold}"
    typography: "{typography.title}"
---

# Design System: ompcraft

## Overview

**Creative North Star: "The HUD Is the Page"**

The site is a WoW screen, not a page about one. A single fixed, full-bleed in-game zone image is the ground; it crossfades to the next zone as the visitor scrolls (Mulgore, The Barrens, Ashenvale, Felwood, Darkshore, Dustwallow Marsh, back to Mulgore). Everything that carries information is recreated game chrome floating over it: a tracker for navigation, a zone plaque, tooltips, a chat window, a parchment quest log, an action bar. The page should read as "it shipped with the game", which is also the product's first principle (native over novel).

Mood: warm, lit-from-within, a little ceremonial. Gold carries identity and hierarchy; everything else is dark glass over photography, with one parchment panel as the sole light surface. Density is HUD-dense in the chrome (tight rows, 1rem text) and generous in the world (92svh legs, wide gutters so the zone art breathes). Motion is damped and game-native (zone text fades in with a letter-spacing settle, pop-ups slide and shine, tracker rows update); nothing snaps.

The system carries one durable product commitment: the unofficial / not-affiliated disclosure and the Blizzard trademark footer stay visible, in the game's own idiom (the version line under the action bar, a footer). Real in-game captures of demo agents are the proof; their own window frames are the frame, so the site adds none.

**Key Characteristics:**
- A fixed photographic world, never a flat colour field; chrome floats above it.
- Gold display type in Marcellus, outlined with black text-shadow so it holds on any zone image.
- Dark navy tooltips with a 1px grey-silver edge are the only boxed content container.
- One parchment surface (the quest log) for the single "read this carefully" explanation.
- WoW's colour language is semantic: gold = objective/title, green = "Use:" lines, red = warning, cyan = clickable chat link.
- Hand-drawn gold/red SVG markers and action icons with dark outlines; no icon font, no glyph icons.

## Colors

A game-UI palette: saturated tracker gold and bronze frame metal over a near-black night, with tooltip navy, one parchment, and four chat/semantic hues borrowed from the game itself.

### Primary
- **Quest-Tracker Gold** (#ffd100): zone title, h2 headings, tracker header, plaque text, section headers, selection background, focus outline, tooltip flavor lines, `.y` emphasis. The identity colour.
- **Objective Yellow** (#f0c800): tracker row titles (brighten to #fff1a6 on hover/current).
- **Link Gold** (#ffe27a): body links on the world (hover #fff3b0); also the slot hover ring.
- **Deep Gold** (#c98a00): base stop of the gold sprite gradient and the gold marker fills.

### Secondary
- **Frame Bronze** (#8a6a2c): borders of the plaque and action bar, scrollbar thumb, slot inner ring; the "metal" of every frame.
- **Bright Bronze** (#e2bd6a): the lit end of the fading hairline under the tracker header and section header.

### Tertiary
- **Use-Line Green** (#22ff1a): "Use:" lines in tooltips and the reply-privacy tooltip line (`.g`). Strictly the game's tooltip green.
- **Warning Red** (#ff4a3d): "Failed or exited" and the unofficial / not-approved line (`.r`).
- **Chat System Orange** (#ff9b3d): the `[ompcraft]` channel tag.
- **Chat Whisper Pink** (#f2a6ff): the whisper-coloured install caveat line (links #ffd1ff).
- **Chat Link Cyan** (#71d5ff): clickable chat links that copy the install command (hover #b6eaff).

### Neutral
- **Night** (#06080b): the page background behind the world, scrollbar track.
- **Tooltip Navy** (rgba(8,11,24,.93), #080b18ed): GameTooltip fill; edge **Silver Edge** (#a7a9b0).
- **Parchment** (#ead8ad, darker #cdb07a): quest-log panel, painted by `parchment.webp`, a tinted paper scan whose torn edge and aged rim are baked into its alpha. Text **Ink** (#2b1a0b), headings #3a2306, links **Scorched Brown** (#7b2b06), objective counts **Objective Green** (#1d5a07).
- **Warm Paper** (#f3ead6) body text on dark; **Dim Paper** (#c9c2b2) captions and secondary lines.

### Named Rules
**The Semantic Colour Rule.** Gold, green, red and cyan each keep their in-game meaning (title/objective, Use:, warning, clickable link). Never use one as decoration for another's job.
**The One Light Surface Rule.** Parchment is the only light panel and appears once, for the quest-log explanation. Everything else is dark glass over the world.
**The World Provides the Colour Rule.** Backgrounds are zone photography under a dark scrim. Don't add flat colour fields or tinted section bands.

## Typography

**Display Font:** Marcellus (with Iowan Old Style, Georgia, serif), self-hosted woff2.
**Body Font:** Alegreya Sans (with Gill Sans, Segoe UI, sans-serif), self-hosted at 400, 500, 700.
**Mono Font:** JetBrains Mono (with ui-monospace), variable 100-800, for commands and code only.

**Character:** Marcellus stands in for the game's engraved Friz-style display caps: classical, calm, high-contrast against photography. Alegreya Sans is the humanist reading face that sits well in tooltip and chat text. Fonts are swapped (`font-display: swap`), Marcellus is preloaded.

### Hierarchy
- **Display** (400, clamp(3.6rem, 9vw, 6rem), 1, +.015em tracking): the single zone-text `h1`, gold with a black outline-glow; fades in with a letter-spacing settle.
- **Headline** (400, clamp(2.1rem, 3.8vw, 3.2rem), 1.08, balanced wrap): each section `h2`, gold with the same outline.
- **Title** (400, clamp(1.25rem, 2.6vw, 1.7rem), 1.25): zone sub-line (#fff4cf), tracker title (1.15rem), quest-log `h3` (1.35rem), centre-screen message (1.35rem), big tooltip title (clamp(1.9rem, 3.2vw, 2.6rem), white).
- **Body** (Alegreya 400, 1.125rem, 1.6; 1.05rem under 640px): running copy, capped at 46ch in text columns (60ch narrow, 44ch hero lede at 500 weight).
- **Tooltip text** (400, 1rem, 1.45): tooltip, chat, and legend copy; titles at 500 / 1.08rem in white.
- **Label** (500, .98rem, +.01em): tracker row titles; slot key numerals 700 / .82rem; action-slot names 500 / .72rem; copy button 500 / .9rem. Sentence case, no uppercase.
- **Caption** (400, .88-.92rem, #c9c2b2): screenshot figcaptions, footer and version line (.82rem).
- **Code** (JetBrains Mono 500, .88em): install command, `ompcraft` CLI and slash commands.

### Named Rules
**The Outline Rule.** Any text set directly on the world carries a black text-shadow outline (`0 0 1px #000, 0 1px 1px #000, 0 0 3px rgba(0,0,0,.9)` for small HUD text; heavier glow for headings). Text inside tooltips and parchment has none.
**The Display Is Gold Rule.** Marcellus is gold on the world and white inside tooltips. Don't set display type in body colour or in other hues.

## Layout

Two modes. The chrome is `position: fixed` and pinned to the screen edges with a 20px HUD margin (12px under 640px): tracker top-left (300px wide, 260px under 1180px), zone plaque top-right, action bar bottom-centre, centre-screen message at 9vh. The content ("legs") scrolls underneath.

Each leg is a two-column grid (`1fr 1fr`; `.8fr/1.2fr` for the wide settings leg) with a text column and a figure column, alternating sides via `leg--flip`. The left padding is tracker width + HUD + 5vw so text never sits under the tracker; the gap is clamp(32px, 5vw, 80px). Legs are min-height 92svh with 16vh vertical padding, giving each zone room to show. The hero is `min-height: 100svh`, centred, with the pop-up (right of centre, 58% down) and chat line (bottom-left, 132px up) absolutely placed. The quest-log leg (760px max), the safety tooltip (680px) and the close leg (860px) are single centred columns.

Spacing is irregular and HUD-scaled rather than a strict grid: 4-6px inside tracker rows, 8-12px in chat and tooltip padding, 16-26px in item tooltips, 22px between stacked pop-ups. Section-level rhythm is expressed in vh, not px.

Responsive: at 1180px and below the legs collapse to one column (centred, 14vh/6vw), the tracker collapses to its header by default and re-closes on link click, the pop-up and chat join the flow, and the world scrim becomes a uniform top-to-bottom wash. A short-desktop query (≥1181px, ≤820px tall) also moves the pop-up and chat into the flow. Under 640px the action slots shrink to 54px and screenshots scale to 88vw.

## Elevation & Depth

Depth is hybrid: layered photographic planes (fixed world, scrim, chrome) plus a restrained shadow vocabulary used only on HUD chrome and captured images. Shadows are ambient black, large and low-opacity; a 1px inset black line inside bordered frames mimics the game's inner bevel. There are no hard offset shadows and no coloured box glows; the only luminous effects are the pop-up shine sweep and the drifting gold motes.

### Shadow Vocabulary
- **Plaque** (`inset 0 0 0 1px rgba(0,0,0,.8), 0 4px 12px rgba(0,0,0,.4)`): zone plaque.
- **Tooltip** (`inset 0 0 0 1px rgba(0,0,0,.9), 0 12px 28px rgba(0,0,0,.5)`): every tooltip and the big item tooltip.
- **Action bar** (`inset 0 1px 0 rgba(255,220,140,.25), 0 -6px 24px rgba(0,0,0,.45)`): top edge catches light.
- **Action slot** (`inset 0 0 0 1px #8a6a2c, inset 0 0 12px rgba(0,0,0,.8), 0 2px 4px rgba(0,0,0,.6)`); hover swaps the ring to `inset 0 0 0 2px #ffe27a`.
- **Capture** (`box-shadow: 0 30px 70px rgba(0,0,0,.65)` on screenshots; `drop-shadow(0 12px 18px rgba(0,0,0,.55))` on pop-up plates; `drop-shadow(0 30px 40px rgba(0,0,0,.6))` on the parchment): lifts real captures and the parchment off the zone.
- **Marker** (`drop-shadow(0 1px 1px #000)` on tracker marks; `0 2px 2px rgba(0,0,0,.8)` on slot icons).

### Named Rules
**The Scrim-Not-Decoration Rule.** The world scrim (left-weighted dark gradient, bottom fade) and the tracker/bar washes exist to hold text legible over photography and to echo the game's own fades. Gradients appear only as this scrim, as frame metal, and as the sprite gold/red fills; never as decorative colour.
**The Game Frame Rule.** Frames are 1px bronze (chrome) or 1px silver (tooltips) with an inner black hairline. Never a thicker border, never a coloured card outline.

## Shapes

Small, nearly-square corners like the game's UI: 3px for plaque and tracker rows, 4px for chat and buttons, 5px for action slots, 6px for tooltips, and 10px only on the top corners of the action bar, which is docked to the screen bottom (no bottom border). Pop-up plates and screenshots keep their native captured frames and are shown at native size. The parchment panel takes its torn, irregular outline from the alpha of `parchment.webp` (stretched to the panel), never from a radius or a mask slice. Focus is a 2px gold outline with 3px offset. No pills, no large rounded cards.

## Components

### Objective Tracker (site navigation)
Fixed top-left, a quest-tracker list on a left-to-right dark wash that fades to transparent. Header "Agents" in gold Marcellus with a collapse/expand toggle (red-and-gold square sprite). Each section is a row: 16px quest-marker sprite, objective-yellow title, tabular-nums time, and `- ` prefixed detail lines in dim paper. Hover and current section (`aria-current`) tint the row `rgba(255,209,0,.09)` and brighten the title to #fff1a6. Some lines swap on a timer (opacity fade) and times tick; both are disabled for still/reduced motion. Collapsed state hides the body; narrow screens start collapsed with a solid dark head/body.

### Zone Plaque
Top-right small bronze-bordered dark plaque with the current zone name in gold Marcellus (.95rem); text crossfades when the zone changes (aria-live polite).

### Zone Text
Centred gold display h1 with a title-size cream subtitle; both fade in with a blur and tracking settle (2.4s, subtitle delayed .45s). The lede fades up at 1.1s delay. A deterministic canvas of ~90 gold light motes drifts upward behind the hero text.

### Tooltips
The universal content container: navy fill, 1px silver edge, 6px corners, 12px 16px padding. Title in white Alegreya 500; body lines at 1rem; flavor lines in gold italic; "Use:" lines in green; warning lines in red. The legend variant is a 22px-icon grid. The item variant (`tooltip--item`) is a big, 680px single-column tooltip with a white Marcellus title, used for the safety statement. Action-bar tooltips are `display:none` until the slot is hovered or focused, anchored above the bar.

### Action Bar and Slots
Bottom-centre dark bronze-framed bar with three 62px slots (54px narrow): a radial brown-black face, 2px near-black border, bronze inner ring, 42px outlined gold SVG icon, white keybind numeral top-right, and a name at the bottom. Hover/focus swaps to a lighter brown face and a #ffe27a ring; active presses down (`translateY(1px) scale(.97)`). Activation runs a conic-gradient cooldown sweep (1.6s linear). The version line sits under the bar, framed in bronze, with the disclaimer.

### Chat Window
Dark 46%-black panel with 4px corners and 1rem text. Lines use semantic chat colours: yellow default (#ffef7a), orange `[ompcraft]` channel, pink whisper, cyan bracketed link. The big variant adds a command box (near-black, 1px #5d4b28 border) with mono text and a red-and-gold copy button (gradient #8d1408 to #4e0802, 1px #e0a83a border, gold label, brightness on hover).

### Quest Log
The parchment panel: textured jpg with edge mask, ink text, Marcellus headings, `Objectives` and `Rewards` lists with game-style `- ` bullets and tabular green `1/1` counts. Max 760px, 46-60px padding, large soft drop shadow.

### Pop-up Plates and Screenshots
Real in-game PNG captures shown at native size (plates) or native ×2 (tracker), with figcaption labelling them demo data. In the hero, a looped script slides plates in with opacity/translate and a masked shine sweep; the stack is static further down.

### Centre-Screen Message
A fixed gold Marcellus line at 9vh (the game's UIErrorsFrame), fades in over .08s and out over .3s (shown 2.6s), used for copy and share confirmations such as "Install command copied. Paste it in a terminal."

## Do's and Don'ts

### Do:
- **Do** put information in recreated game chrome (tracker, tooltip, chat, plaque, parchment) over a zone photograph; let the world be the background.
- **Do** keep gold (#ffd100) for titles and objectives, green for "Use:" lines, red for warnings and the unofficial notice, cyan for clickable chat links.
- **Do** put a black text-shadow outline on every piece of text that sits directly on the world.
- **Do** frame tooltips with 1px silver (#a7a9b0) over navy, and chrome with 1px bronze (#8a6a2c) plus an inner black hairline.
- **Do** keep corners between 3px and 6px (10px only on the docked bar's top corners).
- **Do** ease every transition with `cubic-bezier(.16, 1, .3, 1)`; set zone/reveal motion between .25s and 2.4s, and drop all animation under `prefers-reduced-motion`.
- **Do** keep the unofficial / not-approved line and Blizzard trademark footer visible, and label every demo agent as demo.
- **Do** use a 2px gold focus outline (3px offset) on all interactive elements and keep keyboard parity for the action bar (keys 1, 2, 3).

### Don't:
- **Don't** introduce flat colour sections, tinted bands, or a light theme; the only light surface is the parchment quest log.
- **Don't** use gradients as decoration; they are scrim, frame metal, or sprite fill only.
- **Don't** add generic feature-card grids, thick coloured borders, or rounded cards above 6px; tooltips are the container language.
- **Don't** invent a second display face or set display type in anything but Marcellus; use JetBrains Mono for code only.
- **Don't** reframe real in-game captures in a device or window chrome; their own frames are the frame.
- **Don't** use hard offset shadows or coloured box glows; luminous effects are limited to the pop-up shine sweep and hero motes.
- **Don't** use emoji or icon-font glyphs; icons are the inline gold/red outlined SVG sprites.
