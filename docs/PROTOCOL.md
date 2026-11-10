# ompcraft link protocol, v0 (read-only slice)

Normative for the first slice: **agent status shown in WoW, read-only.** Nothing travels from the game to the PC. The general v1 protocol, with uplink, commands and approvals, is sketched in `PLAN.md` §5 and will extend this document.

The hops:

```
Tern window plugin --(HTTP, loopback)--> companion --(files)--> WoW addon
```

Terms:

| Term | Meaning |
|---|---|
| **G** | The game flavour folder, e.g. `…/World of Warcraft/_forever_`. |
| **A** | `G/Interface/AddOns`. |
| **UI session** | One load of the game UI: a login or a `/reload`. |
| **Launch** | One game process lifetime. |

## 1. Files the companion installs (game closed)

The companion only creates and removes the folders named here. It never touches anything else under `A`.

### 1.1 Main addon `A/Ompcraft/`

Copied from the repository's `addon/Ompcraft/`.

### 1.2 Slots `A/OmpcraftS000` … `A/OmpcraftS{N-1}`

`N` defaults to 1000, which is also the maximum. Each slot holds exactly two files.

`OmpcraftSnnn.toc`. It is identical in every slot except the `Group` line, which names the slot itself:

```
## Interface: 16001
## Title: |cff808080Ompcraft link slot|r
## Notes: Part of Ompcraft. Collapse this section; don't disable these.
## Group: OmpcraftSnnn
## Category: Ompcraft link helpers
## LoadOnDemand: 1
data.lua
```

**Why `Group` names the slot itself.** Blizzard's AddOns list (`Blizzard_AddOnList/AddonList.lua`) only puts an addon under a collapsible `Category` node when the addon is its own group parent (`name == group`). The collapsed state persists per machine (`## SavedVariablesMachine: g_addonCategoriesCollapsed`). So the player collapses "Ompcraft link helpers" once instead of scrolling past 1000 rows.

`data.lua` starts as the single line `OmpcraftInbox = nil`.

### 1.3 Signals `A/OmpcraftSignals/`

`OmpcraftSignals.toc`:

```
## Interface: 16001
## Title: |cff808080Ompcraft link signals|r
## Notes: Part of Ompcraft. Collapse this section; don't disable these.
## Group: OmpcraftSignals
## Category: Ompcraft link helpers
## LoadOnDemand: 1
## X-Ompcraft-Link: 0
## X-Ompcraft-Slots: <N>
## X-Ompcraft-Signals: <M>
```

The folder holds:

- `0000.wav` … `{M-1}.wav`: the doorbells. `M` defaults to 2000. Each is a tiny silent WAV.
- `ctl_present.wav`: the same bytes. It is never deleted.
- `ctl_absent.wav` is **never** created. The addon uses it as the negative control.

### 1.4 Install rules

- Install refuses to run while the game process exists. A process counts as the game when its cmdline contains `_forever_` and `Wow.exe`.
- `--addon-only` copies just `A/Ompcraft` and is allowed while the game runs. A `/reload` picks it up.
- Uninstall removes exactly `A/Ompcraft`, `A/OmpcraftS*` folders whose name is `OmpcraftS` followed by 3 digits, and `A/OmpcraftSignals`.
- The companion stores its chosen `G` in `$XDG_CONFIG_HOME/ompcraft/config.json`. `XDG_CONFIG_HOME` defaults to `~/.config`.

## 2. Client facts this relies on

Re-check these on every client build. Sources are in RESEARCH.md §1.3 and §6.

**Verified live** on 2026-11-09, build 1.60.1, GE-Proton 11-12 via umu, Linux/Hyprland:

- Linux inotify reports `IN_OPEN` when the client (through Wine) reads `Ompcraft/**/*.lua` at UI load and `OmpcraftSnnn/data.lua` at `LoadAddOn`.
- `LoadAddOn` read slot data written after launch.
- The panel header read `live`, which the addon shows only in doorbell mode. So the login self-test passed (`ctl_present` read present, `ctl_absent` absent) and the companion's session was trusted.
- **Doorbells woke the addon.** After a `/reload`, three publishes (gen 7, 8, 9) were each followed by the next slot's `data.lua` open within 67–174 ms, in the companion log. The timer could not do that, since its interval is 5 s or more.
- The game process command line is `C:\Program Files (x86)\World of Warcraft\_forever_\Wow.exe -launcherlogin -uid wowf`.

