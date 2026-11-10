# Third-party notices

ompcraft is MIT-licensed (see `LICENSE`). It adapts small pieces of the MIT-licensed projects below. Each adapted site carries an `Adapted from …` comment.

## t3code-wow

- Source: https://github.com/ryanrhughes/t3code-wow
- License: MIT, Copyright (c) 2026 Ryan Hughes

Adapted:

- `companion/internal/luaser/luaser.go`: the identifier pattern and the Lua reserved-word list used to decide bare versus bracketed table keys. Adapted from `bridge/src/luaser.js` (`IDENT`, `RESERVED`, `key()`).

## ForeverPlusPlus

- Source: https://github.com/xIGBClutchIx/ForeverPlusPlus
- License: MIT, Copyright (c) 2026 Cameron Weaver

Adapted:

- `addon/Ompcraft/Core/Link.lua`, `probe()`: the `PlaySoundFile` presence probe that stops the sound at once. Adapted from `ForeverPlusPlus_AgentChat/AgentChat.lua` (`signalThere`).

## Ideas only, no code

These projects informed the design. No code was copied from them.

- wow-forever-codex (https://github.com/0xinuarashi/wow-forever-codex). No license is published.
- wow-ai (https://github.com/chelinho139/wow-ai), MIT.

See `docs/RESEARCH.md` §1.

## Website and README images (not covered by the MIT license)

- **Zone screenshots** in `site/img/world/` are official World of Warcraft: Forever screenshots © Blizzard Entertainment, from https://worldofwarcraft.blizzard.com/en-us/forever. They are used unaltered on a free, non-commercial fan page under Blizzard's Legal FAQ for fan sites (https://www.blizzard.com/en-us/legal/c1ae32ac-7ff9-4ac3-a03b-fc04b8697010/blizzard-legal-faq). Blizzard can revoke that permission at any time.
- **Images in `docs/media/` and `site/img/`** that show the website (`hero.webp`, `hero-still.webp`, `watch.webp`, `read.webp`, `popups.webp`, `settings.webp`, `og.jpg`) contain those screenshots too, under the same terms. The in-game captures in them and in `site/img/` (`detail.png`, `settings.png`, `tracker.png`, `popups/*.png`) were taken by the author with invented demo agents.
- **Parchment textures** `site/img/parchment.webp` and `parchment-tall.webp` are derived from ambientCG "Paper003" (CC0, https://ambientcg.com/view?id=Paper003).
- **Fonts** in `site/fonts/`: Marcellus, Alegreya Sans and JetBrains Mono, under the SIL Open Font License 1.1 (license texts beside them). They are subset to Latin.
