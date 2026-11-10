-- UI tests: the tracker, the detail page, toasts and the look's hard facts.
-- Loaded by run.lua with its helpers.

return function(H)
	local test, eq, truthy, contains, lacks, near = H.test, H.eq, H.truthy, H.contains, H.lacks, H.near
	local setup, START, pipesBalanced = H.setup, H.START, H.pipesBalanced
	local trackerKinds, agentRows = H.trackerKinds, H.agentRows
	local Stub = require("wowstub")

	local gen = 0
	-- An agent as the companion would publish it.
	local function A(id, state, name, extra)
		local a = { id = id, name = name, state = state, since = START - 10, model = "model-x", cwd = "project", last = "", host = "", session = "demo-session" }
		for k, v in pairs(extra or {}) do
			a[k] = v
		end
		return a
	end

	-- Applies a snapshot straight to the store (the link has its own tests).
	local function put(ns, agents, extra)
		gen = gen + 1
		local snap = { v = 0, run = 1, gen = gen, at = START, session = START, nextSignal = 1, tern = "ok", agents = agents }
		for k, v in pairs(extra or {}) do
			snap[k] = v
		end
		return ns.Store:Apply(snap)
	end

	local function names(c, kind)
		local out = {}
		for _, e in ipairs(trackerKinds(c, kind)) do
			out[#out + 1] = e.name or e.text
		end
		return out
	end

	local function click(frame)
		frame.__s.scripts.OnClick(frame)
	end

	-- element kinds top to bottom; the "companion not installed" line of the
	-- install-less fixtures is left out
	local function kinds(c)
		local out = {}
		for _, e in ipairs(c:tracker()) do
			if e.kind ~= "message" then
				out[#out + 1] = e.kind
			end
		end
		return table.concat(out, ",")
	end

	local function plain(s)
		return (s:gsub("|c%x%x%x%x%x%x%x%x", ""):gsub("|r", ""))
	end

	----------------------------------------------------------------------
	-- the tracker
	----------------------------------------------------------------------

	test("tracker: gold sections with counts, quest markers, objective lines per state", function()
		local _, c, ns = setup({ install = false })
		ns.db.seen["i2"] = START - 800 -- seen long ago
		put(ns, {
			A("n1", "waiting_input", "needs-one", { section = "infra", since = START - 90, preview = "Should I deploy?" }),
			A("w1", "working", "busy", { section = "infra", since = START - 30, activity = "Running tests", todo = "3/7" }),
			A("w2", "working", "quiet", { section = "infra", since = START - 5 }),
			A("u1", "idle", "fresh", { section = "docs-site · host-a", since = START - 60, doneAt = START - 50, stats = "1m 12s \194\183 22K tok", preview = "All green" }),
			A("i1", "idle", "old-one", { section = "docs-site · host-a", since = 0 }),
			A("i2", "idle", "old-two", { section = "docs-site · host-a", since = START - 900, doneAt = START - 800 }),
			A("x1", "exited", "gone", { section = "docs-site · host-a", since = START - 3000, last = "exit code 1" }),
		})
		eq(kinds(c), "header,section,agent,agent,section,agent,agent,agent,idle")
		local t = c:tracker()
		eq(t[1].text, "Agents")
		eq(t[2].text, "docs-site · host-a (2)", "sections sort by name; the count is of the agents shown under it")
		eq(t[5].text, "infra (3)")

		local rows = agentRows(c)
		eq(rows[1].name, "fresh")
		eq(rows[2].name, "gone")
		eq(rows[3].name, "needs-one")
		eq(rows[4].name, "quiet", "within a section, the store's order: newest first")
		eq(rows[5].name, "busy")
		eq(t[9].text, "2 idle", "one idle line, last")

		-- markers: yellow ? unseen, red ! exited, yellow ! needs you, grey ? working
		eq(rows[1].marker.texture, "Interface/GossipFrame/ActiveQuestIcon")
		eq(rows[2].marker.texture, "Interface/GossipFrame/AvailableQuestIcon")
		eq(rows[2].marker.desaturated, true)
		truthy(rows[2].marker.vertex[1] == 1 and rows[2].marker.vertex[2] < 0.3, "red")
		eq(rows[3].marker.texture, "Interface/GossipFrame/AvailableQuestIcon")
		eq(rows[3].marker.desaturated, false)
		eq(rows[4].marker.atlas, "SideInProgressquesticon")
		eq(rows[5].marker.atlas, "SideInProgressquesticon")

		-- objective lines in the tracker's dash style
		eq(#rows[1].lines, 2)
		eq(rows[1].lines[1].text, "1m 12s \194\183 22K tok")
		eq(rows[1].lines[1].dash, "- ")
		eq(rows[1].lines[2].text, "All green")
		eq(rows[2].lines[1].text, "exit code 1")
		eq(rows[3].lines[1].text, "Should I deploy?")
		eq(rows[3].right, "1m")
		eq(rows[5].lines[1].text, "Running tests")
		eq(rows[5].lines[1].pulsing, true, "the activity pulses")
		eq(rows[5].lines[2].text, "Todo 3/7")
		eq(rows[5].lines[2].pulsing, false)
		eq(rows[5].right, "30s")
		eq(#rows[1].lines + #rows[2].lines, 3)

		-- a working agent with no tool in flight is thinking, dimly, with the same pulse
		eq(rows[4].lines[1].text, "Thinking\226\128\166")
		eq(rows[4].lines[1].pulsing, true)
		eq(rows[4].lines[1].color[1], 0.6, "dim")
		eq(rows[2].lines[1].color[1], 0.8, "failed lines use the tracker's red")
		eq(rows[2].lines[1].color[2], 0.1)
		eq(ns.errorCount, 0)
	end)

	test("tracker: idle, seen agents have no marker and hide behind a count", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("i1", "idle", "calm", { section = "S" }), A("w", "working", "busy", { section = "S" }) })
		eq(kinds(c), "header,section,agent,idle")
		eq(trackerKinds(c, "idle")[1].text, "1 idle")
		eq(trackerKinds(c, "idle")[1].open, false)
		put(ns, { A("w", "working", "busy", { section = "S" }) })
		eq(#trackerKinds(c, "idle"), 0)
	end)

	test("tracker: one idle line expands to the idle agents, grouped by section, and persists", function()
		local fx, c, ns = setup({ install = false })
		local agents = {
			A("i1", "idle", "calm-one", { section = "S" }), A("i2", "idle", "calm-two", { section = "S" }),
			A("w", "working", "busy", { section = "S" }), A("o", "idle", "other", { section = "T" }),
		}
		put(ns, agents)
		eq(kinds(c), "header,section,agent,idle", "T has nothing active, so no header")
		eq(trackerKinds(c, "idle")[1].text, "3 idle")
		click(trackerKinds(c, "idle")[1].frame)
		eq(ns.db.idleOpen, true)
		eq(kinds(c), "header,section,agent,idle,label,agent,agent,label,agent")
		eq(trackerKinds(c, "label")[1].text, "S")
		eq(trackerKinds(c, "label")[2].text, "T")
		local rows = agentRows(c)
		eq(#rows, 4, "the busy agent and the three idle ones")
		eq(rows[2].name, "calm-one")
		eq(rows[2].markerShown, false)
		eq(rows[2].state, "idle")
		eq(#rows[2].lines, 0)
		eq(rows[4].name, "other")
		truthy(rows[2].frame.name.__s.textColor[1] < 0.7, "idle titles are dim")
		truthy(trackerKinds(c, "label")[1].frame == nil)
		c:reload()
		local ns2 = c:ns()
		put(ns2, agents)
		eq(#agentRows(c), 4, "still expanded after /reload")
		click(trackerKinds(c, "idle")[1].frame)
		eq(ns2.db.idleOpen, false)
		eq(#agentRows(c), 1)
		eq(#trackerKinds(c, "label"), 0)
	end)

	-- The shape of a real snapshot: 12 sessions, one agent needs you, one is
	-- working, and 18 idle agents sit in 11 of the sessions.
	test("tracker: 12 sessions, 1 needs you, 1 working, 18 idle: both active agents show, one idle line", function()
		local _, c, ns = setup({ install = false })
		local agents = {
			A("n", "waiting_input", "asker", { section = "Session 01", since = START - 90, preview = "Should I deploy?" }),
			A("w", "working", "busy", { section = "Session 02", since = START - 30, activity = "Running tests", todo = "3/7" }),
		}
		local perSection = { 2, 2, 2, 2, 2, 2, 2, 1, 1, 1, 1 } -- sessions 02..12, 18 in all
		local n = 0
		for i, count in ipairs(perSection) do
			for k = 1, count do
				n = n + 1
				agents[#agents + 1] = A("i" .. n, "idle", string.format("idle-%02d-%d", i + 1, k), { section = string.format("Session %02d", i + 1), since = 0 })
			end
		end
		eq(n, 18)
		put(ns, agents)
		eq(kinds(c), "header,section,agent,section,agent,idle", "two headers, not twelve")
		local rows = agentRows(c)
		eq(rows[1].name, "asker")
		eq(rows[1].lines[1].text, "Should I deploy?")
		eq(rows[2].name, "busy")
		eq(rows[2].lines[1].text, "Running tests")
		eq(rows[2].lines[2].text, "Todo 3/7")
		eq(#trackerKinds(c, "idle"), 1)
		eq(trackerKinds(c, "idle")[1].text, "18 idle")
		eq(#trackerKinds(c, "more"), 0, "idle agents that are merely folded are not hidden")
		eq(trackerKinds(c, "section")[1].text, "Session 01 (1)")
		eq(trackerKinds(c, "section")[2].text, "Session 02 (1)")

		-- opened: sub-labels only for the 11 sections with idle agents, no gold headers
		click(trackerKinds(c, "idle")[1].frame)
		eq(#trackerKinds(c, "section"), 2, "still only two headers")
		local labels = trackerKinds(c, "label")
		truthy(#labels >= 1, "sub-labels")
		eq(labels[1].text, "Session 02")
		for _, l in ipairs(labels) do
			lacks(l.text, "(", "sub-labels carry no count")
		end
		rows = agentRows(c)
		eq(rows[1].name, "asker", "the active agents stay first")
		eq(rows[2].name, "busy")
		local shownIdle = #rows - 2
		local more = trackerKinds(c, "more")[1]
		truthy(more, "the idle list is longer than the room")
		eq(more.text, string.format("+%d more", 18 - shownIdle), "only agents the height budget hid")
		truthy(ns.Tracker.frame:GetHeight() <= 440)
		-- shut again: nothing hidden
		click(trackerKinds(c, "idle")[1].frame)
		eq(#trackerKinds(c, "more"), 0)
		eq(ns.errorCount, 0)
	end)

	test("tracker: section headers collapse and remember it", function()
		local _, c, ns = setup({ install = false })
		local agents = { A("a", "working", "one", { section = "S" }), A("b", "working", "two", { section = "S" }), A("c", "working", "three", { section = "T" }) }
		put(ns, agents)
		local section = trackerKinds(c, "section")[1]
		eq(section.text, "S (2)")
		click(section.frame)
		eq(ns.db.collapsedSections.S, true)
		eq(kinds(c), "header,section,section,agent")
		eq(trackerKinds(c, "section")[1].collapsed, true)
		eq(trackerKinds(c, "section")[1].text, "S (2)", "the count stays when collapsed")
		eq(trackerKinds(c, "section")[1].frame.min.face.__s.atlas, "ui-questtrackerbutton-secondary-expand")
		c:reload()
		put(c:ns(), agents)
		eq(kinds(c), "header,section,section,agent")
		click(trackerKinds(c, "section")[1].frame)
		eq(kinds(c), "header,section,agent,agent,section,agent")
		eq(trackerKinds(c, "section")[1].frame.min.face.__s.atlas, "ui-questtrackerbutton-secondary-collapse")
	end)

	test("tracker: the Agents header collapses everything", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("a", "working", "one", { section = "S" }) })
		local frame = ns.Tracker.frame
		local tall = frame:GetHeight()
		local header = ns.Tracker.header
		click(header)
		eq(kinds(c), "header")
		eq(ns.db.collapsed, true)
		eq(frame:GetHeight(), 32)
		truthy(tall > 32)
		eq(header.min.face.__s.atlas, "ui-questtrackerbutton-expand-all")
		click(header.min)
		eq(ns.db.collapsed, false)
		eq(kinds(c), "header,section,agent")
		eq(header.min.face.__s.atlas, "ui-questtrackerbutton-collapse-all")
	end)

	test("tracker: elapsed from since, none when unknown, ticking only while shown", function()
		local _, c, ns = setup({ install = false })
		put(ns, {
			A("a", "working", "known", { section = "S", since = START - 50 }),
			A("b", "working", "unknown", { section = "S", since = 0 }),
			A("c", "waiting_input", "waiting", { section = "S", since = 0 }),
		})
		local by = {}
		for _, e in ipairs(agentRows(c)) do
			by[e.name] = e.right
		end
		eq(by.known, "50s")
		eq(by.unknown, "", "since == 0 shows no time")
		eq(by.waiting, "")
		c:advance(15)
		by = {}
		for _, e in ipairs(agentRows(c)) do
			by[e.name] = e.right
		end
		eq(by.known, "1m")
		truthy(ns.Tracker.ticker, "ticking while shown")
		c:slash("/oc hide")
		eq(ns.Tracker.ticker, nil, "no ticker while hidden")
		for _, b in ipairs(ns.Tracker.blocks) do
			for _, line in ipairs(b.lines) do
				eq(line.group:IsPlaying(), false, "no pulse while hidden")
			end
		end
		c:slash("/oc show")
		truthy(ns.Tracker.ticker)
	end)

	test("tracker: a working agent's activity pulses only while visible", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("a", "working", "one", { section = "S", activity = "Running tests" }) })
		local line = ns.Tracker.blocks[1].lines[1]
		eq(line.group:IsPlaying(), true)
		eq(line.group.__s.looping, "BOUNCE")
		c:slash("/oc hide")
		eq(line.group:IsPlaying(), false)
		c:slash("/oc show")
		eq(line.group:IsPlaying(), true)
		put(ns, { A("a", "waiting_input", "one", { section = "S", preview = "question" }) })
		eq(line.group:IsPlaying(), false, "stops when the line stops being activity")
		eq(line.text.__s.alpha, 1)
	end)

	test("tracker: stops at its height with +N more, and never drops a needs-you agent", function()
		local _, c, ns = setup({ install = false })
		local agents = {}
		for i = 1, 5 do
			agents[#agents + 1] = A("n" .. i, "waiting_input", "needs-" .. i, { section = "S", preview = "question " .. i })
		end
		for i = 1, 30 do
			agents[#agents + 1] = A("w" .. i, "working", "busy-" .. i, { section = "S", since = START - i, activity = "Running", todo = "1/2" })
		end
		put(ns, agents)
		local rows = agentRows(c)
		local needs = 0
		for _, r in ipairs(rows) do
			if r.state == "needs" then
				needs = needs + 1
			end
		end
		eq(needs, 5)
		local more = trackerKinds(c, "more")[1]
		truthy(more, "a +N more line")
		eq(more.text, string.format("+%d more", 35 - #rows))
		truthy(#rows < 35 and #rows > 5)
		truthy(ns.Tracker.frame:GetHeight() <= 440, "within the height cap: " .. ns.Tracker.frame:GetHeight())
		-- collapsing the section frees the room
		click(trackerKinds(c, "section")[1].frame)
		eq(#trackerKinds(c, "more"), 0)
	end)

	test("tracker: link status is a small dot and word, only when the link is not live", function()
		local fx, c, ns = setup({ agents = { A("a", "working", "one", { section = "S" }) } })
		c:advance(2.1)
		local h = c:tracker()[1]
		eq(h.status, "", "nothing while live")
		eq(h.dotShown, false)
		fx.tern = "down"
		fx:publish(c)
		c:advance(1.5)
		h = c:tracker()[1]
		eq(h.status, "Tern down")
		eq(h.dotShown, true)
		local _, c2 = setup({ install = false })
		h = c2:tracker()[1]
		eq(h.status, "not installed")
		eq(h.dotShown, true)
	end)

	test("tracker: empty and broken states", function()
		local fx, c, ns = setup({ session = false })
		eq(trackerKinds(c, "empty")[1].text, "ompcraft companion not running")
		fx:sessionStart(c)
		c:advance(2.1)
		eq(trackerKinds(c, "empty")[1].text, "No agents. Start one in Tern.")
		local _, c2 = setup({ install = false })
		eq(trackerKinds(c2, "message")[1].text, "companion not installed")
	end)

	test("tracker: names and previews containing | render without breaking", function()
		local _, c, ns = setup({ install = false })
		put(ns, {
			A("a", "waiting_input", "build|test", { section = "S|T", preview = "| Input | $2 | $4 |" }),
			A("b", "working", "|cffff0000red|r", { section = "S|T", activity = string.rep("x", 40) .. "|" .. string.rep("y", 30) }),
		})
		local rows = agentRows(c)
		eq(rows[1].name, "build||test")
		eq(rows[1].lines[1].text, "|| Input || $2 || $4 ||")
		eq(rows[2].name, "||cffff0000red||r")
		for _, e in ipairs(c:tracker()) do
			for _, s in ipairs({ e.text or "", e.name or "", e.right or "" }) do
				truthy(pipesBalanced(s), "odd pipe run: " .. s)
			end
			for _, l in ipairs(e.lines or {}) do
				truthy(pipesBalanced(l.text), "odd pipe run: " .. l.text)
			end
		end
		eq(trackerKinds(c, "section")[1].text, "S||T (2)")
		eq(ns.errorCount, 0)
	end)

	test("tracker: hovering shows the agent's details", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("a", "waiting_input", "demo-agent", { section = "S", host = "host-a", preview = "Should I continue?", stats = "43s", last = "older text" }) })
		local frame = agentRows(c)[1].frame
		frame.__s.scripts.OnEnter(frame)
		local lines = c:tooltipLines()
		eq(lines[1], "demo-agent")
		eq(lines[2], "host-a \194\183 demo-session \194\183 model-x")
		eq(lines[3], "cwd: project")
		eq(lines[4], "state: waiting for you")
		eq(lines[5], "last turn: 43s")
		eq(lines[6], "Should I continue?")
		eq(c.env.GameTooltip.__s.lines[6].wrap, true)
		-- the row lights up like a quest under the mouse
		eq(frame.name.__s.textColor[1], 1)
		eq(frame.name.__s.textColor[2], 0.82)
		frame.__s.scripts.OnLeave(frame)
		eq(c.env.GameTooltip.__s.shown, false)
		eq(frame.name.__s.textColor[1], 0.75)
	end)

	test("tracker: uses the objective tracker's fonts and palette, or the closest GameFont", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("a", "working", "one", { section = "S", activity = "Running" }) })
		local block = ns.Tracker.blocks[1]
		eq(block.name.__s.font, "ObjectiveTrackerLineFont")
		eq(ns.Tracker.header.text.__s.font, "ObjectiveTrackerHeaderFont")
		eq(trackerKinds(c, "section")[1].frame.text.__s.font, "ObjectiveTrackerHeaderFont")
		eq(block.lines[1].text.__s.font, "ObjectiveTrackerLineFont")
		eq(block.name.__s.textColor[2], 0.61, "the tracker's Header colour")

		local _, c2, ns2 = setup({ install = false, noTrackerFonts = true })
		put(ns2, { A("a", "working", "one", { section = "S", activity = "Running" }) })
		eq(ns2.Tracker.blocks[1].name.__s.font, "GameFontNormalSmall")
		eq(ns2.Tracker.header.text.__s.font, "GameFontNormal")
		eq(ns2.Tracker.blocks[1].name.__s.textColor[2], 0.61, "fallback palette matches")
		eq(ns2.errorCount, 0)
	end)

	test("tracker: anchored under Blizzard's tracker when it is shown, else top right", function()
		local _, c, ns = setup({ install = false, tracker = { shown = true, bottom = 500 }, saved = { version = 4, anchor = "tracker" } })
		local f = ns.Tracker.frame
		local otf = c.env.ObjectiveTrackerFrame
		local point, rel, relPoint, x, y = f:GetPoint(1)
		eq(point, "TOPRIGHT")
		eq(rel, otf.NineSlice, "anchored to the tracker's backdrop")
		eq(relPoint, "BOTTOMRIGHT")
		eq(x, -5)
		eq(y, -12)
		eq(f:GetParent(), c.env.UIParent, "never a child of Blizzard's frames")
		for _, child in ipairs(otf.__s.children) do
			truthy(child ~= f, "not parented into the tracker")
		end
		eq(#otf.NineSlice.__s.children, 0, "nothing was added to it")

		-- it is hidden or collapses away: back to the corner (the light ticker notices)
		otf.__s.shown = false
		c:advance(1.1)
		point, rel, relPoint, x, y = f:GetPoint(1)
		eq(rel, c.env.UIParent)
		eq(relPoint, "TOPRIGHT")
		eq(x, -24)
		eq(y, -300)
		otf.__s.shown = true
		c:advance(1.1)
		eq(select(2, f:GetPoint(1)), otf.NineSlice, "and under it again")
		otf.NineSlice.__s.bottom = nil -- not laid out yet
		c:advance(1.1)
		eq(select(2, f:GetPoint(1)), c.env.UIParent)

		-- without the objective tracker at all
		local _, c2, ns2 = setup({ install = false, saved = { version = 4, anchor = "tracker" } })
		local p2, r2, rp2, x2, y2 = ns2.Tracker.frame:GetPoint(1)
		eq(p2, "TOPRIGHT")
		eq(r2, c2.env.UIParent)
		eq(rp2, "TOPRIGHT")
		eq(x2, -24)
		eq(y2, -300)
		eq(ns2.errorCount, 0)
	end)

	test("tracker: locked by default; unlock to drag; a saved spot beats the anchor; reset follows it again", function()
		local _, c, ns = setup({ install = false, tracker = { shown = true, bottom = 500 }, saved = { version = 4, anchor = "tracker" } })
		local f = ns.Tracker.frame
		eq(ns.db.locked, true)
		local header = ns.Tracker.header
		local function drag(x, y)
			header.__s.scripts.OnDragStart(header)
			if f.__s.moving then
				f:ClearAllPoints()
				f:SetPoint("TOPLEFT", c.env.UIParent, "TOPLEFT", x, y)
			end
			header.__s.scripts.OnDragStop(header)
		end
		drag(300, -120)
		eq(ns.db.pos, nil, "locked: no move, nothing saved")
		c:slash("/oc unlock")
		eq(ns.db.locked, false)
		drag(300, -120)
		eq(ns.db.pos.x, 300)
		eq(ns.db.pos.y, -120)
		eq(f.__s.moving, false)
		c:advance(1.1)
		eq(select(2, f:GetPoint(1)), c.env.UIParent, "the dragged spot stays")
		c:slash("/oc lock")
		eq(ns.db.locked, true)
		c:reload()
		local rel, _, x = select(2, c.env.OmpcraftTracker:GetPoint(1)), nil, select(4, c.env.OmpcraftTracker:GetPoint(1))
		eq(x, 300)
		c:slash("/oc reset")
		eq(c:ns().db.pos, nil)
		eq(select(2, c.env.OmpcraftTracker:GetPoint(1)), c.env.ObjectiveTrackerFrame.NineSlice)
	end)

	test("tracker: a click that ends a drag is not a click", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("a", "working", "one", { section = "S" }) })
		c:slash("/oc unlock")
		local row = agentRows(c)[1].frame
		row.__s.scripts.OnDragStart(row)
		eq(ns.Tracker.frame.__s.moving, true, "rows hand drags to the tracker")
		row.__s.scripts.OnDragStop(row)
		click(row)
		eq(rawget(c.env, "OmpcraftDetail"), nil, "no detail page opened")
		c:advance(0.5)
		click(row)
		truthy(rawget(c.env, "OmpcraftDetail") and c.env.OmpcraftDetail.__s.shown)
	end)

	test("tracker: /oc toggles, shows and hides; /ompcraft is an alias", function()
		local _, c, ns = setup({ install = false })
		eq(ns.Tracker:IsOpen(), true)
		c:slash("/oc")
		eq(ns.Tracker:IsOpen(), false)
		eq(ns.db.shown, false)
		c:slash("/ompcraft")
		eq(ns.Tracker:IsOpen(), true)
		c:slash("/oc hide")
		eq(ns.Tracker:IsOpen(), false)
		c:slash("/oc show")
		eq(ns.Tracker:IsOpen(), true)
		c:slash("/oc  HIDE  ")
		c:reload()
		eq(c:ns().Tracker:IsOpen(), false, "hidden state is remembered")
		c:clearChat()
		c:slash("/oc detail")
		contains(c:chatText(), "unknown command", "the old two-line mode is gone")
	end)

	test("tracker: /oc scale sets, validates, persists; the spot keeps its place", function()
		local _, c, ns = setup({ install = false })
		eq(ns.db.scale, 1)
		eq(c.env.OmpcraftTracker.__s.scale, 1)
		c:slash("/oc scale 1.2")
		eq(ns.db.scale, 1.2)
		eq(c.env.OmpcraftTracker.__s.scale, 1.2)
		for _, bad in ipairs({ "3", "0.1", "abc", "" }) do
			c:clearChat()
			c:slash("/oc scale " .. bad)
			contains(c:chatText(), "usage: /oc scale")
			eq(ns.db.scale, 1.2, "unchanged by " .. bad)
		end
		c:slash("/oc scale 1")
		c:slash("/oc unlock")
		local f, header = c.env.OmpcraftTracker, ns.Tracker.header
		header.__s.scripts.OnDragStart(header)
		f:ClearAllPoints()
		f:SetPoint("TOPLEFT", c.env.UIParent, "TOPLEFT", 200, -100)
		header.__s.scripts.OnDragStop(header)
		c:slash("/oc scale 0.8")
		near(ns.db.pos.x, 250, 1e-6)
		near(ns.db.pos.y, -125, 1e-6)
		c:reload()
		eq(c:ns().db.scale, 0.8)
		eq(c.env.OmpcraftTracker.__s.scale, 0.8)
		c:clearChat()
		c:slash("/oc help")
		contains(c:chatText(), "/oc scale")
		lacks(c:chatText(), "/oc detail")
	end)

	test("tracker: sits on top of the chat frame and unit frames, with no box", function()
		local _, c, ns = setup({ install = false })
		local f = c.env.OmpcraftTracker
		eq(f.__s.strata, "HIGH")
		eq(f.__s.backdrop, nil, "no backdrop")
		eq(f.__s.template, nil)
		eq(f.__s.clamped, true)
		eq(f:GetWidth(), 260)
	end)

	----------------------------------------------------------------------
	-- seen tracking
	----------------------------------------------------------------------

	test("seen: finished, unseen is a yellow ?; hovering it for a second marks it seen", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("u1", "idle", "fresh", { section = "S", doneAt = START - 20 }) })
		local row = agentRows(c)[1]
		eq(row.state, "unseen")
		eq(row.marker.texture, "Interface/GossipFrame/ActiveQuestIcon")
		row.frame.__s.scripts.OnEnter(row.frame)
		c:advance(0.9)
		eq(ns.db.seen.u1, nil, "not yet")
		c:advance(0.2)
		eq(ns.db.seen.u1, START - 20)
		eq(#agentRows(c), 0, "now an idle, seen agent")
		eq(trackerKinds(c, "idle")[1].text, "1 idle")

		-- leaving early cancels it
		put(ns, { A("u2", "idle", "fresher", { section = "S", doneAt = START - 5 }) })
		local frame = agentRows(c)[1].frame
		frame.__s.scripts.OnEnter(frame)
		c:advance(0.5)
		frame.__s.scripts.OnLeave(frame)
		c:advance(2)
		eq(ns.db.seen.u2, nil)
		eq(agentRows(c)[1].state, "unseen")
	end)

	test("seen: a new finish makes an agent unseen again, and the mark survives /reload", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("u1", "idle", "fresh", { section = "S", doneAt = START - 20 }) })
		click(agentRows(c)[1].frame) -- opens the page: seen
		eq(ns.db.seen.u1, START - 20)
		c:reload()
		local ns2 = c:ns()
		put(ns2, { A("u1", "idle", "fresh", { section = "S", doneAt = START - 20 }) })
		eq(#agentRows(c), 0, "still seen after /reload")
		put(ns2, { A("u1", "idle", "fresh", { section = "S", doneAt = START + 40 }) })
		eq(agentRows(c)[1].state, "unseen", "a later turn finished")
		eq(ns2.Store:Counts().unseen, 1)
	end)

	test("seen: doneAt 0 (not observed) is never unseen; idle for other reasons stays idle", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("a", "idle", "never-seen-finishing", { section = "S", doneAt = 0 }) })
		eq(#agentRows(c), 0)
		eq(ns.Store:Kind(ns.Store.agents[1]), "idle")
	end)

	test("store: kinds, counts, and old seen entries are pruned", function()
		local _, c, ns = setup({ install = false })
		for i = 1, 250 do
			ns.db.seen["dead" .. i] = i
		end
		ns.db.seen.keep = START
		put(ns, { A("keep", "idle", "kept", { doneAt = START }), A("n", "waiting_input", "n"), A("w", "working", "w"), A("x", "exited", "x"), A("u", "idle", "u", { doneAt = START + 1 }) })
		eq(ns.db.seen.dead1, nil, "dead agents forgotten once the table grows")
		eq(ns.db.seen.keep, START)
		local counts = ns.Store:Counts()
		eq(counts.needs, 1)
		eq(counts.working, 1)
		eq(counts.unseen, 1)
		eq(counts.total, 5)
		eq(ns.Store:Kind(ns.Store:Find("x")), "exited")
		eq(ns.Store:Kind({ state = "mystery", doneAt = 0, id = "?" }), "other")
	end)

	test("store: rich fields are cleaned and bounded", function()
		local _, c, ns = setup({ install = false })
		local lines = {}
		for i = 1, 500 do
			lines[i] = { k = "p", t = "line " .. i }
		end
		lines[#lines + 1] = { k = "bogus", t = "dropped" }
		put(ns, {
			A("a", "working", "rich", {
				section = "Home|lab", activity = "Run|ning", todo = "3/7", stats = "1m", doneAt = START, preview = "pre|view",
				detail = { lines = lines, steps = { { n = "bash", t = "x|y", s = "done" }, "junk", { n = 5, t = 6, s = 7 } } },
			}),
			A("b", "working", "plain", { detail = { lines = {}, steps = {} } }),
			A("c", "working", "bad", { doneAt = "soon", detail = "nope" }),
		})
		local a = ns.Store:Find("a")
		eq(a.section, "Home||lab")
		eq(a.activity, "Run||ning")
		eq(a.preview, "pre||view")
		eq(#a.detail.lines, 400, "lines are capped")
		eq(a.detail.steps[1].t, "x||y")
		eq(a.detail.steps[2].n, "5")
		eq(a.detail.steps[2].s, "")
		eq(ns.Store:Find("b").detail, nil, "an empty detail is no detail")
		eq(ns.Store:Find("c").doneAt, 0)
		eq(ns.Store:Find("c").detail, nil)
		eq(ns.errorCount, 0)
	end)

	test("saved variables: the v1 panel's switches are migrated away", function()
		local _, c, ns = setup({ install = false })
		local db = ns.MigrateDB({ version = 1, detail = true, showIdle = true, scale = 0.9, locked = false, lastToast = 12, collapsed = true })
		eq(db.detail, nil)
		eq(db.showIdle, nil)
		eq(db.scale, 1)
		eq(db.locked, true)
		eq(db.lastToast, 12)
		eq(db.collapsed, true)
		eq(db.version, 4)
		truthy(type(db.seen) == "table" and type(db.collapsedSections) == "table" and type(db.idleOpen) == "boolean")
		local again = ns.MigrateDB({ version = 2, scale = 0.7, locked = false, seen = { a = 1 }, idleOpen = "bad",
			pos = { point = "TOPLEFT", relPoint = "TOPLEFT", x = 24, y = -240 } })
		eq(again.scale, 0.7, "a v2 db keeps its choices")
		eq(again.locked, false)
		eq(again.seen.a, 1)
		eq(again.idleOpen, false)
		eq(again.pos, nil, "v3 drops the position inherited from the boxed panel")
		local v3 = ns.MigrateDB({ version = 3, pos = { point = "TOP", relPoint = "TOP", x = 1, y = -2 } })
		eq(v3.pos.point, "TOP", "a position dragged on v3 is kept")
	end)

	----------------------------------------------------------------------
	-- the detail page
	----------------------------------------------------------------------

	local DETAIL = {
		lines = {
			{ k = "h", t = "Summary" },
			{ k = "p", t = "Used \1code\2 and \3bold\4 and a|b" },
			{ k = "gap" },
			{ k = "li", t = "top item", d = 1 },
			{ k = "li", t = "nested item", d = 2 },
			{ k = "code", t = "local x = 1" },
			{ k = "code", t = "return x" },
			{ k = "quote", t = "wise words" },
			{ k = "hr" },
			{ k = "p", t = "| Input | $2 | $4 |" },
			{ k = "p", t = "open \1code never closed" },
			{ k = "more", t = "\226\128\166 open in Tern for the full reply" },
		},
		steps = {
			{ n = "bash", t = "Running tests", s = "running" },
			{ n = "read", t = "the file", s = "done" },
			{ n = "edit", t = "oops", s = "error" },
		},
	}

	local function openDetail(c, ns, agents, id)
		put(ns, agents)
		ns.Detail:Open(id)
		return c.env.OmpcraftDetail
	end

	test("detail: a ButtonFrame window with parchment, scrolling, closing with Esc", function()
		local _, c, ns = setup({ install = false })
		local f = openDetail(c, ns, { A("a", "waiting_input", "demo-agent", { section = "S", since = START - 90, detail = DETAIL }) }, "a")
		eq(f.__s.template, "ButtonFrameTemplate")
		eq(f.__s.shown, true)
		eq(f.TitleContainer.TitleText:GetText(), "demo-agent")
		eq(c.templateCalls[1], "HidePortrait")
		eq(c.templateCalls[2], "HideAttic", "no empty band between the title bar and the page")
		eq(c.templateCalls[3], "HideButtonBar")
		local found
		for _, n in ipairs(c.env.UISpecialFrames) do
			found = found or n == "OmpcraftDetail"
		end
		truthy(found, "in UISpecialFrames, so Esc closes it")
		eq(ns.Detail.paper.bg.__s.atlas, "QuestBG-Parchment")
		eq(ns.Detail.scroll.__s.template, "ScrollFrameTemplate")
		eq(ns.Detail.scroll.__s.scrollChild, ns.Detail.content)
		eq(f.__s.strata, "HIGH")
		eq(ns.Detail.stateText:GetText(), "Needs you \194\183 1m")
		eq(ns.Detail.stateText.__s.font, "QuestFont", "the plain quest font, not the blackletter heading font")
		truthy(ns.Detail.stateText:GetParent() == ns.Detail.paper and ns.Detail.stateText:GetParent() ~= ns.Detail.content, "outside the scroll content")
		local scrollTop = ns.Detail.scroll.__s.points[1]
		truthy(scrollTop.y <= -36, "the scroll area starts below the header band: " .. tostring(scrollTop.y))
		eq(ns.Detail.scroll.__s.points[1].rel, ns.Detail.paper)
		eq(f.__s.width, 420)
		eq(f.__s.height, 560)
		eq(ns.Detail.marker.__s.texture, "Interface/GossipFrame/AvailableQuestIcon")
		eq(f:GetParent(), c.env.UIParent)
		eq(ns.errorCount, 0)
	end)

	test("detail: renders headings, paragraphs, lists, code, quotes, rules, gaps and the more hint", function()
		local _, c, ns = setup({ install = false })
		openDetail(c, ns, { A("a", "working", "demo-agent", { section = "S", host = "host-a", detail = DETAIL }) }, "a")
		local T = c:detailTexts()
		local function find(text)
			for _, t in ipairs(T) do
				if t.text:find(text, 1, true) then
					return t
				end
			end
		end
		local heading = find("Summary")
		eq(heading.font, "QuestFontNormalLarge")
		eq(heading.color[1], 0.35, "heading in the parchment title colour")

		-- inline sentinels become colour codes the addon owns, pipes stay doubled
		local p = find("Used ")
		eq(p.text, "Used |cff0b4f7acode|r and |cff5a1608bold|r and a||b")
		eq(p.font, "QuestFont")
		eq(p.color[1], 0.18)
		lacks(p.text, "\1")
		lacks(p.text, "\3")
		truthy(pipesBalanced(p.text:gsub("|c%x%x%x%x%x%x%x%x", ""):gsub("|r", "")))

		-- list items: bullet and text, indented by depth
		local top, nested = find("top item"), find("nested item")
		eq(top.x, 20)
		eq(nested.x, 34)
		local bullets = {}
		for _, t in ipairs(T) do
			if t.text == "\226\128\162" or t.text == "\226\128\147" then
				bullets[#bullets + 1] = t
			end
		end
		eq(#bullets, 2)
		eq(bullets[1].text, "\226\128\162")
		eq(bullets[1].x, 6)
		eq(bullets[2].text, "\226\128\147", "nested items use a dash")
		eq(bullets[2].x, 20)

		-- code: one non-wrapping line per FontString on one dark band
		local code = find("local x = 1")
		eq(code.text, "local x = 1")
		eq(find("return x").text, "return x")
		eq(code.font, "GameFontHighlightSmall")
		eq(code.color[1], 0.82)
		local bands = 0
		for _, tex in ipairs(c:detailTextures()) do
			if tex.color and tex.color[4] == 0.55 and tex.shown then
				bands = bands + 1
			end
		end
		eq(bands, 1)
		-- a quote is dim, with a bar
		local quote = find("wise words")
		eq(quote.x, 12)
		truthy(quote.color[1] > 0.4 and quote.color[1] < 0.5, "dim")
		-- table rows come through as paragraphs, pipes as sent
		eq(find("Input").text, "|| Input || $2 || $4 ||")
		-- an unclosed highlight is closed
		eq(find("never closed").text, "open |cff0b4f7acode never closed|r")
		-- the hint is dim
		local more = find("open in Tern")
		eq(more.font, "GameFontDisableSmall")
		-- and everything flows top to bottom
		local lastY = -1
		for _, t in ipairs(T) do
			if t.x ~= 6 and t.x ~= 20 then -- bullets sit level with their text
				truthy(-t.y >= lastY, "layout runs downward at " .. t.text)
				lastY = -t.y
			end
		end
		eq(ns.errorCount, 0)
	end)

	test("detail: table rows are single indented lines on faint alternating bands; unknown kinds read as paragraphs", function()
		local _, c, ns = setup({ install = false })
		local long = string.rep("cell  \194\183  ", 40)
		local lines = {
			{ k = "p", t = "before the table" },
			{ k = "tr", t = "\3Item\4  \194\183  \3Price\4" },
			{ k = "tr", t = "apple  \194\183  $2" },
			{ k = "tr", t = "pear  \194\183  a|b" },
			{ k = "tr", t = long },
			{ k = "p", t = "between" },
			{ k = "tr", t = "lonely  \194\183  row" },
			{ k = "callout", t = "a kind from the future" },
			{ k = "tr", t = "row  \194\183  after" },
		}
		openDetail(c, ns, { A("a", "working", "demo-agent", { section = "S", detail = { lines = lines, steps = {} } }) }, "a")
		local kindsOf = {}
		for _, l in ipairs(ns.Store:Find("a").detail.lines) do
			kindsOf[#kindsOf + 1] = l.k
		end
		eq(table.concat(kindsOf, ","), "p,tr,tr,tr,tr,p,tr,p,tr", "tr is kept, the unknown kind became p")

		local pool = ns.Detail.pool
		local function fsWith(text)
			for i = 1, pool.fsUsed do
				if pool.fs[i]:GetText():find(text, 1, true) then
					return pool.fs[i], i
				end
			end
		end
		local header = fsWith("Item")
		eq(header:GetText(), "|cff5a1608Item|r  \194\183  |cff5a1608Price|r", "header cells are bold")
		eq(header.__s.font, "QuestFont")
		eq(header.__s.wordWrap, false, "single line")
		eq(header.__s.points[1].x, 8, "indented 8 px")
		eq(header.__s.textColor[1], 0.18)
		local apple, pear = fsWith("apple"), fsWith("pear")
		eq(-apple.__s.points[1].y - -header.__s.points[1].y, 17, "row height plus 2 px of spacing")
		eq(-pear.__s.points[1].y - -apple.__s.points[1].y, 17)
		eq(pear:GetText(), "pear  \194\183  a||b")

		-- a long row is cut like code
		local cut
		for i = 1, pool.fsUsed do
			if pool.fs[i]:GetText():find("cell", 1, true) and pool.fs[i].__s.wordWrap == false and #pool.fs[i]:GetText() > 100 then
				cut = pool.fs[i]:GetText()
			end
		end
		truthy(cut and cut:sub(-3) == "\226\128\166", "ends in an ellipsis")
		local chars = select(2, cut:gsub("[^\128-\191]", ""))
		eq(chars, 108, "107 characters and the ellipsis")

		-- bands: every second row of a run (2nd and 4th of the first table), none for a lone row
		local bands = {}
		for _, tex in ipairs(c:detailTextures()) do
			if tex.color and tex.color[4] == 0.1 and tex.shown then
				bands[#bands + 1] = tex
			end
		end
		eq(#bands, 2)
		eq(bands[1].height, 17)
		eq(bands[1].points[1].y, -(-apple.__s.points[1].y - 1), "behind the second row")
		eq(bands[1].width, 362)
		truthy(bands[1].color[4] < 0.15, "very faint")

		-- the paragraphs around the tables flow normally, and the unknown kind shows its text
		local future = fsWith("from the future")
		eq(future.__s.font, "QuestFont")
		eq(future.__s.wordWrap, true, "wrapping is back for ordinary lines")
		eq(future.__s.points[1].x, 0)
		-- the row after it starts a new run: no band
		eq(fsWith("row  \194\183  after").__s.points[1].x, 8)
		eq(ns.errorCount, 0)
	end)

	test("detail: a huge code block is capped at 12 lines, long lines are cut, and it stays one short band", function()
		local _, c, ns = setup({ install = false })
		local lines = { { k = "p", t = "before" } }
		for i = 1, 40 do
			lines[#lines + 1] = { k = "code", t = string.format("<path id=\"%02d\" d=\"%s\"/>", i, string.rep("M0 0L9 9 ", 25)) }
		end
		lines[#lines + 1] = { k = "p", t = "after" }
		lines[#lines + 1] = { k = "code", t = "short one" }
		lines[#lines + 1] = { k = "code", t = "short two" }
		openDetail(c, ns, { A("a", "working", "demo-agent", { section = "S", detail = { lines = lines, steps = {} } }) }, "a")
		-- every code line is its own non-wrapping FontString: a real non-wrapping
		-- FontString shows ONE line however many "\n" its text holds
		local shown, ys = {}, {}
		for _, t in ipairs(c:detailTexts()) do
			if t.font == "GameFontHighlightSmall" and (t.text:find("<path id=", 1, true) or t.text:find("more lines", 1, true)) then
				shown[#shown + 1] = t.text
				ys[#ys + 1] = t.y
				lacks(t.text, "\n", "one line per FontString")
			end
		end
		eq(#shown, 13, "12 code lines and one note")
		for i = 1, 12 do
			truthy(#shown[i] <= 110 + 2, "line " .. i .. " is cut: " .. #shown[i])
			eq(shown[i]:sub(-3), "\226\128\166", "a cut line ends in an ellipsis")
			truthy(shown[i]:find(string.format("<path id=\"%02d\"", i), 1, true), "the first 12 lines are the ones shown")
		end
		eq(shown[13], "|cff8f9aa3\226\128\166 28 more lines (open in Tern)|r")
		eq(plain(shown[13]), "\226\128\166 28 more lines (open in Tern)")
		for i = 2, 13 do
			eq(ys[i] - ys[i - 1], -12, "stacked one row (12 px) below the one before: " .. i)
		end

		-- one band of 13 short lines, not 40 long ones
		local bands = {}
		for _, tex in ipairs(c:detailTextures()) do
			if tex.color and tex.color[4] == 0.55 and tex.shown then
				bands[#bands + 1] = tex
			end
		end
		eq(#bands, 2, "the big block and the short run are separate bands")
		eq(bands[1].height, 13 * 12 + 8, "a band of 13 lines")
		eq(bands[2].height, 2 * 12 + 8)

		-- a short run is untouched, with no note: one FontString per line
		local one, two
		for _, t in ipairs(c:detailTexts()) do
			if t.text == "short one" then
				one = t
			elseif t.text == "short two" then
				two = t
			end
		end
		truthy(one and two, "two separate short lines")
		eq(two.y - one.y, -12)

		-- exactly 12 lines is not capped; pipes in cut lines stay paired
		local twelve = {}
		for i = 1, 12 do
			twelve[i] = { k = "code", t = string.rep("a|", 70) }
		end
		openDetail(c, ns, { A("b", "working", "other", { section = "S", detail = { lines = twelve, steps = {} } }) }, "b")
		local seen12 = 0
		for _, t in ipairs(c:detailTexts()) do
			if t.font == "GameFontHighlightSmall" then
				seen12 = seen12 + 1
				lacks(t.text, "more lines")
				truthy(pipesBalanced(t.text), "unbalanced pipes in a cut code line")
			end
		end
		eq(seen12, 12)
		eq(ns.errorCount, 0)
	end)

	test("detail: tab-indented code keeps its indentation (the game font has no tab glyph)", function()
		local _, c, ns = setup({ install = false })
		local lines = { { k = "code", t = "func f() {" }, { k = "code", t = "\tif ok {" }, { k = "code", t = "\t\treturn" } }
		openDetail(c, ns, { A("a", "working", "demo-agent", { section = "S", detail = { lines = lines, steps = {} } }) }, "a")
		local shown = {}
		for _, t in ipairs(c:detailTexts()) do
			if t.font == "GameFontHighlightSmall" and (t.text:find("if ok", 1, true) or t.text:find("return", 1, true)) then
				shown[#shown + 1] = t.text
			end
		end
		eq(#shown, 2)
		eq(shown[1], "    if ok {")
		eq(shown[2], "        return")
		eq(ns.errorCount, 0)
	end)

	test("stub: a non-wrapping FontString is one line high, whatever \"\\n\" its text has", function()
		local _, c = setup({ install = false })
		local fs = c.env.UIParent:CreateFontString(nil, "ARTWORK", "GameFontHighlightSmall")
		fs:SetWordWrap(false)
		fs:SetWidth(300)
		fs:SetText("one")
		local one = fs:GetStringHeight()
		fs:SetText("one\ntwo\nthree")
		eq(fs:GetStringHeight(), one, "the real client draws a single line")
		fs:SetWordWrap(true)
		truthy(fs:GetStringHeight() > one, "a wrapping one does grow")
	end)

	test("detail: the footer never repeats itself, e.g. a session named after its directory", function()
		local _, c, ns = setup({ install = false })
		openDetail(c, ns, { A("a", "idle", "demo-agent", { section = "S", host = "docs-site", session = "docs-site", cwd = "docs-site", model = "Fable 5.2", detail = DETAIL, doneAt = START - 5 }) }, "a")
		local footer
		for _, t in ipairs(c:detailTexts()) do
			if t.text:find("Fable 5.2", 1, true) then
				footer = t.text
			end
		end
		eq(footer, "Fable 5.2 \194\183 docs-site")
		openDetail(c, ns, { A("b", "idle", "demo-agent", { section = "S", host = "host-a", session = "demo", cwd = "project", model = "Fable 5.2", detail = DETAIL, doneAt = START - 5 }) }, "b")
		for _, t in ipairs(c:detailTexts()) do
			if t.text:find("Fable 5.2", 1, true) then
				footer = t.text
			end
		end
		eq(footer, "Fable 5.2 \194\183 host-a \194\183 demo \194\183 project")
		eq(ns.errorCount, 0)
	end)

	test("detail: a user resize is remembered, bounded, and the text reflows to the new width", function()
		local _, c, ns = setup({ install = false })
		local f = openDetail(c, ns, { A("a", "working", "demo-agent", { section = "S", detail = DETAIL }) }, "a")
		truthy(f.__s.resizable, "SetResizable")
		truthy(f.__s.resizeBounds, "SetResizeBounds")
		local grip = ns.Detail.grip
		grip.__s.scripts.OnMouseDown(grip)
		eq(f.__s.sizing, "BOTTOMRIGHT")
		f.__s.width, f.__s.height = 500, 640
		grip.__s.scripts.OnMouseUp(grip)
		eq(f.__s.sizing, nil)
		eq(ns.db.detailSize.w, 500)
		eq(ns.db.detailSize.h, 640)
		eq(ns.Detail.content.__s.width, 500 - 58, "the content follows the window")
		-- out of bounds is clamped
		f.__s.width, f.__s.height = 5000, 10
		grip.__s.scripts.OnMouseUp(grip)
		eq(ns.db.detailSize.w, 760)
		eq(ns.db.detailSize.h, 360)
		-- and comes back after a reload
		c:reload()
		local ns2 = c:ns()
		ns2.Detail:Open("a")
		eq(c.env.OmpcraftDetail.__s.width, 760)
		eq(c.env.OmpcraftDetail.__s.height, 360)
		-- junk in the save is dropped
		local _, _, ns3 = setup({ install = false, saved = { version = 4, detailSize = { w = "wide", h = 1 } } })
		eq(ns3.db.detailSize, nil)
		eq(ns.errorCount, 0)
	end)

	test("detail: steps show done, running and failed marks; the footer lists the details", function()
		local _, c, ns = setup({ install = false })
		openDetail(c, ns, { A("a", "idle", "demo-agent", { section = "S", host = "host-a", stats = "1m 12s \194\183 22K tok", detail = DETAIL, doneAt = START - 5 }) }, "a")
		local atlases = {}
		for _, tex in ipairs(c:detailTextures()) do
			if tex.atlas and tex.shown then
				atlases[#atlases + 1] = tex.atlas
			end
		end
		eq(table.concat(atlases, ","), "ui-questtracker-objective-nub,ui-questtracker-tracker-check,ui-questtracker-objective-fail")
		local texts = {}
		for _, t in ipairs(c:detailTexts()) do
			texts[#texts + 1] = t.text
		end
		local all = table.concat(texts, "\n")
		contains(all, "Recent steps")
		contains(all, "bash \194\183 Running tests")
		contains(all, "read \194\183 the file")
		contains(all, "model-x \194\183 host-a \194\183 demo-session \194\183 project")
		contains(all, "1m 12s \194\183 22K tok")
		eq(ns.Detail.stateText:GetText(), "Finished \194\183 5s ago")
	end)

	test("detail: no reply yet shows the preview and a dim note", function()
		local _, c, ns = setup({ install = false })
		openDetail(c, ns, { A("a", "idle", "quiet", { section = "S", preview = "All tests pass", last = "older" }) }, "a")
		local T = c:detailTexts()
		eq(T[1].text, "All tests pass")
		eq(T[1].font, "QuestFont")
		eq(T[2].text, "Full reply appears while this agent is active or recently finished.")
		eq(T[2].font, "GameFontDisableSmall")
		openDetail(c, ns, { A("b", "idle", "bare", { section = "S", last = "only last" }) }, "b")
		eq(c:detailTexts()[1].text, "only last")
	end)

	test("detail: opening marks the turn seen; staying open keeps new turns seen", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("a", "idle", "fresh", { section = "S", doneAt = START - 20 }) })
		eq(agentRows(c)[1].state, "unseen")
		click(agentRows(c)[1].frame)
		eq(ns.db.seen.a, START - 20)
		eq(ns.Detail.agentId, "a")
		eq(#agentRows(c), 0, "the tracker updated at once")
		-- the agent works again and finishes while the page is open
		put(ns, { A("a", "working", "fresh", { section = "S", doneAt = START - 20 }) })
		eq(ns.Detail.stateText:GetText(), "Working \194\183 10s")
		put(ns, { A("a", "idle", "fresh", { section = "S", doneAt = START + 30 }) })
		eq(ns.db.seen.a, START + 30, "the open page counts as looking at it")
		eq(#agentRows(c), 0)
		-- closed, it would have been unseen
		c.env.OmpcraftDetail:Hide()
		put(ns, { A("a", "idle", "fresh", { section = "S", doneAt = START + 90 }) })
		eq(agentRows(c)[1].state, "unseen")
	end)

	test("detail: follows snapshots, keeps its scroll, and copes with a vanished agent", function()
		local _, c, ns = setup({ install = false })
		local long = {}
		for i = 1, 80 do
			long[i] = { k = "p", t = "paragraph number " .. i .. " " .. string.rep("words ", 20) }
		end
		openDetail(c, ns, { A("a", "working", "demo-agent", { section = "S", detail = { lines = long, steps = {} } }) }, "a")
		local scroll = ns.Detail.scroll
		truthy(scroll:GetVerticalScrollRange() > 0, "long replies scroll")
		scroll:SetVerticalScroll(120)
		put(ns, { A("a", "working", "demo-agent", { section = "S", detail = { lines = long, steps = {} } }) })
		eq(scroll:GetVerticalScroll(), 120, "a new snapshot keeps the reading position")
		ns.Detail:Open("a")
		eq(scroll:GetVerticalScroll(), 0, "opening starts at the top")
		put(ns, { A("b", "working", "other", { section = "S" }) })
		eq(c:detailTexts()[1].text, "This agent is no longer listed.")
		eq(ns.errorCount, 0)
	end)

	test("detail: pipes in text and names never break the page", function()
		local _, c, ns = setup({ install = false })
		openDetail(c, ns, { A("a", "working", "build|test", { section = "S", host = "ho|st", detail = { lines = { { k = "h", t = "|cffff0000Red|r" }, { k = "code", t = "a || b | c" } }, steps = {} } }) }, "a")
		eq(c.env.OmpcraftDetail.TitleContainer.TitleText:GetText(), "build||test")
		for _, t in ipairs(c:detailTexts()) do
			truthy(pipesBalanced(t.text), "odd pipes in " .. t.text)
		end
		eq(c:detailTexts()[1].text, "||cffff0000Red||r")
	end)

	test("detail: only verified atlases and textures are used", function()
		local _, c, ns = setup({ install = false })
		openDetail(c, ns, { A("a", "working", "x", { detail = DETAIL }) }, "a")
		eq(ns.errorCount, 0) -- the stub raises on an unverified atlas or texture file
		for _, name in pairs(ns.Style.ATLAS) do
			truthy(Stub.ATLASES[name:lower()], "atlas not in the verified list: " .. name)
		end
	end)

	----------------------------------------------------------------------
	-- toasts
	----------------------------------------------------------------------

	local function toast(ns, id, kind, agent, text)
		ns:Fire("TOAST", { id = id, kind = kind, agent = agent, text = text or "toast" })
	end

	test("toasts: finished agents get the achievement alert, with the turn stats", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("u1", "idle", "fresh-agent", { section = "S", doneAt = START - 5, stats = "1m 12s \194\183 22K tok" }) })
		toast(ns, 1, "done", "u1", "fresh-agent finished")
		local f = c:toastFrame(1)
		eq(f.__s.shown, true)
		eq(f.__s.width, 300)
		eq(f.__s.height, 101)
		eq(f.bg.__s.atlas, "ui-achievement-alert-background")
		eq(f.glow.__s.atlas, "ui-achievement-alert-glow-glow")
		eq(f.shine.__s.atlas, "ui-achievement-alert-glow-shine")
		eq(f.icon.frame.__s.atlas, "ui-achievement-iconframe")
		eq(f.label:GetText(), "Finished")
		eq(f.label.__s.font, "GameFontBlackTiny")
		eq(f.name:GetText(), "fresh-agent")
		eq(f.name.__s.font, "GameFontHighlight")
		eq(f.stats:GetText(), "1m 12s \194\183 22K tok")
		eq(f.stats.__s.shown, true)
		eq(f.icon.marker.__s.texture, "Interface/GossipFrame/ActiveQuestIcon")
		eq(f.bg.__s.desaturated, false)
		eq(c.soundsPlayed[1], c.env.SOUNDKIT.IG_QUEST_LIST_COMPLETE, "a quest-complete-like sound")
		contains(c:chatText(), "fresh-agent finished")
		-- the stats appear on finished toasts only
		toast(ns, 2, "input", "u1", "fresh-agent needs you")
		eq(c:toastFrame(2).stats.__s.shown, false)
	end)

	test("toasts: a long agent name uses the whole text area and may take two lines", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("u1", "idle", "Migrate CI to the new runners", { section = "S", doneAt = START - 5, stats = "1m 12s \194\183 22K tok" }),
			A("a", "waiting_input", "Migrate CI to the new runners", { section = "S" }) })
		toast(ns, 1, "done", "u1", "Migrate CI to the new runners finished")
		local f = c:toastFrame(1)
		eq(f.name:GetText(), "Migrate CI to the new runners", "not cut at 24 characters")
		eq(f.name.__s.maxLines, 2)
		eq(f.name.__s.wordWrap, true)
		eq(f.name.__s.width, 186, "wider than Blizzard's 155, inside the 200 px text area")
		truthy(f.name.__s.width <= f.label.__s.width, "never wider than the label above it")
		eq(f.stats.__s.points[1].rel, f.name, "the stats line stays below the name")
		eq(f.stats.__s.points[1].point, "TOP")
		eq(f.stats.__s.points[1].relPoint, "BOTTOM")
		toast(ns, 2, "input", "a", "Migrate CI to the new runners needs you")
		local mini = c:toastFrame(2)
		eq(mini.name:GetText(), "Migrate CI to the new runners")
		eq(mini.name.__s.maxLines, 2)
		eq(mini.name.__s.width, 172)
		eq(ns.errorCount, 0)
	end)

	test("toasts: needs-you uses the smaller alert; failed and exited are muted", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("a", "waiting_input", "asker", { section = "S" }), A("x", "exited", "gone", { section = "S" }) })
		toast(ns, 1, "input", "a", "asker needs you")
		local f = c:toastFrame(1)
		eq(f.__s.width, 256)
		eq(f.__s.height, 84)
		eq(f.bg.__s.atlas, "ui-achievement-alert-background-mini")
		eq(f.glow.__s.atlas, "ui-achievement-alert-background-mini-glow")
		eq(f.shine.__s.atlas, "ui-achievement-alert-background-mini-glow-shine")
		eq(f.label:GetText(), "Needs you")
		eq(f.icon.marker.__s.texture, "Interface/GossipFrame/AvailableQuestIcon")
		eq(f.icon.marker.__s.desaturated, false)
		eq(c.soundsPlayed[1], c.env.SOUNDKIT.READY_CHECK)

		c:advance(1.2) -- past the sound cap
		toast(ns, 2, "exited", "x", "gone exited")
		local g = c:toastFrame(2)
		eq(g.label:GetText(), "Exited")
		eq(g.bg.__s.desaturated, true)
		eq(g.icon.marker.__s.desaturated, true)
		truthy(g.icon.marker.__s.vertex[2] < 0.3, "red !")
		eq(c.soundsPlayed[2], c.env.SOUNDKIT.UI_BNET_TOAST, "a muted sound")
		c:advance(1.2)
		toast(ns, 3, "failed", "x", "gone failed")
		eq(c:toastFrame(3).label:GetText(), "Failed")
		eq(c:toastFrame(3).bg.__s.desaturated, true)
		-- an unknown agent falls back to the toast's own text
		toast(ns, 4, "input", "nobody", "somebody needs you")
		eq(c:toastFrame(1).name:GetText(), "asker")
		eq(ns.errorCount, 0)
	end)

	test("toasts: slide and fade in, flash and shine, then settle", function()
		local _, c, ns = setup({ install = false })
		toast(ns, 1, "done", "nobody", "x finished")
		local f = c:toastFrame(1)
		eq(f.slideIn:IsPlaying(), true)
		eq(f.glowAnim:IsPlaying(), true)
		eq(f.shineAnim:IsPlaying(), true)
		eq(select(4, f:GetPoint(1)), 30, "starts off to the right")
		eq(f.slideIn.__s.anims[2].__s.offset[1], -30)
		eq(f.shineSweep.__s.offset[1], 240, "the shine sweeps across the full-size alert")
		c:advance(0.3)
		eq(f.slideIn:IsPlaying(), false)
		eq(select(4, f:GetPoint(1)), 0, "and rests at its place")
		eq(select(5, f:GetPoint(1)), -110)
		c:advance(1.2)
		eq(f.glow.__s.shown, false)
		eq(f.shine.__s.shown, false)
		toast(ns, 2, "input", "nobody", "y needs you")
		c:advance(1.2)
		eq(c:toastFrame(2).shineSweep.__s.offset[1], 210, "and the smaller one is shorter")
		eq(select(5, c:toastFrame(2):GetPoint(1)), -216, "stacked below")
		eq(select(5, c:toastFrame(1):GetPoint(1)), -110)
	end)

	test("toasts: an update notice is neutral, soft, and only dismisses; unknown kinds look the same", function()
		local _, c, ns = setup({ install = false })
		local text = "ompcraft updated to 0.2.0. Type /reload to load it."
		-- it arrives in a snapshot like any toast, even naming an agent that exists
		put(ns, { A("u1", "idle", "fresh", { section = "S", doneAt = START - 5 }) }, {
			toasts = { { id = (START - 1) * 1000, kind = "update", agent = "u1", text = text } },
		})
		local f = c:toastFrame(1)
		eq(f.__s.shown, true)
		eq(f.__s.width, 256, "the smaller alert, not the achievement")
		eq(f.__s.height, 84)
		eq(f.bg.__s.atlas, "ui-achievement-alert-background-mini")
		eq(f.bg.__s.desaturated, false, "not the muted error look")
		eq(f.label:GetText(), "Ompcraft update")
		eq(f.name:GetText(), text, "the whole message, on up to two lines")
		eq(f.name.__s.wordWrap, true)
		eq(f.name.__s.maxLines, 2)
		eq(f.stats.__s.shown, false)
		eq(f.icon.marker.__s.atlas, "SideInProgressquesticon", "neutral marker")
		eq(f.icon.marker.__s.desaturated, false)
		eq(c.soundsPlayed[1], c.env.SOUNDKIT.UI_BNET_TOAST, "a soft sound")
		contains(c:chatText(), text)
		eq(f.agentId, nil, "no agent behind it")
		f.__s.scripts.OnMouseUp(f, "LeftButton")
		eq(f.__s.shown, false, "a click dismisses it")
		eq(rawget(c.env, "OmpcraftDetail"), nil, "and opens nothing")

		-- a kind from the future is shown the same way instead of being dropped
		c:advance(1.2)
		put(ns, { A("u1", "idle", "fresh", { section = "S", doneAt = START - 5 }) }, {
			toasts = { { id = (START - 1) * 1000 + 5, kind = "from-the-future", agent = "u1", text = "something new happened" } },
		})
		local g = c:toastFrame(1)
		eq(g.__s.shown, true)
		eq(g.label:GetText(), "Ompcraft")
		eq(g.name:GetText(), "something new happened", "its own text, not the agent's name")
		eq(g.bg.__s.atlas, "ui-achievement-alert-background-mini")
		eq(g.agentId, nil)
		eq(c.soundsPlayed[2], c.env.SOUNDKIT.UI_BNET_TOAST)
		g.__s.scripts.OnMouseUp(g, "LeftButton")
		eq(rawget(c.env, "OmpcraftDetail"), nil)
		eq(ns.errorCount, 0)
	end)

	test("toasts: clicking opens the agent's detail page", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("u1", "idle", "fresh", { section = "S", doneAt = START - 5 }) })
		toast(ns, 1, "done", "u1", "fresh finished")
		local f = c:toastFrame(1)
		f.__s.scripts.OnMouseUp(f, "LeftButton")
		eq(f.__s.shown, false, "the toast is dismissed")
		eq(c.env.OmpcraftDetail.__s.shown, true)
		eq(ns.Detail.agentId, "u1")
		eq(ns.db.seen.u1, START - 5, "and the turn counts as seen")

		-- a toast for an agent that is gone just goes away
		local _, c2, ns2 = setup({ install = false })
		toast(ns2, 1, "done", "gone", "x finished")
		local g = c2:toastFrame(1)
		g.__s.scripts.OnMouseUp(g, "LeftButton")
		eq(g.__s.shown, false)
		eq(rawget(c2.env, "OmpcraftDetail"), nil)
		eq(ns2.errorCount, 0)
	end)

	test("toasts: all the look's names are verified ones", function()
		local _, c, ns = setup({ install = false })
		for _, name in pairs(ns.Style.ATLAS) do
			truthy(Stub.ATLASES[name:lower()], "atlas not verified: " .. name)
		end
		for _, name in pairs(ns.Style.font) do
			truthy(rawget(c.env, name) ~= nil, "font object missing: " .. name)
		end
		for kind, style in pairs(ns.Toasts.STYLES) do
			truthy(c.env.SOUNDKIT[style.sound], "sound kit for " .. kind)
		end
		for _, tex in pairs(ns.Style.TEXTURE) do
			local ok = pcall(function()
				local t = c.env.UIParent:CreateTexture()
				t:SetTexture(tex)
			end)
			truthy(ok, "texture file not verified: " .. tex)
		end
	end)
end