1. The client sees only addon folders and loose files that existed at launch.
2. `C_AddOns.LoadAddOn(slot)` reads `data.lua` from disk at that moment, and loads each slot once per UI session.
3. `PlaySoundFile(path, "Master")` returns a truthy `willPlay` iff the file is present now. A file found once reads as present until the client restarts.

## 3. Plugin → companion

### 3.1 Discovery

The companion writes `$XDG_CONFIG_HOME/ompcraft/link.json` with mode 0600 at startup:

```json
{ "v": 0, "url": "http://127.0.0.1:47131", "token": "<64 hex chars>", "pid": 12345 }
```

- The port is 47131 if free, otherwise any free port.
- A new random token is generated on every companion start.

### 3.2 Request

```
POST {url}/v1/sync
Authorization: Bearer {token}
Content-Type: application/json
```

```json
{
  "v": 0,
  "window": "<cx:window_key()>",
  "vm": "<random id chosen when the plugin VM loads>",
  "hosts":    [ { "id": 0, "name": "…", "kind": "local", "state": "connected" } ],
  "sessions": [ { "id": 12884901894, "name": "…", "host": 0 } ],
  "agents":   [ { "pane": "7881264988162", "state": "working", "title": "…", "cwd": "…", "model": "…",
                  "host": 0, "session": 12884901894, "last_message": "…", "delivery_error": null } ],
  "events":   [ { "t": "finished", "pane": "…", "result": "done" },
                { "t": "state", "pane": "…", "old": "working", "new": "idle" } ]
}
```

Field rules:

- `agents` is `cx.agents:list()` with `pane` stringified. Pane ids can exceed 2^52.
- `sessions` is `cx.sessions:list()`.
- `hosts` is `cx.hosts:list()` reduced to the fields shown.
- Every request carries the full lists. `events` carries what happened since the previous request.
- Unknown fields are ignored. Optional fields may be absent or JSON `null`.

### 3.3 Response

The response is always immediate. It is never held open. RESEARCH §2.5 explains why.

```json
{ "v": 0, "leader": true, "interval_ms": 500 }
```

- **Leader.** One `(window, vm)` pair is the leader. A pair from a different window receives `"leader": false, "interval_ms": 3000`, and the companion ignores its data. A leader that sends nothing for 5 s loses leadership to the next caller. A sync from the leader's own window with a new `vm` takes over at once, because a reloaded plugin VM replaces the old one.
- **Errors.** A wrong or missing token gets `401`. Malformed JSON gets `400`. On any error, or when the connection fails, the plugin re-reads `link.json` and retries at 3000 ms.

## 4. Companion model

### 4.1 Agent fields

Agents are keyed by `pane`, and the companion keeps `since` per agent. `since` is the unix time of a state change this companion actually observed. It is **0 when the start of the current state was not observed**, for example for an agent first seen already idle. The addon shows no elapsed time when `since` is 0.

Display fields:

- **`name`**: `clean(title)`. If that is empty, `"omp"` or `"zsh"`, use the basename of `cwd`.
- **`clean`**: removes Unicode private-use code points (U+E000–U+F8FF, U+F0000–U+10FFFF), characters outside the BMP, and C0 controls except `\n`, then collapses runs of whitespace and trims.
- **`last`**: `clean(last_message)`. Cut at the first `"\n·\n"`, which is omp's turn-footer separator. Join lines with `" "`, then truncate to 280 bytes on a UTF-8 boundary, appending `…` if cut.
- **`host`**: `""` for the local host (`kind = "local"`). Otherwise the host name (`hosts[id].name`). **`session`**: the session name. **`model`**: `clean(model)`. **`cwd`**: the basename of `cwd`.

### 4.2 Toasts

Toasts come from state transitions observed between consecutive syncs:

