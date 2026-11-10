-- Plain-Lua test runner for the Ompcraft addon.
-- Run from the repository root:  lua5.1 addon/tests/run.lua

local here = (arg and arg[0] or "addon/tests/run.lua"):match("^(.*)/[^/]*$") or "."
package.path = here .. "/?.lua;" .. package.path

local Stub = require("wowstub")
local Fx = require("fixtures")

local START = 1762728000 -- unix time at login in every test
local ADDON_DIR = here .. "/../Ompcraft"

------------------------------------------------------------------------
-- tiny framework
------------------------------------------------------------------------

local tests = {}
local function test(name, fn)
	tests[#tests + 1] = { name = name, fn = fn }
end

local function eq(actual, expected, msg)
	if actual ~= expected then
		error(string.format("%s: expected %s, got %s", msg or "values differ", tostring(expected), tostring(actual)), 2)
	end
end

local function truthy(v, msg)
	if not v then
		error(msg or "expected a truthy value", 2)
	end
end

local function contains(haystack, needle, msg)
	if not tostring(haystack):find(needle, 1, true) then
		error(string.format("%s: %q not found in %q", msg or "contains", needle, tostring(haystack)), 2)
	end
end

local function lacks(haystack, needle, msg)
	if tostring(haystack):find(needle, 1, true) then
		error(string.format("%s: %q unexpectedly found in %q", msg or "lacks", needle, tostring(haystack)), 2)
	end
end

local function near(actual, expected, tolerance, msg)
	if math.abs(actual - expected) > tolerance then
		error(string.format("%s: expected %s +/- %s, got %s", msg or "near", expected, tolerance, actual), 2)
	end
end

local cleanups = {}

-- Fresh fixtures + client, logged in. The companion has seen the UI session
-- unless opts.session == false. opts.sessionOffset skews its idea of when.
local function setup(opts)
	opts = opts or {}
	local fx = Fx.new(opts)
	cleanups[#cleanups + 1] = fx
	local c = Stub.new({
		addonDir = ADDON_DIR, fixtureDir = fx.dir, unix = START, noTrackerFonts = opts.noTrackerFonts,
		noSettings = opts.noSettings, noMenu = opts.noMenu,
	})
	c.objectiveTracker = opts.tracker
	c.saved = opts.saved -- SavedVariables as they were on disk
	c:login()
	if opts.session ~= false and opts.install ~= false then
		fx:sessionStart(c, opts.sessionOffset)
	end
	return fx, c, c:ns()
end

local function agent(state, since, name, extra)
	local a = {
		id = tostring(7881264988000 + (since or 0)),
		name = name,
		host = "host-a",
		session = "demo-session",
		state = state,
		since = since,
		model = "model-x",
		cwd = "project",
		last = "last words",
	}
	for k, v in pairs(extra or {}) do
		a[k] = v
	end
	return a
end

-- The link status word in the tracker header; nothing is drawn while "live".
local function panelStatus(c)
	local h = c:tracker()[1]
	return h.status == "" and "live" or h.status
end

local function trackerKinds(c, kind)
	local out = {}
	for _, e in ipairs(c:tracker()) do
		if e.kind == kind then
			out[#out + 1] = e
		end
	end
	return out
end

local function agentRows(c)
	return trackerKinds(c, "agent")
end

-- true if every run of "|" in s has even length (so none can start markup)
local function pipesBalanced(s)
	for run in s:gmatch("|+") do
		if #run % 2 ~= 0 then
			return false
		end
	end
	return true
end

------------------------------------------------------------------------
-- util
------------------------------------------------------------------------

test("FormatElapsed picks s/m/h/d", function()
	local _, c, ns = setup({ install = false })
	local f = ns.FormatElapsed
	eq(f(0), "0s")
	eq(f(45), "45s")
	eq(f(59), "59s")
	eq(f(60), "1m")
	eq(f(12 * 60 + 5), "12m")
	eq(f(3599), "59m")
	eq(f(3600), "1h")
	eq(f(3 * 3600 + 59), "3h")
	eq(f(86399), "23h")
	eq(f(2 * 86400), "2d")
	eq(f(-5), "0s", "negative clamps to zero")
	eq(f(nil), "0s")
end)

test("Truncate never splits a || pair or a UTF-8 sequence", function()
	local _, c, ns = setup({ install = false })
	local samples = {
		"plain-name-that-is-long-enough",
		"a||b||c||d||e||f||g||h||i||j||k",
		"||||||||||||||||||||",
		"caf\195\169 \195\169\195\169\195\169\195\169\195\169\195\169 x",
		"\228\184\173\230\150\135\228\184\173\230\150\135\228\184\173\230\150\135",
	}
	for _, s in ipairs(samples) do
		for max = 1, 20 do
			local out = ns.Truncate(s, max)
			truthy(pipesBalanced(out), "unbalanced pipes for max " .. max .. ": " .. out)
			-- a valid UTF-8 string never ends on a lone lead byte
			local lastByte = out:byte(#out)
			truthy(lastByte < 0xC0, "cut inside a UTF-8 sequence: " .. out)
		end
	end
	eq(ns.Truncate("short", 30), "short")
	eq(ns.Truncate("abcdefghij", 10), "abcdefghij")
	eq(ns.Truncate("abcdefghijk", 10), "abcdefg...")
end)

test("EscapePipes doubles stray pipes and keeps doubled ones", function()
	local _, c, ns = setup({ install = false })
	eq(ns.EscapePipes("a||b"), "a||b")
	eq(ns.EscapePipes("a|b"), "a||b")
	eq(ns.EscapePipes("|cffff0000x|r"), "||cffff0000x||r")
	eq(ns.EscapePipes("ctl\1\2x"), "ctlx")
	eq(ns.EscapePipes(nil), "")
	truthy(pipesBalanced(ns.EscapePipes("|||")))
end)

------------------------------------------------------------------------
-- startup, slots, doorbells
------------------------------------------------------------------------

test("bootstrap loads S000 two seconds after login", function()
	local fx, c, ns = setup({ agents = { agent("working", START, "demo-agent") } })
	c:advance(1.9)
	eq(#c.loadLog, 0, "nothing loads before the 2 s mark")
	eq(c.env.C_AddOns.IsAddOnLoaded("OmpcraftS000"), false)
	c:advance(0.2)
	eq(#c.loadLog, 1)
	eq(c.loadLog[1].name, "OmpcraftS000")
	near(c.loadLog[1].at - c.loginAt, 2.0, 0.06, "load time after login")
	eq(c.env.OmpcraftInbox, nil, "OmpcraftInbox is cleared after the handoff")
	local rows = agentRows(c)
	eq(#rows, 1)
	eq(rows[1].name, "demo-agent")
	eq(panelStatus(c), "live")
	eq(ns.Link:Mode(), "doorbell")
	eq(ns.Link.nextSignal, 1)
end)

test("self-test probes ctl_present and ctl_absent at login", function()
	local fx, c, ns = setup({ install = false })
	eq(#c.soundFileCalls, 0, "no probes when the signals addon is missing")
	local fx2, c2, ns2 = setup()
	local calls = {}
	for _, call in ipairs(c2.soundFileCalls) do
		calls[call.file] = call.present
	end
	eq(calls["OmpcraftSignals\\ctl_present.wav"], true)
	eq(calls["OmpcraftSignals\\ctl_absent.wav"], false)
	truthy(ns2.Link.selfTest.ok)
	-- found files are stopped at once, so nothing plays
	eq(#c2.stopped, 1)
end)

test("a rung doorbell loads the next slot, and load gaps are respected", function()
	local fx, c, ns = setup({ agents = { agent("working", START, "demo-agent") } })
	c:advance(2.1) -- bootstrap loaded S000 at ~2.0 s
	eq(#c.loadLog, 1)
	fx:publish(c) -- gen 2, rings doorbell 1, the one snapshot 1 asked for
	c:advance(0.5)
	eq(#c.loadLog, 1, "panel shown: min gap 1 s has not passed")
	c:advance(0.8)
	eq(#c.loadLog, 2)
	eq(c.loadLog[2].name, "OmpcraftS001")
	eq(ns.Store.gen, 2)
	eq(ns.Link.nextSignal, 2)

	-- hidden panel: the gap is 3 s
	c:slash("/oc hide")
	fx:publish(c) -- gen 3, rings doorbell 2
	c:advance(2.5)
	eq(#c.loadLog, 2, "panel hidden: min gap 3 s has not passed")
	c:advance(1.0)
	eq(#c.loadLog, 3)
	eq(c.loadLog[3].name, "OmpcraftS002")
	eq(ns.Store.gen, 3)
	eq(ns.errorCount, 0)
end)

test("no doorbell, no load", function()
	local fx, c, ns = setup()
	c:advance(2.1)
	c:advance(20)
	eq(#c.loadLog, 1, "only the bootstrap slot loaded")
end)

test("two doorbells without news switch to timer mode (false-ring guard)", function()
	local fx, c, ns = setup({ agents = { agent("working", START, "demo-agent") } })
	c:advance(2.1)
	eq(#c.loadLog, 1)
	fx:ring(1) -- a ring with no new snapshot behind it
	c:advance(1.5)
	eq(#c.loadLog, 2, "first ring loads a slot")
	eq(ns.Link:Mode(), "doorbell", "one false ring is tolerated")
	c:advance(1.5)
	eq(#c.loadLog, 3, "the doorbell still reads present, so a second false ring")
	local mode, why = ns.Link:Mode()
	eq(mode, "timer")
	eq(why, "false-ring")
	contains(c:chatText(), "timer mode")
	eq(panelStatus(c), "timer")
	c:clearChat()
	c:slash("/oc status")
	contains(c:chatText(), "doorbells: 2, timer loads: 0, false rings: 2")

	local before = #c.loadLog
	c:advance(3.5)
	eq(#c.loadLog, before, "timer mode waits 5 s while the panel is shown")
	c:advance(2.0)
	eq(#c.loadLog, before + 1)
	-- and a real snapshot does not undo the switch for this UI session
	fx:publish(c)
	c:advance(10.5) -- the empty timer load above doubled the interval to 10 s
	eq(ns.Store.gen, 2)
	eq(select(2, ns.Link:Mode()), "false-ring")
end)

test("timer mode loads every 30 s while the panel is hidden", function()
	local fx, c, ns = setup({ ctlPresent = false })
	eq(select(2, ns.Link:Mode()), "selftest")
	c:slash("/oc hide")
	c:advance(2.1)
	eq(#c.loadLog, 1)
	c:advance(25)
	eq(#c.loadLog, 1)
	c:advance(6)
	eq(#c.loadLog, 2)
	c:slash("/oc show")
	c:advance(5.5)
	eq(#c.loadLog, 3, "shown: every 5 s")
end)

local function gaps(c)
	local out = {}
	for i = 2, #c.loadLog do
		out[#out + 1] = c.loadLog[i].at - c.loadLog[i - 1].at
	end
	return out
end

test("timer back-off: doubles on empty loads, caps at 60 s shown, resets on news and on opening", function()
	local fx, c, ns = setup({ ctlPresent = false, slots = 30 })
	c:advance(2.1 + 5 + 10 + 20 + 40 + 60 + 60 + 3, 0.25)
	local g = gaps(c)
	eq(#g, 6)
	for i, want in ipairs({ 5, 10, 20, 40, 60, 60 }) do
		near(g[i], want, 0.3, "gap " .. i)
	end
	-- news resets the interval
	fx:publish(c)
	local waited = 0
	while ns.Store.gen ~= 2 and waited < 70 do
		c:advance(0.25, 0.25)
		waited = waited + 0.25
	end
	eq(ns.Store.gen, 2, "the next timer load (up to 60 s away) brings the news")
	local n = #c.loadLog
	c:advance(5.6, 0.25)
	eq(#c.loadLog, n + 1)
	near(c.loadLog[n + 1].at - c.loadLog[n].at, 5, 0.3, "back to the base interval")
	-- opening the panel resets it too
	eq(ns.Link.timerMult, 2)
	c:slash("/oc hide")
	c:slash("/oc show")
	eq(ns.Link.timerMult, 1)
	eq(ns.errorCount, 0)
end)

test("timer back-off: hidden panel starts at 30 s and caps at 120 s", function()
	local fx, c, ns = setup({ ctlPresent = false, slots = 30 })
	c:slash("/oc hide")
	c:advance(2.1 + 30 + 60 + 120 + 120 + 3, 0.25)
	local g = gaps(c)
	eq(#g, 4)
	for i, want in ipairs({ 30, 60, 120, 120 }) do
		near(g[i], want, 0.3, "gap " .. i)
	end
end)

test("session far from uiStart: timer mode until a snapshot passes the trust check", function()
	local fx, c, ns = setup({ sessionOffset = -100, agents = { agent("working", START, "demo-agent") } })
	c:advance(2.1)
	eq(#ns.Store.agents, 1, "the snapshot is still shown")
	local mode, why = ns.Link:Mode()
	eq(mode, "timer")
	eq(why, "untrusted")
	-- a doorbell is not trusted either
	fx:publish(c) -- gen 2 (same stale session), rings doorbell 1
	c:advance(1.5)
	eq(#c.loadLog, 1, "doorbell ignored while untrusted")
	-- the companion now reports the right session; the timer load picks it up
	fx.session = c:time()
	fx:publish(c) -- gen 3
	c:advance(5.5)
	eq(ns.Store.gen, 3)
	eq(ns.Link:Mode(), "doorbell")
end)

test("trust window is 15 seconds either side", function()
	local _, c, ns = setup({ sessionOffset = 15 })
	c:advance(2.1)
	eq(ns.Link:Mode(), "doorbell", "+15 s still trusted")
	local _, c2, ns2 = setup({ sessionOffset = 16 })
	c2:advance(2.1)
	eq(select(2, ns2.Link:Mode()), "untrusted", "+16 s is not")
	local _, c3, ns3 = setup({ sessionOffset = -START }) -- session = 0: not seen
	c3:advance(2.1)
	eq(select(2, ns3.Link:Mode()), "untrusted", "session 0 means the UI start was not seen")
end)

test("out of slots: stops loading and says so", function()
	local fx, c, ns = setup({ slots = 3, session = false })
	c:advance(18.5, 0.25) -- timer mode: loads at ~2 s, ~7 s, then ~17 s after one empty load
	eq(#c.loadLog, 3)
	eq(ns.Link.state, "out_of_slots")
	eq(panelStatus(c), "out of slots")
	contains(c:chatText(), "out of link slots")
	contains(c:chatText(), "/reload to refresh")
	eq(trackerKinds(c, "message")[1].text, ns.Link.OUT_OF_SLOTS_TEXT)
	c:advance(120)
	eq(#c.loadLog, 3, "no further loads")
	eq(ns.errorCount, 0)
	-- a /reload frees every slot
	c:reload()
	fx:sessionStart(c)
	c:advance(2.1)
	eq(#c.loadLog, 1, "the new UI session starts again at S000")
	eq(c.loadLog[1].name, "OmpcraftS000")
end)

test("out of slots right after the last slot loads", function()
	local fx, c, ns = setup({ slots = 1 })
	c:advance(2.1)
	eq(#c.loadLog, 1)
	eq(ns.Link.state, "out_of_slots")
	eq(#ns.Store.agents, 0)
end)

test("not installed: says so and does nothing else", function()
	local fx, c, ns = setup({ install = false })
	eq(ns.Link.state, "not_installed")
	eq(panelStatus(c), "not installed")
	eq(trackerKinds(c, "message")[1].text, "companion not installed")
	contains(c:chatText(), "companion not installed")
	c:advance(60)
	eq(#c.loadLog, 0)
	eq(#c.soundFileCalls, 0)
	eq(ns.errorCount, 0)
end)

test("self-test failure forces timer mode; /oc selftest re-checks", function()
	local fx, c, ns = setup({ ctlPresent = false })
	local mode, why = ns.Link:Mode()
	eq(mode, "timer")
	eq(why, "selftest")
	c:clearChat()
	c:slash("/oc selftest")
	contains(c:chatText(), "FAIL")
	fx:addControl()
	c:clearChat()
	c:slash("/oc selftest")
	contains(c:chatText(), "pass")
	truthy(ns.Link.selfTest.ok)
	-- no snapshot yet, so still timer, but no longer because of the self-test
	eq(select(2, ns.Link:Mode()), "no-snapshot")

	local fx2, c2, ns2 = setup({ ctlAbsent = true })
	eq(select(2, ns2.Link:Mode()), "selftest", "a negative control that reads present fails the test")
	eq(ns2.Link.selfTest.absent, true)
end)

test("a snapshot naming a doorbell beyond the signal range means timer mode", function()
	local fx, c, ns = setup({ signals = 3 })
	c:advance(2.1) -- snapshot 1 -> nextSignal 1
	eq(ns.Link:Mode(), "doorbell")
	fx:publish(c) -- nextSignal 2, rings 1
	c:advance(1.5)
	eq(ns.Store.gen, 2)
	fx:publish(c) -- nextSignal 3 == M: nothing left to watch
	c:advance(1.5)
	eq(ns.Store.gen, 3)
	local mode, why = ns.Link:Mode()
	eq(mode, "timer")
	eq(why, "signals-exhausted")
	fx:publish(c) -- the companion stopped ringing; the timer finds it
	c:advance(5.5)
	eq(ns.Store.gen, 4)
end)

test("a slot that will not load is skipped, not retried forever", function()
	local fx, c, ns = setup({ slots = 4, session = false })
	fx:removeSlot(0)
	fx:sessionStart(c)
	c:advance(2.1)
	eq(#c.loadLog, 0, "S000 is missing")
	truthy(ns.Link.skipped[0])
	contains(ns.Link.lastLoadError, "OmpcraftS000")
	c:advance(5.5)
	eq(#c.loadLog, 1)
	eq(c.loadLog[1].name, "OmpcraftS001")
	eq(ns.Link.state, "ok")
end)

test("a stale OmpcraftInbox is never mistaken for a snapshot", function()
	local fx, c, ns = setup({ session = false })
	c.env.OmpcraftInbox = { v = 0, run = 1, gen = 99, at = START, tern = "ok", agents = { agent("working", START, "ghost") } }
	c:advance(2.1)
	eq(#c.loadLog, 1)
	eq(ns.Store.hasSnapshot, false)
	eq(c.env.OmpcraftInbox, nil)
end)

test("a hand-written data.lua in the exact protocol format loads", function()
	local fx, c, ns = setup({ session = false })
	fx:writeSlot(0, table.concat({
		"OmpcraftInbox = {",
		"  v = 0,",
		"  gen = 17,",
		"  run = 1762727000123,",
		"  at = " .. START .. ",",
		"  session = " .. START .. ",",
		"  nextSignal = 5,",
		'  tern = "ok",',
		'  companion = "0.1.0",',
		"  agents = {",
		'    { id = "7881264988162", name = "demo \\"quoted\\" na||me", host = "host-a", session = "demo-session", state = "working",',
		'      since = ' .. (START - 90) .. ', model = "model-x", cwd = "project", last = "line one\\nline two \\195\\169 \\\\ end" },',
		"  },",
		"  toasts = {",
		'    { id = 1762727999123, kind = "done", agent = "7881264988162", text = "demo finished" },',
		"  },",
		"}",
		"",
	}, "\n"))
	c:advance(2.1)
	eq(ns.Store.gen, 17)
	eq(ns.Store.run, 1762727000123)
	eq(ns.Link.nextSignal, 5)
	eq(ns.Link:Mode(), "doorbell")
	local rows = agentRows(c)
	eq(rows[1].name, 'demo "quoted" na||me')
	eq(rows[1].right, "1m")
	local block = rows[1].frame
	block.__s.scripts.OnEnter(block)
	local tip = table.concat(c:tooltipLines(), "\n")
	contains(tip, "line one\nline two \195\169 \\ end")
	contains(tip, "state: working")
	contains(tip, "host-a \194\183 demo-session \194\183 model-x")
	eq(c:visibleToasts()[1], 'demo "quoted" na||me', "a toast shows the agent it is about")
	eq(ns.errorCount, 0)
end)

------------------------------------------------------------------------
-- snapshot acceptance
------------------------------------------------------------------------

test("snapshot acceptance: lower gen ignored, new run accepted", function()
	local _, c, ns = setup({ install = false })
	local S = ns.Store
	local function snap(run, gen, name)
		return { v = 0, run = run, gen = gen, at = START, session = START, nextSignal = 1, tern = "ok", agents = { agent("working", START, name) } }
	end
	eq(S:Apply(snap(10, 5, "first")), true)
	eq(S.agents[1].name, "first")
	eq(S:Apply(snap(10, 4, "older")), false, "lower gen")
	eq(S.agents[1].name, "first")
	eq(S:Apply(snap(10, 5, "same")), false, "same gen")
	eq(S:Apply(snap(10, 6, "newer")), true, "higher gen")
	eq(S:Apply(snap(11, 1, "restarted")), true, "a new run is accepted even with a lower gen")
	eq(S.gen, 1)
	eq(S.agents[1].name, "restarted")
	eq(S:Apply(snap(10, 99, "old run")), true, "any run that differs from the last applied one is accepted (PROTOCOL 7.3)")
	local bad = snap(12, 1, "bad")
	bad.v = 1
	eq(S:Apply(bad), false, "unknown protocol version")
	eq(S:Apply("junk"), false)
	eq(S:Apply({ v = 0 }), false, "missing run/gen")
	eq(ns.errorCount, 0)
end)

test("malformed agent data cannot break the store", function()
	local _, c, ns = setup({ install = false })
	local ok = ns.Store:Apply({
		v = 0, run = 1, gen = 1, at = START, session = START, tern = "ok",
		agents = { "x", { name = 5, state = 7, since = "soon", last = false }, { name = "ok", state = "idle", since = START } },
		toasts = { "x", { id = "nope" }, { id = START * 1000 + 1, kind = "done", text = 42 } },
	})
	eq(ok, true)
	eq(#ns.Store.agents, 2)
	eq(ns.errorCount, 0)
end)

------------------------------------------------------------------------
-- toasts
------------------------------------------------------------------------

test("toasts: shown once, with a sound and a chat line", function()
	local id = (START - 5) * 1000 + 1
	local fx, c, ns = setup({ toasts = { { id = id, kind = "input", agent = "a1", text = "demo-agent needs you" } } })
	c:advance(2.1)
	eq(c:visibleToasts()[1], "demo-agent needs you")
	contains(c:chatText(), "demo-agent needs you")
	contains(c:chatText(), "ompcraft")
	eq(#c.soundsPlayed, 1)
	eq(c.soundsPlayed[1], c.env.SOUNDKIT.READY_CHECK, "needs-you uses the ready-check sound")
	eq(ns.db.lastToast, id)
	-- the same toast arriving again is not shown again
	c:clearChat()
	c:slash("/oc hide")
	fx:publish(c)
	c:advance(4)
	eq(ns.Store.gen, 2)
	eq(c:visibleToasts()[1], "demo-agent needs you", "the first toast is still on screen, not repeated")
	lacks(c:chatText(), "needs you")
	eq(#c.soundsPlayed, 1)
end)

test("toasts: lastToast dedupes across /reload", function()
	local id = (START - 5) * 1000 + 1
	local fx, c, ns = setup({ toasts = { { id = id, kind = "done", agent = "a1", text = "demo-agent finished" } } })
	c:advance(2.1)
	eq(#c:visibleToasts(), 1)
	eq(ns.db.lastToast, id)
	c:reload()
	c:clearChat()
	local ns2 = c:ns()
	eq(ns2.db.lastToast, id, "lastToast survived the reload via OmpcraftDB")
	fx:sessionStart(c) -- the companion republishes its last toasts
	c:advance(2.1)
	eq(ns2.Store.hasSnapshot, true)
	eq(#c:visibleToasts(), 0, "already seen")
	lacks(c:chatText(), "finished")
	eq(#c.soundsPlayed, 1)
	-- a genuinely new toast still shows
	table.insert(fx.toasts, { id = id + 5000, kind = "input", agent = "a1", text = "demo-agent needs you" })
	c:slash("/oc hide")
	fx:publish(c)
	c:advance(4)
	eq(#c:visibleToasts(), 1)
	contains(c:chatText(), "needs you")
end)

test("toasts: older than uiStart - 30 s are skipped silently", function()
	local old = (START - 31) * 1000
	local edge = (START - 30) * 1000
	local recent = (START - 29) * 1000
	local fx, c, ns = setup({
		toasts = {
			{ id = recent, kind = "done", agent = "a", text = "recent" },
			{ id = old, kind = "input", agent = "a", text = "too old" },
			{ id = edge, kind = "done", agent = "a", text = "edge" },
		},
	})
	c:advance(2.1)
	local shown = table.concat(c:visibleToasts(), "|")
	contains(shown, "recent")
	contains(shown, "edge")
	lacks(shown, "too old")
	lacks(c:chatText(), "too old")
	eq(ns.db.lastToast, recent, "lastToast covers the skipped ones too")
	eq(#c.soundsPlayed, 1)
end)

test("toasts: held in combat, flushed when combat ends", function()
	local id = (START - 5) * 1000
	local fx, c, ns = setup({
		toasts = {
			{ id = id + 1, kind = "input", agent = "a", text = "alpha needs you" },
			{ id = id + 2, kind = "done", agent = "b", text = "beta finished" },
		},
	})
	c:setCombat(true)
	c:advance(2.1)
	eq(ns.Store.hasSnapshot, true)
	eq(#c:visibleToasts(), 0, "nothing on screen during combat")
	eq(#c.soundsPlayed, 0)
	lacks(c:chatText(), "alpha")
	eq(ns.db.lastToast, id + 2, "lastToast already raised, so they are not queued twice")
	c:advance(10)
	eq(#c:visibleToasts(), 0, "still held")
	c:setCombat(false)
	local shown = c:visibleToasts()
	eq(#shown, 2)
	eq(shown[1], "alpha needs you")
	eq(shown[2], "beta finished")
	contains(c:chatText(), "alpha needs you")
	eq(#c.soundsPlayed, 1, "one sound for the whole flush")
	-- a second snapshot in the same combat does not duplicate
	eq(select(2, ns.Toasts:Counts()), 0)
end)

test("toasts: three at a time, seven seconds each, fade out, click to dismiss", function()
	local base = (START - 5) * 1000
	local toasts = {}
	for i = 1, 5 do
		toasts[#toasts + 1] = { id = base + i, kind = "done", agent = "a", text = "toast " .. i }
	end
	local fx, c, ns = setup({ toasts = toasts })
	c:advance(2.1)
	local visible, queued = ns.Toasts:Counts()
	eq(visible, 3)
	eq(queued, 2)
	eq(c:visibleToasts()[1], "toast 1")
	eq(#c.chat > 0, true)
	lacks(c:chatText(), "toast 4", "queued toasts have no chat line yet")

	c:advance(6.6) -- 6.7 s after they appeared: fading
	local f1 = c.env.OmpcraftToast1
	truthy(f1.__s.alpha < 1 and f1.__s.alpha > 0, "alpha during the fade: " .. tostring(f1.__s.alpha))
	c:advance(0.6) -- past 7 s
	local shown = c:visibleToasts()
	eq(#shown, 2, "the two queued toasts took the free frames")
	eq(shown[1], "toast 4")
	eq(f1.__s.alpha, 1, "a reused frame starts opaque")
	contains(c:chatText(), "toast 5")

	-- click dismisses one
	local frame = c.env.OmpcraftToast1
	frame.__s.scripts.OnMouseUp(frame, "LeftButton")
	eq(#c:visibleToasts(), 1)
	c:advance(8)
	eq(#c:visibleToasts(), 0)
	eq(ns.errorCount, 0)
end)
test("/oc status prints mode, slots, nextSignal, gen/run, age and self-test", function()
	local fx, c, ns = setup({ agents = { agent("working", START, "a") } })
	c:advance(5)
	c:clearChat()
	c:slash("/oc status")
	local out = c:chatText()
	contains(out, "|cff7fbfffompcraft|r:")
	contains(out, "mode: doorbell")
	contains(out, "slots: 1/6 used")
	contains(out, "nextSignal: 1 of 40")
	contains(out, "gen 1")
	contains(out, "run 1762727000123")
	contains(out, "age 5s")
	contains(out, "self-test: pass")
	contains(out, "doorbells: 0, timer loads: 0, false rings: 0")
	contains(out, "link: live")
	local fx2, c2 = setup({ install = false })
	c2:clearChat()
	c2:slash("/oc status")
	contains(c2:chatText(), "link: not installed")
	contains(c2:chatText(), "snapshot: none yet")
end)

test("/oc help and unknown commands", function()
	local _, c = setup({ install = false })
	c:clearChat()
	c:slash("/oc help")
	truthy(#c.chat >= 7)
	contains(c:chatText(), "/oc status")
	c:clearChat()
	c:slash("/oc bogus")
	contains(c:chatText(), "unknown command")
end)

test("errors are caught, kept in a ring buffer and printed by /oc errors", function()
	local _, c, ns = setup({ install = false })
	ns:Listen("SNAPSHOT", function()
		error("boom")
	end)
	local function snap(gen)
		return { v = 0, run = 1, gen = gen, at = START, session = START, tern = "ok", agents = {} }
	end
	eq(ns.Store:Apply(snap(1)), true, "a failing listener does not fail the apply")
	eq(ns.errorCount, 1)
	eq(ns.Store:Apply(snap(2)), true)
	eq(ns.errorCount, 2)
	eq(#ns.errors, 1, "the same error is counted, not repeated")
	eq(ns.errors[1].count, 2)

	-- event handlers and guarded callbacks are protected too
	ns:On("PLAYER_REGEN_ENABLED", function()
		error("regen handler")
	end)
	c:setCombat(false) -- must not throw
	ns.Guard("timer", function()
		error("in a timer")
	end)()
	for i = 1, 12 do
		ns:RecordError("flood", "distinct " .. i)
	end
	eq(#ns.errors, 10, "ring buffer is capped")
	eq(ns.errors[#ns.errors].msg, "distinct 12")

	c:clearChat()
	c:slash("/oc errors")
	contains(c:chatText(), "distinct 12")
	contains(c:chatText(), "[flood]")

	local _, c2, ns2 = setup({ install = false })
	c2:clearChat()
	c2:slash("/oc errors")
	contains(c2:chatText(), "no errors")
end)

test("error text with | is made safe before printing", function()
	local _, c, ns = setup({ install = false })
	ns:RecordError("x", "bad |cffff0000 markup")
	c:clearChat()
	c:slash("/oc errors")
	truthy(pipesBalanced((c:chatText():gsub("|cff7fbfffompcraft|r:", ""))))
end)

test("addon compartment hooks work", function()
	local fx, c, ns = setup({ agents = {
		agent("waiting_input", START, "a", { id = "w1" }),
		agent("working", START, "b", { id = "w2" }),
		agent("idle", START - 10, "c", { id = "w3", doneAt = START - 5 }),
	} })
	c:advance(2.1)
	local env = c.env
	env.Ompcraft_OnAddonCompartmentClick("Ompcraft", "LeftButton")
	eq(ns.Tracker:IsOpen(), false)
	env.Ompcraft_OnAddonCompartmentClick("Ompcraft", "LeftButton")
	eq(ns.Tracker:IsOpen(), true)
	c:clearChat()
	env.Ompcraft_OnAddonCompartmentClick("Ompcraft", "RightButton")
	eq(c.openedCategory, 1, "right-click opens the settings page")
	local button = env.UIParent
	env.Ompcraft_OnAddonCompartmentEnter("Ompcraft", button)
	local tip = table.concat(c:tooltipLines(), "\n")
	contains(tip, "Ompcraft")
	contains(tip, "Needs you: 1")
	contains(tip, "Working: 1")
	contains(tip, "Finished, unseen: 1")
	contains(tip, "Link: live")
	env.Ompcraft_OnAddonCompartmentLeave("Ompcraft", button)
	eq(env.GameTooltip.__s.shown, false)
	eq(ns.errorCount, 0)
end)

------------------------------------------------------------------------
-- hygiene
------------------------------------------------------------------------

test("only the expected globals are written", function()
	local fx, c, ns = setup({ agents = { agent("working", START, "a") }, toasts = { { id = (START - 1) * 1000, kind = "done", agent = "a", text = "t" } } })
	c:advance(30)
	c:slash("/oc status")
	c:slash("/oc selftest")
	local allowed = {
		OmpcraftDB = true, OmpcraftInbox = true, SLASH_OMPCRAFT1 = true, SLASH_OMPCRAFT2 = true,
		Ompcraft_OnAddonCompartmentClick = true, Ompcraft_OnAddonCompartmentEnter = true,
		Ompcraft_OnAddonCompartmentLeave = true,
	}
	for name in pairs(c.globalWrites) do
		truthy(allowed[name], "leaked global: " .. name)
	end
	eq(ns.errorCount, 0)
end)

test("the TOC lists every addon file and the compartment hooks exist", function()
	local fx, c = setup({ install = false })
	eq(c:tocField("Interface"), "16001")
	eq(c:tocField("Title"), "Ompcraft")
	eq(c:tocField("Author"), "TechDufus")
	eq(c:tocField("Version"), "0.1.0")
	eq(c:tocField("SavedVariables"), "OmpcraftDB")
	for _, field in ipairs({ "AddonCompartmentFunc", "AddonCompartmentFuncOnEnter", "AddonCompartmentFuncOnLeave" }) do
		local fn = c:tocField(field)
		truthy(fn and type(c.env[fn]) == "function", field .. " names a global function")
	end
	truthy(c:tocField("IconTexture"):match("^Interface\\Icons\\"), "built-in icon")
	local listed = {}
	for _, f in ipairs(c:addonFiles()) do
		listed[f] = true
	end
	-- every .lua under Ompcraft/ is in the TOC
	local p = io.popen("cd '" .. ADDON_DIR .. "' && find . -name '*.lua' | sed 's|^\\./||' | sort")
	for line in p:lines() do
		truthy(listed[line], line .. " is not listed in the TOC")
	end
	p:close()
end)

test("no secure or protected API in the addon source", function()
	local forbidden = {
		"ReloadUI", "SendChatMessage", "SecureActionButtonTemplate", "SecureHandlerBaseTemplate", "SecureUnitButtonTemplate",
		"RunMacro", "CastSpellByName", "UseAction", "TargetUnit", "SetBinding", "securecall", "loadstring", "RunScript",
		"EnableAddOn", "DisableAddOn", "SaveAddOns", "ShowUIPanel", "ToggleFrame", "LoadAddOn%(", "IsAddOnLoaded%(",
		"GetAddOnMetadata%(",
	}
	local p = io.popen("cd '" .. ADDON_DIR .. "' && find . -name '*.lua'")
	for file in p:lines() do
		local f = assert(io.open(ADDON_DIR .. "/" .. file, "rb"))
		local src = f:read("*a")
		f:close()
		-- strip comments so documentation may mention them
		src = src:gsub("%-%-[^\n]*", "")
		for _, word in ipairs(forbidden) do
			-- the C_AddOns.* forms are the only allowed spelling of the add-on APIs
			local hit = src:find("[^_%.%w]" .. word)
			truthy(not hit, file .. " uses " .. word)
		end
	end
	p:close()
end)

local H = {
	test = test, eq = eq, truthy = truthy, contains = contains, lacks = lacks, near = near, setup = setup,
	agent = agent, START = START, pipesBalanced = pipesBalanced, panelStatus = panelStatus,
	trackerKinds = trackerKinds, agentRows = agentRows,
}
require("ui")(H)
require("settings")(H)

------------------------------------------------------------------------
-- run
------------------------------------------------------------------------

local failed, passed = 0, 0
for _, t in ipairs(tests) do
	local ok, err = pcall(t.fn)
	if ok then
		passed = passed + 1
		print("ok   " .. t.name)
	else
		failed = failed + 1
		print("FAIL " .. t.name .. "\n     " .. tostring(err))
	end
end
for _, fx in ipairs(cleanups) do
	fx:cleanup()
end
print(string.format("\n%d passed, %d failed", passed, failed))
os.exit(failed == 0 and 0 or 1)
