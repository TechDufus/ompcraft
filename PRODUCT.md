# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Static HTML/CSS/JS in `site/`, served by GitHub Pages; no build step (user choice, 2026-11-09). The product itself is a Go companion, a WoW Lua addon and a Tern Luau plugin; only the marketing page is web.

## Users

- omp users who also play World of Warcraft: Forever and want to keep playing while their coding agents work.
- AI-curious developers and gamers who discover it through shared posts (Reddit, X, Discord) and pass it on.
- WoW addon players and authors interested in what an addon can do.

## Product Purpose

ompcraft shows omp coding-agent sessions inside WoW: Forever as a native quest-tracker list, achievement-style pop-ups when an agent finishes, fails or needs you, and a parchment page with the agent's last reply and recent tool steps. Success for the marketing page: a visitor is wowed within one screen and shares it; secondary actions are the one-line install and the GitHub repo.

## Positioning

Your AI agents rendered as WoW's own quest UI, in the real game client, fully local: a Tern plugin reads agent state, a Go companion writes data-only Lua into pre-installed slot addons, and the addon loads them live without `/reload`. No screen capture, no input into the game, no cloud.

## Operating Context

Players run WoW: Forever (Linux under Proton/umu tested; macOS planned) with Stencil Tern and omp agents on the same machine or attached remote hosts. Install is `curl … | sh`, then `ompcraft setup` does the rest; `ompcraft doctor` diagnoses.

## Capabilities and Constraints

- Read-only today: watch, read, alerts. Replying, interrupting and approvals from the game are planned, not shipped.
- Unofficial and not approved by Blizzard; relies on undocumented client behaviour. The disclosure must stay visible on every public surface.
- Hard rules: no generated input into the game, no automated gameplay, combat lockdown respected, free and visible source.
- MIT license. Repo `TechDufus/ompcraft` (not yet public).

## Brand Commitments

- Name: ompcraft (lowercase). Logo: `docs/media/logo.svg` (gold quest "!" beside a terminal prompt).
- The user asked for a "sexy", bleeding-edge feel with dressed-up screenshots and animation (2026-11-09).
- WoW and Blizzard trademarks belong to Blizzard; non-affiliation footer required.

## Evidence on Hand

- Real in-game captures with invented demo data: `docs/media/hero.png`, `detail.png`, `toast.png`, `settings.png`.
- `ompcraft demo` produces the demo data; every agent, path, host and model in it is invented and must be labeled demo where it could be mistaken for real use.
- No users, testimonials, download counts, benchmarks or press exist. Never fabricate them.

## Product Principles

1. Native over novel: it should look like it shipped with the game.
2. Local and honest: nothing leaves the PC, and the risks are stated plainly.
3. Glanceable: the right agent's state in one look, without leaving the game.
4. Never play the game for you.