| Transition | Kind | Text |
|---|---|---|
| any → `waiting_input` | `input` | `<name> needs you` |
| `working` → `idle` | `done` | `<name> finished` |
| any → `exited` | `exited` | `<name> exited` |
| the companion replaced the installed addon while the game runs (§8.7) | `update` | `ompcraft updated to <version>. Type /reload to load it.` |

A `finished` event with `result = "failed"` within 5 s of the transition turns `done` into `failed` (`<name> failed`).

Toast `id` is the companion's unix time in ms, strictly increasing. Keep the last 20 toasts that are younger than 10 min.

### 4.3 Tern status

`tern = "ok"` when the last leader sync was under 3 s ago. Otherwise `"down"`.

## 5. Snapshot (`data.lua`)

`data.lua` is one assignment made of literals only: tables, strings, numbers and booleans. There are no function calls, no concatenation and no identifiers other than `OmpcraftInbox`.

Strings are emitted with only these characters escaped:

- printable ASCII except `\` and `"`;
- `\\`, `\"` and `\n`;
- `|` is doubled to `||`, so WoW markup can't be injected;
- every other byte as `\ddd`.

```lua
OmpcraftInbox = {
  v = 0,
  gen = 17,               -- increments on every publish within one companion run
  run = 1762727000123,    -- companion start time in ms; a new value means the companion restarted
  at = 1762728000,        -- unix seconds this snapshot was written
  session = 1762727900,   -- unix seconds the companion saw this UI session start; 0 = not seen
  nextSignal = 5,         -- doorbell to watch for the next snapshot
  tern = "ok",
  companion = "0.1.0",
  agents = {
    { id = "7881264988162", name = "…", host = "…", session = "…", state = "working",
      since = 1762727000, model = "…", cwd = "…", last = "…" },
  },
  toasts = {
    { id = 1762727999123, kind = "done", agent = "7881264988162", text = "…" },
  },
}
```

Agent order:

1. Group by state: `waiting_input`, then `working`, then `idle`, then `exited`.
2. Within a group, newest `since` first.

## 6. Publishing (companion)

### 6.1 When to publish

Each publish costs the addon a slot load, and slots are limited per UI session. So changes come in two classes:

- **Important changes publish at once,** coalesced to at most one publish per 1000 ms:
  - an agent added or removed;
  - any agent state change;
  - a new toast, or a toast kind rewritten;
  - `tern` flipping between ok and down;
  - a UI session change;
  - the first publish of a run.
- **Cosmetic changes** are everything else: `name`, `model`, `host`, `session` name, `cwd` and `last`. When only these have changed, publish once 60 s have passed since the previous publish of any kind. An important publish carries any pending cosmetic changes with it.
- **Heartbeat.** Publish every 120 s even with no change.

**While the game is closed,** publishing writes no slots and creates no doorbells. "Closed" means no game process is detected and no UI session has started since the game last exited. The snapshot state keeps updating, and the next UI-session start writes every slot.

### 6.2 Publish procedure

1. Set `snap.nextSignal = signalNext + 1`.
2. Write `data.lua` atomically (write `data.lua.tmp`, then rename) into the slots the addon may load next:
   - Once at least one slot open has been observed in the current UI session, write the **8 lowest-index slots not marked loaded**.
   - Until then, or when no UI session has been seen, write **every slot not marked loaded**.
3. Create doorbell `signalNext`.
4. Increment `signalNext`.

If `signalNext` reaches `M`, stop ringing and log it. The addon then falls back to its timer.

**Why this order works.** The doorbell rung is always the one the previous snapshot told the addon to watch. An addon that is behind sees an older doorbell already present and loads the newest snapshot anyway.

### 6.3 Detection with Linux inotify

Watch `IN_OPEN` on:

- **Files ending `.lua` anywhere under `A/Ompcraft/`.** Watch recursively, because the addon's files live in subfolders and inotify does not report events from subfolders. Re-add watches when a subfolder is created or the addon is reinstalled. The first open after 2 s of quiet marks a **UI session start** at time T. On it:
  - set `session = T` and clear the loaded-slot set;
  - if the game PID differs from the PID at the previous UI session start (a new launch), delete every doorbell file and set `signalNext = 0`;
  - publish immediately.
