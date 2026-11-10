# ompcraft UI brief (v1)

Status: proposed and awaiting confirmation. This brief was shaped with the owner on 2026-11-09.

## Job and audience

- **Who:** one player who runs several omp agents in Tern while playing WoW: Forever on the same PC. Their attention belongs to the game. They glance at agents between pulls, on flight paths, and in town.
- **Mode:** Operate. Success means knowing which agent needs them at a glance, reading what it said without alt-tabbing, and answering it from the game.
- **What only ompcraft can say:** the agents live in the player's game world. The UI should feel like part of WoW, not like an app overlay.

## Direction: native WoW

The visual language comes from the quest log and the objective tracker.

- **No box.** Agents are listed in the objective-tracker style, anchored under Blizzard's objective tracker when it is shown, otherwise top-right.
  - Gold section headers, one per Tern session. The host name is added only for remote hosts, e.g. "infra", "docs-site · host-a".
  - Agent titles look like quest titles. Detail lines underneath look like quest objectives, with the standard tracker fonts, colours and text shadow.
  - Rows highlight on hover the way quests do. Headers collapse the way tracker modules do.
- **State markers use quest iconography:**

  | Marker | Meaning |
  |---|---|
  | yellow `!` | needs you |
  | yellow `?` | finished, not yet seen |
  | grey `?` | working |
  | none | idle, seen |
  | red `!` | failed or exited |

  The atlas or texture names must be verified in the `forever` UI source before use.
- **"Seen" is local.** Opening an agent's detail page, or hovering it for a second, marks its current turn as seen. This is stored in SavedVariables. It needs no game → PC channel.
- **Toasts reuse Blizzard's alert-frame look:** achievement or "quest complete" style for finished, a ready-check style for needs-you, a muted error style for failed. A finished toast carries the turn stats ("1m 12s · 22K tok · $0.42"). Toasts are held during combat and restricted states, as today.
- **Detail page:** a quest-log-style page that opens when an agent is clicked.
  - The page has these parts:
    - A title and a state line.
    - The agent's last reply. Markdown is rendered to WoW markup: headers in gold, lists indented, inline code and code blocks in a distinct colour, links shown as text.
    - The recent tool steps. Each shows its name, its intent, and a done, failed or running mark.
    - A footer with model, host, session, cwd and the turn stats.
  - It scrolls. It closes with Esc, by joining `UISpecialFrames`.
  - Later it gains a reply box (see Interaction).

## What each row shows

Restraint keeps the tracker readable with 20 agents:

| Agent state | Title line | Objective lines |
|---|---|---|
| needs you | marker, title, elapsed waiting | one line with the question or last-reply preview |
| working | marker, title, turn elapsed | current activity (the tool intent, e.g. "Running tests…", with a soft pulse); todo progress `3/7` when the agent has a todo list |
| finished, unseen | marker, title | turn stats line; one-line preview |
| idle, seen | title only, dim | none. These are hidden behind a per-section "N idle" line, collapsed by default. |
| failed / exited | marker, title | reason line, if any |

Cost and tokens come from omp's turn footer and appear only after a turn ends. During a turn, only elapsed time is shown.

## Interaction (next milestone: reply and interrupt)

- **The detail page gains a reply box** and buttons for Send, Interrupt and Quick replies ("continue", "yes", "stop").
  - Quick replies are text the player chooses to send. They are never automatic.
  - Every send is a player click or keypress (rule R1).
- **Transport.** Input travels over the game → PC channel of PLAN §5.1, the `AddOns.txt` bits.
  - It carries only text the player typed or picked, never game state (RESEARCH §5.5).
  - It needs an M0 measurement first: `SaveAddOns` cost and rate, and whether `AddOns.txt` is written atomically.
- **Feedback.** Sent text shows at once as "sending…". It turns into the real transcript entry when the companion confirms. Failures show inline, in red.
- **Off by default.** The repo is public, so the reply channel ships disabled.
  - A player turns it on with `/oc reply on`. The command explains in-game what it does: it writes their typed text into the addon list file, which an unofficial channel reads, and Blizzard has not ruled on it.
  - The companion ignores the channel unless the player has opted in.
  - The owner has opted in to building it locally. Turning it on by default, or promoting it, waits for the WoWUI@blizzard.com email step (PLAN risk 2a).

## Data the companion must add

All of these come from the Tern plugin API that already exists. The plugin reads transcripts only for agents that are working, waiting, or finished-unseen.

- **Current activity:** the latest tool in `cx.agents:transcript(pane, {last = 2})` with status `running`. Use its `intent`, else its `title`.
- **Todo progress:** the latest `todo` tool record's `target`, e.g. `3/7`.
- **Turn stats:** parsed from the `last_message` footer (`43.2s · 22K tok · $1.09 this turn`).
- **Detail page content** for one selected agent: the last reply text (up to about 8 KB) and the last 10 tool steps.
  - Read-only for now, so the companion cannot know which agent the player opened.
  - Until the uplink exists, it includes details for agents that need attention and for the 3 most recently finished agents.

## States and ranges

- Agents: 0 to 30, across 1 to 3 hosts and 1 to 12 sessions.
- Reply length: 0 to 20 KB. It is truncated with "open in Tern for the full reply".
- **Empty:** "No agents. Start one in Tern."
- **Companion offline:** a dim line, "ompcraft companion not running".
- **Out of slots:** an inline prompt to `/reload`.

## Constraints

- Mainline-family Forever API only, verified against the `forever` UI source.
- Addon-owned insecure frames only. Never parent to or modify Blizzard's tracker modules, because that is taint-prone. Anchor next to them.
- Must not cost frame rate. Elapsed and pulse updates run at most once per second, and only while visible.
- Gamepad navigation is a later milestone, but the layout must leave a clear focus order: sections, then rows, then detail.
