-- Tests for the settings page and everything it controls. Loaded by run.lua.

return function(H)
	local test, eq, truthy, contains, lacks, near = H.test, H.eq, H.truthy, H.contains, H.lacks, H.near
	local setup, START = H.setup, H.START
	local trackerKinds, agentRows = H.trackerKinds, H.agentRows
	local Stub = require("wowstub")

	local gen = 1000
	local function A(id, state, name, extra)
		local a = { id = id, name = name, state = state, since = START - 10, model = "model-x", cwd = "project", last = "", host = "", session = "S", section = "S" }
		for k, v in pairs(extra or {}) do
			a[k] = v
		end
		return a
	end

	local function put(ns, agents, extra)
		gen = gen + 1
		local snap = { v = 0, run = 7, gen = gen, at = START, session = START, nextSignal = 1, tern = "ok", agents = agents }
		for k, v in pairs(extra or {}) do
			snap[k] = v
		end
		return ns.Store:Apply(snap)
	end

	local function var(ns, key)
		return ns.Config.vars[key]
	end

	-- change a setting the way its control would
	local function set(c, ns, key, value)
		c:setSetting(var(ns, key), value)
	end

	local function get(c, ns, key)
		return c:getSetting(var(ns, key))
	end

	local function names(rows)
		local out = {}
		for i, r in ipairs(rows) do
			out[i] = r.name
		end
		return table.concat(out, ",")
	end

	local function kinds(c)
		local out = {}
		for _, e in ipairs(c:tracker()) do
			if e.kind ~= "message" then
				out[#out + 1] = e.kind
			end
		end
		return table.concat(out, ",")
	end

	local function initializers(c, template)
		local out = {}
		for _, init in ipairs(c:settingsCategory().initializers) do
			if init.template == template then
				out[#out + 1] = init
			end
		end
		return out
	end

	local function button(c, name)
		for _, init in ipairs(initializers(c, "SettingButtonControlTemplate")) do
			if init.name == name then
				return init
			end
		end
	end

	local function shown(c)
		return c.env.OmpcraftTracker.__s.shown
	end

	-- a spy on a setting's value-changed callbacks
	local function spy(c, ns, key)
		local seen = { count = 0 }
		c.env.Settings.GetSetting(var(ns, key)):SetValueChangedCallback(function(_, value)
			seen.count = seen.count + 1
			seen.value = value
		end)
		return seen
	end

	-- a mixed bag of agents in one section
	local function bag()
		return {
			A("n", "waiting_input", "needs-one", { since = START - 90, preview = "Should I deploy?" }),
			A("w", "working", "busy", { since = START - 30, activity = "Running tests", todo = "1/2" }),
			A("u", "idle", "fresh", { since = START - 60, doneAt = START - 5, stats = "1m 12s", preview = "All green" }),
			A("x", "exited", "gone", { since = START - 900, last = "exit code 1" }),
			A("i1", "idle", "calm-one", { since = 0 }),
			A("i2", "idle", "calm-two", { since = 0 }),
		}
	end

	----------------------------------------------------------------------
	-- the page itself
	----------------------------------------------------------------------

	test("settings: an AddOn category 'Ompcraft' with every section and setting, defaults as specified", function()
		local _, c, ns = setup({ install = false })
		local cat = c:settingsCategory()
		eq(cat.name, "Ompcraft")
		eq(cat.registered, true, "registered as an AddOn category")
		eq(#c.settingsCategories, 1)

		local headers = {}
		for _, h in ipairs(initializers(c, "SettingsListSectionHeaderTemplate")) do
			headers[#headers + 1] = h.name
		end
		eq(table.concat(headers, ","), "Placement,Agent states,Detail lines,Visibility,Alerts,Link")

		local labels = {}
		for _, s in ipairs(cat.settings) do
			labels[#labels + 1] = s.name
		end
		eq(table.concat(labels, "|"), table.concat({
			"Position", "Lock position", "Scale", "Max height",
			"Needs you", "Working", "Finished, not yet seen", "Exited / failed", "Idle summary line",
			"Current activity", "Todo progress", "Turn time & cost", "Reply preview",
			"Group by Tern session", "Only show the tracker when something needs me or is working",
			"Hide during combat", "Hide in dungeons, raids and PvP",
			"Pop-up: needs you", "Pop-up: finished", "Pop-up: failed or exited", "Pop-up: updates", "Sound", "Chat line",
		}, "|"))
		eq(#initializers(c, "SettingsCheckboxControlTemplate"), 20)
		eq(#initializers(c, "SettingsSliderControlTemplate"), 2)
		eq(#initializers(c, "SettingsDropdownControlTemplate"), 1)
		eq(#initializers(c, "SettingButtonControlTemplate"), 3)

		-- defaults, as stored in OmpcraftDB
		local db = ns.db
		eq(get(c, ns, "position"), "topleft", "a new install starts top left")
		eq(db.locked, true)
		eq(db.scale, 1)
		eq(db.maxHeight, 440)
		for _, key in ipairs({ "needs", "working", "unseen", "exited", "idle" }) do
			eq(db.show[key], true, "show " .. key)
		end
		for _, key in ipairs({ "activity", "todo", "stats", "preview" }) do
			eq(db.lines[key], true, "line " .. key)
		end
		for _, key in ipairs({ "input", "done", "failed", "update", "sound", "chat" }) do
			eq(db.alerts[key], true, "alert " .. key)
		end
		eq(db.grouped, true)
		eq(db.onlyWhenActive, false)
		eq(db.hideInCombat, false)
		eq(db.hideInInstance, false)
		-- the settings are the database fields themselves
		eq(get(c, ns, "scale"), 1)
		eq(get(c, ns, "show_needs"), true)
		eq(get(c, ns, "onlyWhenActive"), false)
		eq(ns.errorCount, 0)
	end)

	test("settings: slider ranges, dropdown choices and button texts", function()
		local _, c, ns = setup({ session = false })
		local sliders = initializers(c, "SettingsSliderControlTemplate")
		local scale, height = sliders[1], sliders[2]
		eq(scale.setting.name, "Scale")
		eq(scale.options.minValue, 0.6)
		eq(scale.options.maxValue, 1.5)
		near(scale.options.steps, 18, 1e-6, "0.05 steps")
		eq(scale.options.formatters[2](0.95), "0.95")
		eq(height.setting.name, "Max height")
		eq(height.options.minValue, 200)
		eq(height.options.maxValue, 800)
		near(height.options.steps, 30, 1e-6, "20 px steps")
		eq(height.options.formatters[2](440), "440 px")

		local dropdown = initializers(c, "SettingsDropdownControlTemplate")[1]
		local data = dropdown.options()
		eq(#data, 4)
		eq(data[1].value, "topleft")
		eq(data[1].label, "Top left")
		eq(data[2].value, "tracker")
		eq(data[2].label, "Under quest objectives")
		eq(data[3].value, "corner")
		eq(data[3].label, "Top right")
		eq(data[4].value, "free")
		eq(data[4].label, "Where I drag it")
		eq(dropdown.setting:GetVariableType(), "string")

		-- link buttons: the status text is read when the row is built
		local status = button(c, "Link status")
		eq(type(status.buttonText), "function")
		eq(status.buttonText(), "stale", "no snapshot yet")
		eq(button(c, "Reset position").buttonText, "Reset")
		eq(button(c, "Link self-test").buttonText, "Run self-test")
		for _, init in ipairs(initializers(c, "SettingButtonControlTemplate")) do
			truthy(type(init.tooltip) == "string" and #init.tooltip > 0, "tooltip for " .. init.name)
		end
		-- the pop-up note is on the Alerts header and on the pop-up switches
		local alerts
		for _, h in ipairs(initializers(c, "SettingsListSectionHeaderTemplate")) do
			if h.name == "Alerts" then
				alerts = h
			end
		end
		eq(alerts.tooltip, "Pop-ups always wait until combat ends.")
		for _, init in ipairs(initializers(c, "SettingsCheckboxControlTemplate")) do
			if init.setting.name:find("^Pop%-up") then
				contains(init.tooltip, "wait until combat ends")
			end
		end
	end)

	test("settings: without the Settings API the addon still runs", function()
		local _, c, ns = setup({ install = false, noSettings = true })
		eq(rawget(c.env, "Settings"), nil)
		eq(ns.Config.category, nil)
		eq(ns.errorCount, 0)
		put(ns, bag())
		truthy(#agentRows(c) > 0, "the tracker works")
		c:clearChat()
		c:slash("/oc config")
		contains(c:chatText(), "not available")
		c:slash("/oc lock") -- Notify has nothing to tell
		eq(ns.errorCount, 0)
	end)

	test("settings: /oc config and /oc settings open the page; not in combat; help lists it", function()
		local _, c, ns = setup({ install = false })
		c:slash("/oc config")
		eq(c.openedCategory, 1)
		c.openedCategory = nil
		c:slash("/oc settings")
		eq(c.openedCategory, 1)
		c.openedCategory = nil
		c:setCombat(true)
		c:clearChat()
		c:slash("/oc config")
		eq(c.openedCategory, nil, "Blizzard's panel is restricted in combat")
		contains(c:chatText(), "combat")
		eq(ns.errorCount, 0)
		c:setCombat(false)
		c:clearChat()
		c:slash("/oc help")
		contains(c:chatText(), "/oc config")
		-- the AddOn button's right click
		c.env.Ompcraft_OnAddonCompartmentClick("Ompcraft", "RightButton")
		eq(c.openedCategory, 1)
		c:clearChat()
		c.env.Ompcraft_OnAddonCompartmentEnter("Ompcraft", c.env.UIParent)
		contains(table.concat(c:tooltipLines(), "\n"), "Right-click: settings")
	end)

	----------------------------------------------------------------------
	-- placement
	----------------------------------------------------------------------

	test("settings: Position - under objectives, top right, or where I drag it", function()
		local _, c, ns = setup({ install = false, tracker = { shown = true, bottom = 500 }, saved = { version = 4, anchor = "tracker" } })
		local f = ns.Tracker.frame
		local nine = c.env.ObjectiveTrackerFrame.NineSlice
		eq(get(c, ns, "position"), "tracker")
		eq(select(2, f:GetPoint(1)), nine)

		set(c, ns, "position", "corner")
		local point, rel, relPoint, x, y = f:GetPoint(1)
		eq(rel, c.env.UIParent)
		eq(point, "TOPRIGHT")
		eq(relPoint, "TOPRIGHT")
		eq(x, -24)
		eq(y, -300)
		eq(ns.db.anchor, "corner")
		eq(get(c, ns, "position"), "corner")
		c:advance(1.1)
		eq(select(2, f:GetPoint(1)), c.env.UIParent, "it stays in the corner while Blizzard's tracker is up")

		set(c, ns, "position", "tracker")
		eq(select(2, f:GetPoint(1)), nine)

		-- "Where I drag it" pins it where it is now, and unlocks
		f.__s.left, f.__s.top = 300, 700
		eq(ns.db.locked, true)
		set(c, ns, "position", "free")
		eq(ns.db.locked, false, "selecting it unlocks")
		eq(get(c, ns, "locked"), false, "and the checkbox follows")
		eq(ns.db.pos.point, "TOPLEFT")
		eq(ns.db.pos.relPoint, "BOTTOMLEFT")
		eq(ns.db.pos.x, 300)
		eq(ns.db.pos.y, 700)
		local p2, r2, rp2, x2, y2 = f:GetPoint(1)
		eq(r2, c.env.UIParent)
		eq(p2, "TOPLEFT")
		eq(rp2, "BOTTOMLEFT")
		eq(x2, 300)
		eq(y2, 700)
		eq(get(c, ns, "position"), "free")
		c:advance(1.1)
		eq(select(2, f:GetPoint(1)), c.env.UIParent, "a pinned spot beats Blizzard's tracker")

		-- either of the first two forgets the dragged spot
		set(c, ns, "position", "corner")
		eq(ns.db.pos, nil)
		eq(get(c, ns, "position"), "corner")
		set(c, ns, "position", "free")
		set(c, ns, "position", "tracker")
		eq(ns.db.pos, nil)
		eq(select(2, f:GetPoint(1)), nine)
		eq(ns.errorCount, 0)
	end)

	test("settings: a new install sits top left; a saved choice is kept", function()
		local _, c, ns = setup({ install = false, tracker = { shown = true, bottom = 500 } })
		local point, rel, relPoint, x, y = ns.Tracker.frame:GetPoint(1)
		eq(point, "TOPLEFT")
		eq(rel, c.env.UIParent)
		eq(relPoint, "TOPLEFT")
		eq(x, 24)
		eq(y, -260)
		eq(ns.db.anchor, "topleft")
		c:advance(1.1)
		eq(select(2, ns.Tracker.frame:GetPoint(1)), c.env.UIParent, "Blizzard's tracker does not move it")
		set(c, ns, "position", "tracker")
		eq(select(2, ns.Tracker.frame:GetPoint(1)), c.env.ObjectiveTrackerFrame.NineSlice)
		set(c, ns, "position", "topleft")
		eq(select(4, ns.Tracker.frame:GetPoint(1)), 24)

		-- saves from before the option existed keep their placement
		local _, _, ns2 = setup({ install = false, saved = { version = 3 } })
		eq(ns2.db.anchor, "tracker")
		local _, _, ns3 = setup({ install = false, saved = { version = 4, anchor = "corner" } })
		eq(ns3.db.anchor, "corner")
		eq(ns.errorCount, 0)
	end)

	test("settings: Position - 'free' with unreadable geometry falls back to the corner spot", function()
		local _, c, ns = setup({ install = false })
		set(c, ns, "position", "free") -- the stub frame has no geometry yet
		eq(ns.db.pos.point, "TOPRIGHT")
		eq(ns.db.pos.x, -24)
		eq(ns.db.pos.y, -300)
		eq(ns.db.locked, false)
	end)

	test("settings: a drag makes the dropdown read 'Where I drag it'", function()
		local _, c, ns = setup({ install = false })
		set(c, ns, "locked", false)
		local header, f = ns.Tracker.header, ns.Tracker.frame
		header.__s.scripts.OnDragStart(header)
		f:ClearAllPoints()
		f:SetPoint("TOPLEFT", c.env.UIParent, "TOPLEFT", 120, -80)
		header.__s.scripts.OnDragStop(header)
		eq(get(c, ns, "position"), "free")
		set(c, ns, "position", "corner")
		eq(get(c, ns, "position"), "corner")
	end)

	test("settings: Lock position, and the slash commands keep the page in step", function()
		local _, c, ns = setup({ install = false })
		local locked, scale, position = spy(c, ns, "locked"), spy(c, ns, "scale"), spy(c, ns, "position")
		eq(get(c, ns, "locked"), true)
		set(c, ns, "locked", false)
		eq(ns.db.locked, false)
		set(c, ns, "locked", true)
		eq(ns.db.locked, true)

		locked.count = 0
		c:slash("/oc unlock")
		eq(ns.db.locked, false)
		eq(get(c, ns, "locked"), false)
		eq(locked.count, 1, "the open page was told")
		eq(locked.value, false)
		c:slash("/oc lock")
		eq(get(c, ns, "locked"), true)
		eq(locked.value, true)

		c:slash("/oc scale 1.25")
		eq(get(c, ns, "scale"), 1.25)
		eq(scale.value, 1.25)

		c:slash("/oc unlock")
		set(c, ns, "position", "free")
		position.count = 0
		c:slash("/oc reset")
		eq(ns.db.pos, nil)
		eq(get(c, ns, "position"), "topleft")
		eq(position.count, 1)
		eq(position.value, "topleft")
		eq(ns.errorCount, 0)
	end)

	test("settings: Scale is applied live and keeps a dragged spot where it is", function()
		local _, c, ns = setup({ install = false })
		set(c, ns, "scale", 1.2)
		eq(ns.db.scale, 1.2)
		eq(c.env.OmpcraftTracker.__s.scale, 1.2)
		set(c, ns, "scale", 0.9500000001) -- a slider's float noise
		eq(ns.db.scale, 0.95)
		eq(c.env.OmpcraftTracker.__s.scale, 0.95)
		set(c, ns, "scale", 1)
		set(c, ns, "position", "free")
		ns.db.pos = { point = "TOPLEFT", relPoint = "TOPLEFT", x = 200, y = -100 }
		ns.Tracker.anchorMode = nil
		set(c, ns, "scale", 0.8)
		near(ns.db.pos.x, 250, 1e-6)
		near(ns.db.pos.y, -125, 1e-6)
		eq(c.env.OmpcraftTracker.__s.scale, 0.8)
	end)

	test("settings: Max height limits the tracker, with +N more", function()
		local _, c, ns = setup({ install = false })
		local agents = {}
		for i = 1, 30 do
			agents[#agents + 1] = A("w" .. i, "working", "busy-" .. i, { since = START - i, activity = "Running", todo = "1/2" })
		end
		put(ns, agents)
		local base = #agentRows(c)
		truthy(base < 30 and base > 3)
		set(c, ns, "maxHeight", 200)
		local small = #agentRows(c)
		truthy(small < base, "a lower limit shows fewer")
		eq(trackerKinds(c, "more")[1].text, string.format("+%d more", 30 - small))
		truthy(ns.Tracker.frame:GetHeight() <= 200, "within the limit: " .. ns.Tracker.frame:GetHeight())
		set(c, ns, "maxHeight", 800)
		truthy(#agentRows(c) > base, "a higher limit shows more")
		eq(ns.db.maxHeight, 800)
		set(c, ns, "maxHeight", 20 * 21 + 0.3) -- not on a step
		eq(ns.db.maxHeight, 420)
	end)

	----------------------------------------------------------------------
	-- what to show
	----------------------------------------------------------------------

	test("settings: each agent state can be switched off, live", function()
		local _, c, ns = setup({ install = false })
		put(ns, bag())
		eq(kinds(c), "header,section,agent,agent,agent,agent,idle")
		eq(names(agentRows(c)), "needs-one,busy,fresh,gone", "the baseline, in the store's order")

		set(c, ns, "show_needs", false)
		eq(names(agentRows(c)), "busy,fresh,gone")
		set(c, ns, "show_needs", true)
		set(c, ns, "show_working", false)
		eq(names(agentRows(c)), "needs-one,fresh,gone")
		set(c, ns, "show_working", true)
		set(c, ns, "show_exited", false)
		eq(names(agentRows(c)), "needs-one,busy,fresh")
		set(c, ns, "show_exited", true)

		-- "finished, not yet seen" off: such an agent counts as idle
		set(c, ns, "show_unseen", false)
		eq(names(agentRows(c)), "needs-one,busy,gone")
		eq(trackerKinds(c, "idle")[1].text, "3 idle")
		set(c, ns, "show_unseen", true)
		eq(trackerKinds(c, "idle")[1].text, "2 idle")

		-- the idle line off: idle agents are not listed at all
		set(c, ns, "show_idle", false)
		eq(#trackerKinds(c, "idle"), 0)
		eq(names(agentRows(c)), "needs-one,busy,fresh,gone")
		set(c, ns, "show_unseen", false)
		eq(names(agentRows(c)), "needs-one,busy,gone", "unseen counts as idle, and idle is hidden")
		set(c, ns, "show_unseen", true)
		set(c, ns, "show_idle", true)

		-- everything off: a hint instead of a blank box
		for _, key in ipairs({ "show_needs", "show_working", "show_unseen", "show_exited", "show_idle" }) do
			set(c, ns, key, false)
		end
		eq(kinds(c), "header,empty")
		eq(trackerKinds(c, "empty")[1].text, "Nothing to show with your settings.")
		eq(ns.errorCount, 0)
	end)

	test("settings: each detail line can be switched off, live", function()
		local _, c, ns = setup({ install = false })
		put(ns, bag())
		local function linesOf(name)
			for _, r in ipairs(agentRows(c)) do
				if r.name == name then
					local out = {}
					for i, l in ipairs(r.lines) do
						out[i] = l.text
					end
					return table.concat(out, "|")
				end
			end
		end
		eq(linesOf("busy"), "Running tests|Todo 1/2")
		eq(linesOf("fresh"), "1m 12s|All green")
		eq(linesOf("needs-one"), "Should I deploy?")
		eq(linesOf("gone"), "exit code 1")

		set(c, ns, "lines_activity", false)
		eq(linesOf("busy"), "Todo 1/2")
		set(c, ns, "lines_activity", true)
		set(c, ns, "lines_todo", false)
		eq(linesOf("busy"), "Running tests")
		set(c, ns, "lines_todo", true)
		set(c, ns, "lines_stats", false)
		eq(linesOf("fresh"), "All green")
		set(c, ns, "lines_stats", true)
		set(c, ns, "lines_preview", false)
		eq(linesOf("fresh"), "1m 12s")
		eq(linesOf("needs-one"), "")
		eq(linesOf("gone"), "")
		set(c, ns, "lines_activity", false)
		set(c, ns, "lines_todo", false)
		eq(linesOf("busy"), "", "a working agent with no lines is just a title")
		eq(ns.errorCount, 0)
	end)

	test("settings: ungrouped is one flat list, ordered needs, working, finished, exited", function()
		local _, c, ns = setup({ install = false })
		put(ns, {
			A("w1", "working", "work-a", { section = "A", since = START - 5 }),
			A("n1", "waiting_input", "need-a", { section = "A", since = START - 10 }),
			A("n2", "waiting_input", "need-b", { section = "B", since = START - 20 }),
			A("x1", "exited", "gone-b", { section = "B" }),
			A("u1", "idle", "fresh-b", { section = "B", doneAt = START - 3 }),
			A("i1", "idle", "calm-b", { section = "B", since = 0 }),
			A("i2", "idle", "calm-a", { section = "A", since = 0 }),
		})
		eq(kinds(c), "header,section,agent,agent,section,agent,agent,agent,idle", "grouped by session")
		eq(trackerKinds(c, "section")[1].text, "A (2)")

		set(c, ns, "grouped", false)
		eq(kinds(c), "header,agent,agent,agent,agent,agent,idle", "flat: no section headers")
		eq(names(agentRows(c)), "need-a,need-b,work-a,fresh-b,gone-b")
		eq(trackerKinds(c, "idle")[1].text, "2 idle")
		trackerKinds(c, "idle")[1].frame.__s.scripts.OnClick(trackerKinds(c, "idle")[1].frame)
		eq(kinds(c), "header,agent,agent,agent,agent,agent,idle,agent,agent", "idle agents, with no sub-labels")
		eq(#trackerKinds(c, "label"), 0)
		trackerKinds(c, "idle")[1].frame.__s.scripts.OnClick(trackerKinds(c, "idle")[1].frame)

		set(c, ns, "grouped", true)
		eq(kinds(c), "header,section,agent,agent,section,agent,agent,agent,idle")
		-- section collapsing still works when grouped again
		set(c, ns, "grouped", false)
		eq(ns.errorCount, 0)
	end)

	test("settings: an ungrouped list respects the height limit like a grouped one", function()
		local _, c, ns = setup({ install = false })
		local agents = {}
		for i = 1, 20 do
			agents[#agents + 1] = A("w" .. i, "working", "busy-" .. i, { section = "S" .. i, activity = "Running", todo = "1/2" })
		end
		put(ns, agents)
		local grouped = #agentRows(c)
		set(c, ns, "grouped", false)
		truthy(#agentRows(c) > grouped, "twenty one-agent sections cost twenty headers; flat does not")
		truthy(ns.Tracker.frame:GetHeight() <= 440)
		eq(trackerKinds(c, "more")[1].text, string.format("+%d more", 20 - #agentRows(c)))
	end)

	----------------------------------------------------------------------
	-- when the tracker is on screen
	----------------------------------------------------------------------

	test("settings: only show the tracker when something needs me or is working", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("i1", "idle", "calm", { since = 0 }) })
		eq(shown(c), true, "off by default")
		set(c, ns, "onlyWhenActive", true)
		eq(shown(c), false, "nothing active: hidden entirely")
		eq(ns.Tracker.ticker, nil, "and nothing ticks")

		put(ns, { A("w", "working", "busy") })
		eq(shown(c), true, "something is working")
		put(ns, { A("i1", "idle", "calm", { since = 0 }) })
		eq(shown(c), false)
		put(ns, { A("n", "waiting_input", "asks", { preview = "?" }) })
		eq(shown(c), true, "something needs me")
		put(ns, { A("u", "idle", "fresh", { doneAt = START - 5 }) })
		eq(shown(c), false, "finished-unseen alone is not enough")

		-- the slash command, the header click and the AddOn button still bring it up
		c:slash("/oc show")
		eq(shown(c), true)
		eq(ns.db.shown, true)
		put(ns, { A("i1", "idle", "calm", { since = 0 }) })
		eq(shown(c), true, "an explicit show stays until something active comes and goes")
		c:slash("/oc hide")
		eq(shown(c), false)
		eq(ns.db.shown, false)
		put(ns, { A("w", "working", "busy") })
		eq(shown(c), false, "turned off means off")
		c:slash("/oc")
		eq(shown(c), true)
		put(ns, { A("i1", "idle", "calm", { since = 0 }) })
		eq(shown(c), false, "hidden again: nothing active")
		c:slash("/oc")
		eq(shown(c), true, "a toggle brings it up anyway")
		c:slash("/oc")
		eq(shown(c), false)
		eq(ns.db.shown, false, "and turns it off")
		c.env.Ompcraft_OnAddonCompartmentClick("Ompcraft", "LeftButton")
		eq(shown(c), true, "the AddOn button toggles it")

		set(c, ns, "onlyWhenActive", false)
		eq(shown(c), true)
		eq(ns.errorCount, 0)
	end)

	test("settings: hide during combat, and come back after", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("w", "working", "busy") })
		set(c, ns, "hideInCombat", true)
		eq(shown(c), true, "not in combat")
		c:setCombat(true)
		eq(shown(c), false)
		c:setCombat(false)
		eq(shown(c), true, "re-shown")
		-- turned off by the player: it stays off afterwards
		c:slash("/oc hide")
		c:setCombat(true)
		c:setCombat(false)
		eq(shown(c), false)
		c:slash("/oc show")
		-- the setting turned off mid-fight shows it again at once
		c:setCombat(true)
		eq(shown(c), false)
		set(c, ns, "hideInCombat", false)
		eq(shown(c), true)
		c:setCombat(false)
		eq(ns.errorCount, 0)
	end)

	test("settings: hide in dungeons, raids and PvP", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("w", "working", "busy") })
		set(c, ns, "hideInInstance", true)
		eq(shown(c), true)
		for _, kind in ipairs({ "party", "raid", "pvp", "arena" }) do
			c.instanceType = kind
			c:fire("PLAYER_ENTERING_WORLD")
			eq(shown(c), false, kind)
			c.instanceType = "none"
			c:fire("ZONE_CHANGED_NEW_AREA")
			eq(shown(c), true, "outside again after " .. kind)
		end
		c.instanceType = "scenario"
		c:fire("PLAYER_ENTERING_WORLD")
		eq(shown(c), true, "scenarios are not hidden")
		c.instanceType = "raid"
		c:fire("PLAYER_ENTERING_WORLD")
		eq(shown(c), false)
		set(c, ns, "hideInInstance", false)
		eq(shown(c), true, "turning the rule off shows it at once")
		set(c, ns, "hideInInstance", true)
		eq(shown(c), false, "and turning it on hides it at once")
		eq(ns.errorCount, 0)
	end)

	test("settings: the hide rules never touch protected frames", function()
		local _, c, ns = setup({ install = false })
		put(ns, { A("w", "working", "busy") })
		set(c, ns, "hideInCombat", true)
		set(c, ns, "hideInInstance", true)
		c:setCombat(true)
		c.instanceType = "raid"
		c:fire("PLAYER_ENTERING_WORLD")
		-- only the addon's own plain frame was shown or hidden (the stub errors on anything else)
		eq(c.env.OmpcraftTracker.__s.template, nil)
		eq(ns.errorCount, 0)
	end)

	----------------------------------------------------------------------
	-- alerts
	----------------------------------------------------------------------

	local function toast(ns, id, kind, text)
		ns:Fire("TOAST", { id = id, kind = kind, agent = "nobody", text = text or (kind .. " toast") })
	end

	test("settings: each pop-up can be switched off", function()
		local _, c, ns = setup({ install = false })
		local cases = {
			{ switch = "alerts_input", kinds = { "input" } },
			{ switch = "alerts_done", kinds = { "done" } },
			{ switch = "alerts_failed", kinds = { "failed", "exited" } },
			{ switch = "alerts_update", kinds = { "update", "something-new" } },
		}
		local id = 0
		for _, case in ipairs(cases) do
			set(c, ns, case.switch, false)
			for _, kind in ipairs(case.kinds) do
				id = id + 1
				toast(ns, id, kind)
			end
			eq(#c:visibleToasts(), 0, case.switch .. " off: nothing shows")
			eq(select(2, ns.Toasts:Counts()), 0, "and nothing waits")
			set(c, ns, case.switch, true)
			c:advance(1.2)
			id = id + 1
			toast(ns, id, case.kinds[1])
			eq(#c:visibleToasts(), 1, case.switch .. " on: it shows")
			c:advance(7.5)
			eq(#c:visibleToasts(), 0)
		end
		eq(ns.errorCount, 0)
	end)

	test("settings: a switched-off pop-up still counts as seen", function()
		local _, c, ns = setup({ install = false })
		set(c, ns, "alerts_done", false)
		local id = (START - 5) * 1000
		put(ns, { A("u", "idle", "fresh", { doneAt = START - 5 }) }, { toasts = { { id = id, kind = "done", agent = "u", text = "fresh finished" } } })
		eq(ns.db.lastToast, id, "it will not come back after a reload")
		eq(#c:visibleToasts(), 0)
		set(c, ns, "alerts_done", true)
		put(ns, { A("u", "idle", "fresh", { doneAt = START - 5 }) }, { toasts = { { id = id, kind = "done", agent = "u", text = "fresh finished" } } })
		eq(#c:visibleToasts(), 0, "not replayed")
	end)

	test("settings: sound and chat line are separate switches", function()
		local _, c, ns = setup({ install = false })
		set(c, ns, "alerts_sound", false)
		toast(ns, 1, "input", "asker needs you")
		eq(#c:visibleToasts(), 1, "the pop-up still shows")
		eq(#c.soundsPlayed, 0, "without a sound")
		contains(c:chatText(), "asker needs you")

		c:advance(7.5)
		c:clearChat()
		set(c, ns, "alerts_sound", true)
		set(c, ns, "alerts_chat", false)
		toast(ns, 2, "input", "second needs you")
		eq(#c:visibleToasts(), 1)
		eq(#c.soundsPlayed, 1)
		lacks(c:chatText(), "second needs you")

		c:advance(7.5)
		set(c, ns, "alerts_sound", false)
		toast(ns, 3, "done", "quiet one")
		eq(#c:visibleToasts(), 1, "both off: still a pop-up")
		eq(#c.soundsPlayed, 1)
		eq(ns.errorCount, 0)
	end)

	test("settings: pop-ups still wait for the end of combat", function()
		local _, c, ns = setup({ install = false })
		c:setCombat(true)
		toast(ns, 1, "input")
		eq(#c:visibleToasts(), 0)
		eq(select(2, ns.Toasts:Counts()), 1)
		c:setCombat(false)
		eq(#c:visibleToasts(), 1)
	end)

	----------------------------------------------------------------------
	-- link buttons
	----------------------------------------------------------------------

	test("settings: link status and self-test buttons print to chat", function()
		local fx, c, ns = setup({ agents = { A("w", "working", "busy") } })
		c:advance(2.1)
		local status = button(c, "Link status")
		eq(status.buttonText(), "live")
		c:clearChat()
		status.buttonClick()
		contains(c:chatText(), "link: live")
		contains(c:chatText(), "mode: doorbell")
		c:clearChat()
		button(c, "Link self-test").buttonClick()
		contains(c:chatText(), "self-test: pass")
		fx.tern = "down"
		fx:publish(c)
		c:advance(1.5)
		eq(status.buttonText(), "Tern down", "read again each time the row is built")
	end)

	test("settings: Reset position clears a dragged spot and the page follows", function()
		local _, c, ns = setup({ install = false, tracker = { shown = true, bottom = 500 }, saved = { version = 4, anchor = "tracker" } })
		c.env.OmpcraftTracker.__s.left, c.env.OmpcraftTracker.__s.top = 50, 60
		set(c, ns, "position", "free")
		eq(get(c, ns, "position"), "free")
		local position = spy(c, ns, "position")
		button(c, "Reset position").buttonClick()
		eq(ns.db.pos, nil)
		eq(get(c, ns, "position"), "tracker")
		eq(position.value, "tracker")
		eq(select(2, c.env.OmpcraftTracker:GetPoint(1)), c.env.ObjectiveTrackerFrame.NineSlice)
	end)

	----------------------------------------------------------------------
	-- the header menu
	----------------------------------------------------------------------

	test("settings: right-click on the Agents header opens a small menu", function()
		local _, c, ns = setup({ install = false })
		local header = ns.Tracker.header
		eq(header.__s.clicks[2], "RightButtonUp")
		header.__s.scripts.OnClick(header, "RightButton")
		local menu = c.lastMenu
		truthy(menu, "a context menu")
		eq(menu.owner, header)
		local texts = {}
		for _, e in ipairs(menu.elements) do
			texts[#texts + 1] = e.kind .. ":" .. (e.text or "")
		end
		eq(table.concat(texts, ","), "title:Ompcraft,checkbox:Locked,checkbox:Show idle agents,divider:,button:Settings...")

		local lock, idle, settings = menu.elements[2], menu.elements[3], menu.elements[5]
		eq(lock.a(), true)
		lock.b()
		eq(ns.db.locked, false)
		eq(get(c, ns, "locked"), false, "the page follows")
		eq(idle.a(), false)
		idle.b()
		eq(ns.db.idleOpen, true)
		settings.a()
		eq(c.openedCategory, 1)

		-- a left click still collapses the tracker
		c.lastMenu = nil
		header.__s.scripts.OnClick(header, "LeftButton")
		eq(ns.db.collapsed, true)
		eq(c.lastMenu, nil)
		eq(ns.errorCount, 0)
	end)

	test("settings: no Menu API, no menu, no error", function()
		local _, c, ns = setup({ install = false, noMenu = true })
		local header = ns.Tracker.header
		header.__s.scripts.OnClick(header, "RightButton")
		eq(c.lastMenu, nil)
		eq(ns.errorCount, 0)
	end)

	----------------------------------------------------------------------
	-- saved variables
	----------------------------------------------------------------------

	test("settings: v3 to v4 keeps every choice and fills the new defaults", function()
		local _, c, ns = setup({ install = false })
		local db = ns.MigrateDB({
			version = 3, locked = false, scale = 0.8, idleOpen = true, collapsed = true, shown = false, lastToast = 5,
			pos = { point = "TOP", relPoint = "TOP", x = 1, y = -2 }, collapsedSections = { S = true }, seen = { a = 1 },
		})
		eq(db.version, 4)
		eq(db.locked, false)
		eq(db.scale, 0.8)
		eq(db.idleOpen, true)
		eq(db.collapsed, true)
		eq(db.shown, false)
		eq(db.lastToast, 5)
		eq(db.pos.point, "TOP")
		eq(db.collapsedSections.S, true)
		eq(db.seen.a, 1)
		eq(db.anchor, "tracker")
		eq(db.maxHeight, 440)
		eq(db.grouped, true)
		eq(db.onlyWhenActive, false)
		eq(db.show.needs, true)
		eq(db.lines.stats, true)
		eq(db.alerts.update, true)

		-- wrong types and values are repaired; deliberate "off" choices survive
		local fixed = ns.MigrateDB({
			version = 4, maxHeight = 5, anchor = "bogus", grouped = "yes", show = "bad",
			lines = { todo = false, stats = "no" }, alerts = { sound = false, chat = 7 },
		})
		eq(fixed.maxHeight, 440)
		eq(fixed.anchor, "tracker")
		eq(fixed.grouped, true)
		eq(type(fixed.show), "table")
		eq(fixed.show.needs, true)
		eq(fixed.lines.todo, false)
		eq(fixed.lines.stats, true)
		eq(fixed.alerts.sound, false)
		eq(fixed.alerts.chat, true)
		eq(ns.MigrateDB({ version = 4, maxHeight = 801 }).maxHeight, 440)
		eq(ns.MigrateDB({ version = 4, maxHeight = 200 }).maxHeight, 200)
		eq(ns.MigrateDB({ version = 4, anchor = "corner" }).anchor, "corner")
	end)

	test("settings: a v3 save loads with the dragged spot, scale and lock it had", function()
		local _, c, ns = setup({
			install = false,
			saved = {
				version = 3, locked = false, scale = 0.8, shown = false, idleOpen = true,
				pos = { point = "TOPLEFT", relPoint = "TOPLEFT", x = 120, y = -80 },
			},
		})
		eq(ns.db.version, 4)
		eq(get(c, ns, "position"), "free")
		eq(get(c, ns, "locked"), false)
		eq(get(c, ns, "scale"), 0.8)
		eq(c.env.OmpcraftTracker.__s.scale, 0.8)
		local point, rel, _, x, y = c.env.OmpcraftTracker:GetPoint(1)
		eq(point, "TOPLEFT")
		eq(rel, c.env.UIParent)
		eq(x, 120)
		eq(y, -80)
		eq(shown(c), false, "still turned off")
		eq(ns.db.idleOpen, true)
		-- and settings made this session are saved with it
		set(c, ns, "show_idle", false)
		c:reload()
		eq(c:ns().db.show.idle, false)
		eq(c:ns().db.version, 4)
	end)

	test("settings: only verified Settings and Menu names are used", function()
		local _, c, ns = setup({ install = false })
		-- the stub errors on any Settings.*, MenuUtil.* or root description method it does
		-- not know; a clean run through registration, a menu and the buttons proves the names
		ns.Tracker:OpenMenu(ns.Tracker.header)
		ns.Config:Open()
		for _, init in ipairs(initializers(c, "SettingButtonControlTemplate")) do
			init.buttonClick()
		end
		eq(ns.errorCount, 0)
		truthy(Stub)
	end)
end