- **`A/OmpcraftSnnn/data.lua`.** Each open marks slot `nnn` as loaded. The companion's own temp-file writes use a different name, and rename does not raise `IN_OPEN`.

### 6.4 Game process

Poll `/proc` every 2 s for the game PID. When the game exits, recreate all doorbell files, so they exist at the next launch.

### 6.5 Persisted state

`$XDG_STATE_HOME/ompcraft/state.json` (default `~/.local/state/ompcraft/`) holds `{gamePid, signalNext}`. A restarted companion resumes them when the same PID is still running. Otherwise it starts with `signalNext = 0`.

## 7. Addon behaviour

### 7.1 Startup

- **Load.** Record `uiStart = time()` at `ADDON_LOADED`.
- **Login self-test.**
  - Read `X-Ompcraft-Slots` and `X-Ompcraft-Signals` with `C_AddOns.GetAddOnMetadata("OmpcraftSignals", …)`. If `C_AddOns.DoesAddOnExist("OmpcraftSignals")` is false, show "companion not installed" and stop.
  - Probe `ctl_present.wav` (expect present) and `ctl_absent.wav` (expect absent).
  - Doorbell mode is allowed only if both behave as expected. Otherwise the addon uses **timer mode**.
- **Bootstrap.** Two seconds after login, load the next slot.

### 7.2 Loading a slot

- The addon loads the lowest-index slot for which `C_AddOns.IsAddOnLoaded(name)` is false.
- Set `OmpcraftInbox = nil`, call `C_AddOns.LoadAddOn(name)`, read the global, then set it to `nil` again.
- If all slots are used, show "out of link slots — /reload to refresh" and stop loading.

### 7.3 Applying a snapshot

- Accept it only if `v == 0`, and either `run` differs from the last applied `run` or `gen` is higher than the last applied `gen`.
- **Trust check.** Doorbells are trusted only if `abs(session - uiStart) <= 15`. Otherwise the companion did not see this UI session (it started later), so stay in timer mode until a snapshot passes the check.

### 7.4 Doorbell mode

- Every 0.25 s, probe doorbell `nextSignal`, as long as the minimum gap since the last load has passed: 1 s when the panel is shown, 3 s when hidden.
- Present means load the next slot.
- **False rings.** Two consecutive loads that bring no newer snapshot switch the addon to timer mode for the rest of the UI session.

### 7.5 Timer mode

Load the next slot every 5 s when the panel is shown, every 30 s when hidden.

**Back-off.** After a timer load that brings no newer snapshot, double the interval, up to 60 s shown and 120 s hidden. Reset to the base interval after a load that brings a newer snapshot, and when the panel opens.

### 7.6 Toasts

- Show toasts with `id > OmpcraftDB.lastToast`, then raise `lastToast`.
- Skip toasts with `id / 1000 < uiStart - 30` silently. This prevents a replay storm after login or `/reload`.
- Hold toasts while `InCombatLockdown()` is true, and flush them on `PLAYER_REGEN_ENABLED`.

### 7.7 Freshness

The status shows **stale** if `time() - at > 300`.

## 8. Rich status (v0.1 additions)

These fields drive the native tracker UI in `docs/UI-BRIEF.md`. All of them are optional. An older peer ignores them, so `v` stays 0.

### 8.1 Plugin → companion: `agents[].detail`

The plugin may attach `detail` to an agent. It derives it from `cx.agents:transcript(pane, { last = 8 })`:

```json
"detail": {
  "activity": "Running tests",
  "todo": "3/7",
  "steps": [ { "name": "bash", "text": "Running tests", "status": "running" } ],
  "reply": "markdown of the last assistant message with non-empty text"
}
```

- **`activity`.** The last tool record with `status == "running"` in the newest assistant message. Its value is that record's `intent`, else its `title`. It is `null` when nothing is running.
- **`todo`.** The `target` of the newest tool record whose `name` is `"todo"`, for example `"3/7"`.
- **`steps`.** The last ≤10 tool records across the fetched messages, oldest first. `text` is the record's `intent`, else `title`, else `name`. `status` is the record's status as given (`done`, `running`, `error`, …).
- **`reply`.** The text of the newest assistant message whose text is non-empty, truncated by the plugin to 16 KB.
- **When the plugin fetches.** A transcript fetch happens when the agent's `state` or `last_message` changed since the last fetch, and at most every 2 s while the agent is `working` or `waiting_input`. The plugin caches the result per pane. It sends the cached `detail` on every sync, and drops the cache when the pane disappears.

### 8.2 Snapshot: new per-agent fields

```lua
{ id = "…", …existing fields…,
  section = "infra",              -- session name; remote agents get " · <host>" appended
  activity = "Running tests",     -- only while working; from detail.activity
  todo = "3/7",
  stats = "1m 12s · 22K tok · $0.42",  -- from the omp turn footer in last_message; "" if none
  doneAt = 1762728000,            -- unix time of the last observed working → idle/waiting_input/exited change; 0 = not observed
  preview = "…",                  -- one line, ≤140 bytes, from detail.reply (else last), markdown stripped
  detail = { lines = { … }, steps = { … } },  -- only for some agents, see 8.4
}
```

**`stats`.** omp's footer looks like `43.2s · 22K tok · $1.09 this turn`. The companion reformats durations as `1m 12s`, `43s` or `1h 4m`, drops "this turn", and joins the parts with ` · `.

### 8.3 Markdown lines

`detail.lines` is a list of `{ k = kind, t = text, d = depth }`. `d` is set only for list items, with depth starting at 1.

| kind | from |
|---|---|
| `h` | ATX heading (`#`…`######`) or a `**Bold line**` that stands alone |
| `p` | paragraph line (soft-wrapped lines are joined) |
| `li` | `-`, `*`, `+` or `1.` list item, nested by indent |
| `code` | one line inside a fenced code block (fence lines dropped); `t` is raw |
| `quote` | `>` line |
| `hr` | `---` |
| `tr` | markdown table row: outer pipes stripped, cells trimmed and joined by `  ·  `. Separator rows (`|---|`) are dropped. The header row has its cells wrapped in bold sentinels. |
| `gap` | blank line between blocks (never two in a row) |
| `more` | the final line when truncated: "… open in Tern for the full reply" |

The addon renders an unknown kind as `p`, so new kinds degrade safely.

**Data at rest.** Snapshots carry reply text into files under `Interface/AddOns`. So:

- The companion rewrites every slot's `data.lua` to `OmpcraftInbox = nil` when the game exits and when `ompcraft run` shuts down.
- `ompcraft config detail off` makes snapshots omit `detail`, `preview` and `last`.

Inline markup inside `t`, except in `code` lines, is converted to sentinel bytes. The addon swaps them for colour codes it owns:

| Sentinel | Meaning |
|---|---|
| `\1` … `\2` | inline code |
| `\3` … `\4` | bold |

Further rules:

- Links `[text](url)` become `text`. Bare URLs stay as text.
- Italics markers are removed.
- All other text goes through `clean()`, except that newlines split lines.
- The total of `t` across all lines is capped at 6000 bytes.

### 8.4 Which agents carry `detail`

At most 8 agents carry `detail`, chosen in this order:

1. Every `waiting_input` agent.
2. Every `working` agent.
3. The most recent agents by `doneAt > 0`, up to 4.

`detail.steps` holds the last ≤10 steps as `{ n = name, t = text, s = status }`.

### 8.5 Publish classes, revised

- **Important** (1 s coalesce): unchanged from §6.1.
- **Activity** (`activity`, `todo`): at most one publish per 20 s from this class alone. An important publish carries it along.
- **Cosmetic** (everything else, including `stats`, `preview` and `detail`): 60 s, as before.

**Slot count.** `install --slots` defaults to 1000.

### 8.6 Addon: seen tracking

An agent is **finished, unseen** when its `state` is `idle`, `doneAt > 0`, and `doneAt > OmpcraftDB.seen[id]`. Opening its detail page sets `OmpcraftDB.seen[id] = doneAt`. Hovering its row for 1 s does the same.

### 8.7 Toast kind `update`

When the companion starts and finds that the installed `A/Ompcraft` differs from the addon embedded in its own binary, it replaces the folder atomically (as `install --addon-only` does). The comparison is a content hash of every file, not the TOC version.

If the game was running at that moment, the snapshot gets one more toast:

| Field | Value |
|---|---|
| `kind` | `update` |
| `agent` | `""` (the toast belongs to no agent) |
| `text` | `ompcraft updated to <version>. Type /reload to load it.` |
| `id` | the usual strictly increasing ms timestamp |

If the game was not running there is no toast: the next launch loads the new addon.

The companion also rewrites outdated helper TOC files (`OmpcraftSnnn`, `OmpcraftSignals`) in place. The client reads TOC metadata at launch, so the change shows after the next game start.

### 8.8 `/v1/sync` response field `notice`

After such an update the companion puts a one-shot `notice` into the next `/v1/sync` response **to the leader**:

```json
{ "v": 0, "leader": true, "interval_ms": 500,
  "notice": { "id": "update-0.2.0", "level": "info", "text": "ompcraft updated to 0.2.0",
              "sub": "Type /reload in WoW to load the new addon." } }
```

- `level` is `info`. `id` is stable per update, so the plugin can de-duplicate.
- The companion sends each notice once, then forgets it. Receiving it is the acknowledgement. Followers never receive or consume it.
- When the game was not running, `sub` is `The new addon loads the next time you start WoW.`
- The plugin shows it with `cx:toast`.

### 8.9 Updates

Release builds (`ompcraft version` prints `(release)`) update themselves. Source builds never do.

**Signed releases.** Every release carries `ompcraft_<goos>_<goarch>.tar.gz`, `checksums.txt` and `checksums.txt.sig`, the base64 (std) of `ed25519.Sign(priv, checksums.txt)`. The matching 32-byte public key is compiled into the binary (`ReleasePublicKey`). A build with no key refuses every update: "this build has no release key; updates are disabled".

**Verification order.** The updater downloads `checksums.txt` and its signature and verifies the signature with the embedded key **before it trusts any checksum**. Only then does it download the archive (≤ 50 MB), check its sha256 against `checksums.txt` and extract the `ompcraft` member. Any failure refuses the update and is logged.

**Schedule.** 2 min after start, then every 6 h, each with ±10 % jitter, unless `config.json` has `"autoupdate": false`.

**Never mid-session.** The daemon never replaces its binary while the game runs:

- If the game runs, a check that finds a newer release only downloads and verifies it, and stages the verified binary as `<exe dir>/.ompcraft.staged`. `state.json` records `stagedVersion`. The log says `update: <v> is ready; it installs after you quit WoW`, and the next `/v1/sync` response to the leader carries the one-shot notice `{"id": "update-ready-<v>", "level": "info", "text": "ompcraft <v> is ready", "sub": "It installs after you quit WoW."}`.
- The staged binary is installed when the game exits (the §6.4 exit handler), or at the next check that finds no game running.
- `ompcraft update` is the user's explicit choice and applies at once. It prints "type /reload in game" if the game runs.

**Safe swap.** Before the swap the staged binary runs `ompcraft version` (5 s timeout) and its output must name the expected version, otherwise the update is refused and the staged file removed. The previous binary is kept as `<exe>.old`, overwriting an older one. Then the staged binary is renamed over the executable (atomic, allowed while the old binary runs).

**Restart.** The daemon runs its normal shutdown (slot wipe, `link.json` removal), then re-executes the new binary with `exec(2)`. The PID is unchanged, so systemd and launchd notice nothing.

**Rollback.** Before the re-exec, `state.json` records `pendingVersion`, `startAttempts: 0` and `previousVersion`. Every start of that version increments `startAttempts`. After the new process has served HTTP for 30 s it clears the pending record. A start that finds `startAttempts >= 3` restores `<exe>.old` over the executable, logs `update: <v> failed to start 3 times; rolled back to <old>`, records the version as `badVersion` (never applied again) and re-executes the old binary.

**State.** `GET /v1/state` carries `update: { channel, autoupdate, lastCheck, latest, lastError, staged }`. `lastCheck` is unix ms, 0 before the first check. `staged` is present while a verified update waits for the game to quit.

**Manual.** `ompcraft update [--check] [--force]`.
