-- Ompcraft: the agent tracker. No box: gold section headers, quest-style titles
-- and objective lines in the objective tracker's own fonts and colours, anchored
-- under Blizzard's tracker when it is shown (anchor only: never parented to it,
-- never modifying it).

local _, ns = ...

local Tracker = {}
ns.Tracker = Tracker

local Style = ns.Style

local WIDTH = 260
local HEADER_H = 32
local SECTION_H = 26
local SECTION_GAP = 8
local TITLE_H = 18
local LINE_H = 15
local BLOCK_PAD = 5
local MARKER = 16
local MAX_LINES = 3
local NAME_CHARS = 30
local LINE_CHARS = 46
local HOVER_SEEN = 1 -- seconds of hovering that marks a turn seen
local CLICK_AFTER_DRAG = 0.3
local ANCHOR_GAP = 12 -- under Blizzard's tracker
local FALLBACK_X, FALLBACK_Y = -24, -300 -- top right
-- Top left: clear of the default player frame (bottom centre on Edit Mode's Modern
-- layout; top left at (4, -4) on its Classic layout, so it starts below that).
local TOPLEFT_X, TOPLEFT_Y = 24, -260

Tracker.MIN_SCALE, Tracker.MAX_SCALE = 0.6, 1.5
Tracker.WIDTH = WIDTH

-- Order in which things survive when the tracker runs out of height.
local PRIORITY = { needs = 1, working = 2, unseen = 3, exited = 4, other = 5, idleSummary = 6, idle = 7 }

local STATE_TEXT = {
	waiting_input = "waiting for you",
	working = "working",
	idle = "idle",
	exited = "exited",
}

local STATUS_TONE = {
	good = { 0.3, 0.85, 0.4 },
	info = { 0.3, 0.6, 1 },
	warn = { 1, 0.69, 0 },
	bad = { 0.9, 0.25, 0.25 },
}

Tracker.sections, Tracker.blocks, Tracker.idles, Tracker.labels = {}, {}, {}, {}
Tracker.shown = {} -- what the last Refresh put on screen, top to bottom

------------------------------------------------------------------------
-- small helpers
------------------------------------------------------------------------

local function sectionName(a)
	if a.section ~= "" then
		return a.section
	elseif a.session ~= "" then
		return a.session
	end
	return "Agents"
end

local function metaOf(a)
	local parts = {}
	if a.host ~= "" then
		parts[#parts + 1] = a.host
	end
	if a.session ~= "" then
		parts[#parts + 1] = a.session
	end
	if a.model ~= "" then
		parts[#parts + 1] = a.model
	end
	return table.concat(parts, " \194\183 ")
end

local function elapsedOf(a, now)
	if a.since and a.since > 0 then
		return ns.FormatElapsed(now - a.since)
	end
	return ""
end

local function highlightable(button, on, off)
	button:SetScript("OnEnter", ns.Guard("hover", function(self)
		on(self)
	end))
	button:SetScript("OnLeave", ns.Guard("hover", function(self)
		off(self)
	end))
end

-- Buttons made of atlas textures, like the tracker's collapse buttons.
local function atlasButton(parent, w, h, highlightAtlas)
	local b = CreateFrame("Button", nil, parent)
	b:SetSize(w, h)
	b.face = b:CreateTexture(nil, "ARTWORK")
	b.face:SetAllPoints(b)
	-- Drawn only while the mouse is over the button (the engine does that for
	-- the HIGHLIGHT layer), as the tracker's own collapse buttons do.
	b.glow = b:CreateTexture(nil, "HIGHLIGHT")
	b.glow:SetAllPoints(b)
	b.glow:SetAtlas(highlightAtlas, false)
	b.glow:SetBlendMode("ADD")
	b:SetScript("OnMouseDown", ns.Guard("btn-down", function(self)
		if self.pressedAtlas then
			self.face:SetAtlas(self.pressedAtlas, false)
		end
	end))
	b:SetScript("OnMouseUp", ns.Guard("btn-up", function(self)
		if self.normalAtlas then
			self.face:SetAtlas(self.normalAtlas, false)
		end
	end))
	return b
end

local function setFace(button, normal, pressed)
	button.normalAtlas, button.pressedAtlas = normal, pressed
	button.face:SetAtlas(normal, false)
end

------------------------------------------------------------------------
-- tooltip and seen-on-hover
------------------------------------------------------------------------

local function showTooltip(block)
	local a = block.agent
	if not a then
		return
	end
	local tip = GameTooltip
	tip:SetOwner(block, "ANCHOR_LEFT")
	tip:SetText(a.name ~= "" and a.name or "?", 1, 0.82, 0)
	local meta = metaOf(a)
	if meta ~= "" then
		tip:AddLine(meta, 0.6, 0.6, 0.6)
	end
	if a.cwd ~= "" then
		tip:AddDoubleLine("cwd", a.cwd, 0.6, 0.6, 0.6, 1, 1, 1)
	end
	tip:AddDoubleLine("state", STATE_TEXT[a.state] or "unknown", 0.6, 0.6, 0.6, 1, 1, 1)
	if a.stats ~= "" then
		tip:AddDoubleLine("last turn", a.stats, 0.6, 0.6, 0.6, 1, 1, 1)
	end
	local text = a.preview ~= "" and a.preview or a.last
	if text ~= "" then
		tip:AddLine(text, 0.85, 0.85, 0.85, true)
	end
	tip:Show()
end

local function hideTooltip(block)
	if GameTooltip:GetOwner() == block then
		GameTooltip:Hide()
	end
end

-- Hovering a finished agent for HOVER_SEEN seconds marks its turn seen.
local function startHoverTimer(block)
	block.hoverToken = (block.hoverToken or 0) + 1
	local token, id = block.hoverToken, block.agentId
	C_Timer.After(HOVER_SEEN, ns.Guard("hover-seen", function()
		if block.hoverToken ~= token or not block.hovering then
			return
		end
		local a = ns.Store:Find(id)
		if a and ns.Store:MarkSeen(a) then
			Tracker:Refresh()
		end
	end))
end

------------------------------------------------------------------------
-- colours
------------------------------------------------------------------------

local function applyBlockColors(block)
	local c = Style.color
	local hot = block.hovering
	local titleColor = hot and c.HeaderHighlight or c.Header
	local lineColor = hot and c.NormalHighlight or c.Normal
	if block.kind == "idle" then
		titleColor = hot and c.Normal or c.Complete
	end
	Style.Color(block.name, titleColor)
	Style.Color(block.elapsed, hot and c.Normal or c.Complete)
	for i = 1, #block.lines do
		local line = block.lines[i]
		if line.used then
			Style.Color(line.dash, lineColor)
			local tone = line.tone
			local color = lineColor
			if tone == "complete" then
				color = hot and c.Normal or c.Complete
			elseif tone == "failed" then
				color = hot and c.FailedHighlight or c.Failed
			end
			Style.Color(line.text, color)
		end
	end
end

------------------------------------------------------------------------
-- frames (created on demand and reused)
------------------------------------------------------------------------

function Tracker:NewText(parent, fontName)
	local fs = parent:CreateFontString(nil, "ARTWORK", fontName)
	fs:SetJustifyH("LEFT")
	fs:SetWordWrap(false)
	return fs
end

function Tracker:GetSection(i)
	local s = self.sections[i]
	if s then
		return s
	end
	local f = self.frame
	s = CreateFrame("Button", nil, f)
	s:SetSize(WIDTH, SECTION_H)
	s.bg = s:CreateTexture(nil, "BACKGROUND")
	s.bg:SetAtlas(Style.ATLAS.sectionHeader, true)
	s.bg:SetPoint("CENTER", s, "CENTER", 0, 0)
	s.text = self:NewText(s, Style.font.header)
	s.text:SetPoint("LEFT", s, "LEFT", 7, 0)
	s.text:SetPoint("RIGHT", s, "RIGHT", -24, 0)
	s.min = atlasButton(s, 16, 16, Style.ATLAS.buttonHighlight)
	s.min:SetPoint("RIGHT", s, "RIGHT", 1, 0)
	local function toggle()
		Tracker:ToggleSection(s.name)
	end
	s:SetScript("OnClick", ns.Guard("section-click", toggle))
	s.min:SetScript("OnClick", ns.Guard("section-click", toggle))
	self:WireDrag(s)
	self.sections[i] = s
	return s
end

function Tracker:GetBlock(i)
	local b = self.blocks[i]
	if b then
		return b
	end
	b = CreateFrame("Button", nil, self.frame)
	b:SetSize(WIDTH, TITLE_H)
	b.marker = b:CreateTexture(nil, "ARTWORK")
	b.marker:SetSize(MARKER, MARKER)
	b.marker:SetPoint("TOPLEFT", b, "TOPLEFT", 2, -1)
	b.elapsed = self:NewText(b, Style.font.line)
	b.elapsed:SetJustifyH("RIGHT")
	b.elapsed:SetPoint("TOPRIGHT", b, "TOPRIGHT", -2, -3)
	b.name = self:NewText(b, Style.font.line)
	b.name:SetPoint("TOPLEFT", b, "TOPLEFT", 22, -3)
	b.name:SetPoint("RIGHT", b.elapsed, "LEFT", -6, 0)
	b.lines = {}
	b:SetScript("OnClick", ns.Guard("block-click", function(self)
		if Tracker.lastDragStop and GetTime() - Tracker.lastDragStop < CLICK_AFTER_DRAG then
			return
		end
		if self.agentId and ns.Detail then
			ns.Detail:Open(self.agentId)
		end
	end))
	highlightable(b, function(self)
		self.hovering = true
		applyBlockColors(self)
		showTooltip(self)
		if self.agentId then
			startHoverTimer(self)
		end
	end, function(self)
		self.hovering = false
		self.hoverToken = (self.hoverToken or 0) + 1
		applyBlockColors(self)
		hideTooltip(self)
	end)
	self:WireDrag(b)
	self.blocks[i] = b
	return b
end

function Tracker:GetLine(block, i)
	local line = block.lines[i]
	if line then
		return line
	end
	line = {}
	line.dash = self:NewText(block, Style.font.line)
	line.dash:SetText("- ")
	line.text = self:NewText(block, Style.font.line)
	line.group = line.text:CreateAnimationGroup()
	local pulse = line.group:CreateAnimation("Alpha")
	pulse:SetFromAlpha(1)
	pulse:SetToAlpha(0.5)
	pulse:SetDuration(1.2)
	pulse:SetSmoothing("IN_OUT")
	line.group:SetLooping("BOUNCE")
	block.lines[i] = line
	return line
end

function Tracker:GetIdle(i)
	local r = self.idles[i]
	if r then
		return r
	end
	r = CreateFrame("Button", nil, self.frame)
	r:SetSize(WIDTH, TITLE_H)
	r.text = self:NewText(r, Style.font.line)
	r.text:SetPoint("TOPLEFT", r, "TOPLEFT", 22, -3)
	r.text:SetPoint("RIGHT", r, "RIGHT", -2, 0)
	r:SetScript("OnClick", ns.Guard("idle-click", function(self)
		if Tracker.lastDragStop and GetTime() - Tracker.lastDragStop < CLICK_AFTER_DRAG then
			return
		end
		Tracker:ToggleIdle()
	end))
	highlightable(r, function(self)
		Style.Color(self.text, Style.color.Normal)
	end, function(self)
		Style.Color(self.text, Style.color.Complete)
	end)
	self:WireDrag(r)
	self.idles[i] = r
	return r
end

-- Anything clickable hands drags to the tracker.
function Tracker:WireDrag(widget)
	widget:RegisterForDrag("LeftButton")
	widget:SetScript("OnDragStart", ns.Guard("drag-start", function()
		Tracker:DragStart()
	end))
	widget:SetScript("OnDragStop", ns.Guard("drag-stop", function()
		Tracker:DragStop()
	end))
end

------------------------------------------------------------------------
-- model: what to show, and what survives the height budget
------------------------------------------------------------------------

local function lineEntry(text, tone, pulse)
	return { text = ns.Truncate(text, LINE_CHARS), tone = tone, pulse = pulse }
end

-- Objective lines for an agent (UI-BRIEF "What each row shows"), as far as the
-- settings allow.
local function linesFor(a, kind)
	local opt = ns.db.lines
	local lines = {}
	local text = a.preview ~= "" and a.preview or a.last
	if kind == "needs" then
		if opt.preview and text ~= "" then
			lines[#lines + 1] = lineEntry(text)
		end
	elseif kind == "working" then
		if opt.activity then
			if a.activity ~= "" then
				lines[#lines + 1] = lineEntry(a.activity, nil, true)
			else
				-- no tool in flight: the model is writing
				lines[#lines + 1] = lineEntry("Thinking\226\128\166", "complete", true)
			end
		end
		if opt.todo and a.todo ~= "" then
			lines[#lines + 1] = lineEntry("Todo " .. a.todo)
		end
	elseif kind == "unseen" then
		if opt.stats and a.stats ~= "" then
			lines[#lines + 1] = lineEntry(a.stats, "complete")
		end
		if opt.preview and text ~= "" then
			lines[#lines + 1] = lineEntry(text)
		end
	elseif kind == "exited" then
		if opt.preview and text ~= "" then
			lines[#lines + 1] = lineEntry(text, "failed")
		end
	end
	while #lines > MAX_LINES do
		table.remove(lines)
	end
	return lines
end

local function blockHeight(lineCount)
	return TITLE_H + lineCount * LINE_H + BLOCK_PAD
end

-- Sections that have something active, in name order, each with its items in
-- the store's order (one flat list when grouping is off). Every idle, seen agent
-- goes into one list instead, grouped by section name. States the settings hide
-- are left out; a finished agent whose "not yet seen" state is hidden counts as idle.
function Tracker:BuildModel(now)
	local db, Store = ns.db, ns.Store
	local show, grouped = db.show, db.grouped
	local order, by = {}, {}
	local idleOrder, idleBy, idleCount = {}, {}, 0
	local seq = 0
	local flat = nil
	if not grouped then
		flat = { name = "", flat = true, items = {}, count = 0, collapsed = false }
		order[1] = flat
	end
	for i = 1, #Store.agents do
		local a = Store.agents[i]
		local name = sectionName(a)
		local kind = Store:Kind(a)
		if kind == "unseen" and not show.unseen then
			kind = "idle"
		end
		local shown = true
		if kind == "idle" then
			shown = false
			if show.idle then
				local gname = grouped and name or ""
				local g = idleBy[gname]
				if not g then
					g = { name = gname, agents = {}, items = {} }
					idleBy[gname] = g
					idleOrder[#idleOrder + 1] = g
				end
				g.agents[#g.agents + 1] = a
				idleCount = idleCount + 1
			end
		elseif kind == "other" then
			shown = true
		elseif not show[kind] then
			shown = false
		end
		if shown then
			local s = flat or by[name]
			if not s then
				s = { name = name, items = {}, count = 0, collapsed = db.collapsedSections[name] == true }
				by[name] = s
				order[#order + 1] = s
			end
			s.count = s.count + 1
			seq = seq + 1
			local lines = linesFor(a, kind)
			s.items[#s.items + 1] = {
				type = "agent", agent = a, kind = kind, lines = lines, section = s,
				height = blockHeight(#lines), priority = PRIORITY[kind] or PRIORITY.other, seq = seq,
			}
		end
	end
	local function byName(x, y)
		local lx, ly = x.name:lower(), y.name:lower()
		if lx ~= ly then
			return lx < ly
		end
		return x.name < y.name
	end
	if flat then
		table.sort(flat.items, function(x, y)
			if x.priority ~= y.priority then
				return x.priority < y.priority
			end
			return x.seq < y.seq
		end)
		if #flat.items == 0 then
			order = {}
		end
	else
		table.sort(order, byName)
	end
	table.sort(idleOrder, byName)

	local idle
	if idleCount > 0 then
		seq = seq + 1
		idle = {
			count = idleCount, open = db.idleOpen == true, groups = idleOrder,
			summary = { type = "idle", height = TITLE_H, priority = PRIORITY.idleSummary, seq = seq },
		}
		if idle.open then
			for _, g in ipairs(idleOrder) do
				for _, a in ipairs(g.agents) do
					seq = seq + 1
					g.items[#g.items + 1] = {
						type = "agent", agent = a, kind = "idle", lines = {}, group = g,
						height = TITLE_H + 2, priority = PRIORITY.idle, seq = seq,
					}
				end
			end
		end
	end
	return order, idle
end

-- Marks which items fit in the height limit, most important first, charging a
-- section header (or an idle sub-label) only once something under it is shown.
-- Returns the number of agents the budget left out.
local function fit(sections, idle, reserved)
	local limit = ns.db.maxHeight
	local used = HEADER_H + reserved
	local candidates = {}
	for _, s in ipairs(sections) do
		s.charged = s.flat == true -- a flat list has no header to pay for
		if s.collapsed then
			used = used + SECTION_H + SECTION_GAP -- shown, with nothing under it
			s.charged = true
		end
		for _, item in ipairs(s.items) do
			item.out = true -- until it is shown; a collapsed section shows none
			if not s.collapsed then
				candidates[#candidates + 1] = item
			end
		end
	end
	if idle then
		idle.summary.out = true
		candidates[#candidates + 1] = idle.summary
		for _, g in ipairs(idle.groups) do
			g.charged = g.name == "" -- no sub-label in a flat list
			for _, item in ipairs(g.items) do
				item.out = true
				candidates[#candidates + 1] = item
			end
		end
	end
	table.sort(candidates, function(x, y)
		if x.priority ~= y.priority then
			return x.priority < y.priority
		end
		return x.seq < y.seq
	end)
	local left = 0
	for _, item in ipairs(candidates) do
		local s, g = item.section, item.group
		local cost = item.height
		if s and not s.charged then
			cost = cost + SECTION_H + SECTION_GAP
		end
		if g and not g.charged then
			cost = cost + LINE_H
		end
		if g and idle.summary.out then
			-- the "N idle" line itself did not fit: those agents are counted there
		elseif used + cost <= limit then
			item.out = false
			used = used + cost
			if s then
				s.charged = true
			end
			if g then
				g.charged = true
			end
		elseif item.type == "idle" then
			left = left + idle.count
		else
			left = left + 1
		end
	end
	return left
end

------------------------------------------------------------------------
-- layout
------------------------------------------------------------------------

local function placeAt(widget, parent, y)
	widget:ClearAllPoints()
	widget:SetPoint("TOPLEFT", parent, "TOPLEFT", 0, -y)
end

function Tracker:RefreshHeader()
	local f, h = self.frame, self.header
	local db = ns.db
	local text, tone = ns.Link:StatusText()
	if text == "live" then
		h.status:SetText("")
		h.dot:Hide() -- restraint: nothing to say while the link is fine
	else
		local c = STATUS_TONE[tone] or STATUS_TONE.info
		h.status:SetText(text)
		h.dot:SetColorTexture(c[1], c[2], c[3], 1)
		h.dot:Show()
	end
	if db.collapsed then
		setFace(h.min, Style.ATLAS.expandAll, Style.ATLAS.expandAllPressed)
	else
		setFace(h.min, Style.ATLAS.collapseAll, Style.ATLAS.collapseAllPressed)
	end
	self.shown[#self.shown + 1] = { kind = "header", text = h.text, status = h.status, dot = h.dot, frame = h }
end

function Tracker:Refresh()
	local f = self.frame
	if not f then
		return
	end
	local db, Store = ns.db, ns.Store
	local now = time()
	self.shown = {}
	self:UpdateAnchor()
	self:RefreshHeader()

	local nSection, nBlock, nIdle, nLabel = 0, 0, 0, 0
	local y = HEADER_H
	local more = 0

	if not db.collapsed then
		local sections, idle = self:BuildModel(now)
		local linkMessage = ns.Link:Message()
		local emptyMessage
		if #Store.agents == 0 then
			if Store:IsStale() then
				emptyMessage = "ompcraft companion not running"
			else
				emptyMessage = "No agents. Start one in Tern."
			end
		end
		if #Store.agents > 0 and #sections == 0 and not idle then
			emptyMessage = "Nothing to show with your settings."
		end
		local reserved = (linkMessage and LINE_H or 0) + (emptyMessage and LINE_H or 0) + LINE_H
		more = fit(sections, idle, reserved)

		for _, s in ipairs(sections) do
			local anyShown = s.collapsed
			for _, item in ipairs(s.items) do
				if not item.out then
					anyShown = true
				end
			end
			if anyShown and s.flat then
				for _, item in ipairs(s.items) do
					if not item.out then
						nBlock = nBlock + 1
						y = y + self:ShowBlock(self:GetBlock(nBlock), item, y, now)
					end
				end
				y = y + SECTION_GAP - 4
			elseif anyShown then
				nSection = nSection + 1
				local sf = self:GetSection(nSection)
				sf.name = s.name
				placeAt(sf, f, y)
				sf:Show()
				sf.text:SetText(string.format("%s (%d)", ns.Truncate(s.name, 28), s.count))
				if s.collapsed then
					setFace(sf.min, Style.ATLAS.sectionExpand, Style.ATLAS.sectionExpandPressed)
				else
					setFace(sf.min, Style.ATLAS.sectionCollapse, Style.ATLAS.sectionCollapsePressed)
				end
				self.shown[#self.shown + 1] = { kind = "section", frame = sf, text = sf.text, collapsed = s.collapsed, name = s.name }
				y = y + SECTION_H + 4
				for _, item in ipairs(s.items) do
					if not item.out then
						nBlock = nBlock + 1
						y = y + self:ShowBlock(self:GetBlock(nBlock), item, y, now)
					end
				end
				y = y + SECTION_GAP - 4
			end
		end

		-- every idle agent, from every section, behind one dim line
		if idle and not idle.summary.out then
			nIdle = 1
			y = y + self:ShowIdle(self:GetIdle(1), idle, y)
			if idle.open then
				for _, g in ipairs(idle.groups) do
					local any = false
					for _, item in ipairs(g.items) do
						any = any or not item.out
					end
					if any then
						if g.name ~= "" then
							nLabel = nLabel + 1
							y = y + self:ShowLabel(self:GetLabel(nLabel), g.name, y)
						end
						for _, item in ipairs(g.items) do
							if not item.out then
								nBlock = nBlock + 1
								y = y + self:ShowBlock(self:GetBlock(nBlock), item, y, now)
							end
						end
					end
				end
			end
		end

		self:SetMessage(linkMessage, y)
		if linkMessage then
			y = y + LINE_H
		end
		self:SetEmpty(emptyMessage, y)
		if emptyMessage then
			y = y + LINE_H
		end
		if more > 0 then
			self.moreText:SetText(string.format("+%d more", more))
			self.moreText:ClearAllPoints()
			self.moreText:SetPoint("TOPLEFT", f, "TOPLEFT", 22, -y)
			self.moreText:Show()
			self.shown[#self.shown + 1] = { kind = "more", text = self.moreText }
			y = y + LINE_H
		else
			self.moreText:Hide()
		end
	else
		self:SetMessage(nil, 0)
		self:SetEmpty(nil, 0)
		self.moreText:Hide()
	end

	for i = nSection + 1, #self.sections do
		self.sections[i]:Hide()
	end
	for i = nBlock + 1, #self.blocks do
		self:HideBlock(self.blocks[i])
	end
	for i = nIdle + 1, #self.idles do
		self.idles[i]:Hide()
	end
	for i = nLabel + 1, #self.labels do
		self.labels[i]:Hide()
	end
	f:SetHeight(y)
end

function Tracker:SetMessage(text, y)
	local fs = self.message
	if text then
		fs:SetText(text)
		fs:ClearAllPoints()
		fs:SetPoint("TOPLEFT", self.frame, "TOPLEFT", 22, -y)
		Style.Color(fs, Style.color.Failed)
		fs:Show()
		self.shown[#self.shown + 1] = { kind = "message", text = fs }
	else
		fs:Hide()
	end
end

function Tracker:SetEmpty(text, y)
	local fs = self.empty
	if text then
		fs:SetText(text)
		fs:ClearAllPoints()
		fs:SetPoint("TOPLEFT", self.frame, "TOPLEFT", 22, -y)
		Style.Color(fs, Style.color.Complete)
		fs:Show()
		self.shown[#self.shown + 1] = { kind = "empty", text = fs }
	else
		fs:Hide()
	end
end

function Tracker:HideBlock(block)
	for i = 1, #block.lines do
		block.lines[i].used = false
		block.lines[i].group:Stop()
		block.lines[i].dash:Hide()
		block.lines[i].text:Hide()
	end
	block.agent, block.agentId = nil, nil
	block.hovering = false
	block:Hide()
end

-- Fills one agent block; returns its height.
function Tracker:ShowBlock(block, item, y, now)
	local a, kind = item.agent, item.kind
	block.agent, block.agentId, block.kind = a, a.id, kind
	placeAt(block, self.frame, y)
	block:SetHeight(item.height)
	block:Show()

	Style.SetMarker(block.marker, kind, MARKER)
	block.name:SetText(ns.Truncate(a.name ~= "" and a.name or "?", NAME_CHARS))
	local elapsed = ""
	if kind == "needs" or kind == "working" then
		elapsed = elapsedOf(a, now)
	end
	block.elapsed:SetText(elapsed)

	local shownLines = {}
	for i = 1, MAX_LINES do
		local line = self:GetLine(block, i)
		local entry = item.lines[i]
		if entry then
			line.used, line.tone = true, entry.tone
			local ly = TITLE_H + (i - 1) * LINE_H
			line.dash:ClearAllPoints()
			line.dash:SetPoint("TOPLEFT", block, "TOPLEFT", 22, -ly)
			line.text:ClearAllPoints()
			line.text:SetPoint("TOPLEFT", block, "TOPLEFT", 32, -ly)
			line.text:SetPoint("RIGHT", block, "RIGHT", -2, 0)
			line.text:SetText(entry.text)
			line.dash:Show()
			line.text:Show()
			if entry.pulse then
				if not line.group:IsPlaying() then
					line.group:Play()
				end
			else
				line.group:Stop()
				line.text:SetAlpha(1)
			end
			shownLines[#shownLines + 1] = line
		else
			line.used = false
			line.group:Stop()
			line.text:SetAlpha(1)
			line.dash:Hide()
			line.text:Hide()
		end
	end
	applyBlockColors(block)
	self.shown[#self.shown + 1] = {
		kind = "agent", frame = block, agent = a, state = kind, title = block.name, elapsed = block.elapsed,
		marker = block.marker, lines = shownLines,
	}
	return item.height
end

function Tracker:ShowIdle(row, idle, y)
	placeAt(row, self.frame, y)
	row:Show()
	row.text:SetText(string.format("%d idle", idle.count))
	Style.Color(row.text, Style.color.Complete)
	self.shown[#self.shown + 1] = { kind = "idle", frame = row, text = row.text, open = idle.open }
	return idle.summary.height
end

-- A small dim sub-label naming the section of the idle agents under it.
function Tracker:GetLabel(i)
	local fs = self.labels[i]
	if not fs then
		fs = self:NewText(self.frame, Style.font.line)
		self.labels[i] = fs
	end
	return fs
end

function Tracker:ShowLabel(fs, name, y)
	fs:SetText(ns.Truncate(name, 34))
	Style.Color(fs, Style.color.Complete)
	fs:ClearAllPoints()
	fs:SetPoint("TOPLEFT", self.frame, "TOPLEFT", 22, -y - 2)
	fs:Show()
	self.shown[#self.shown + 1] = { kind = "label", text = fs }
	return LINE_H
end

------------------------------------------------------------------------
-- state changes
------------------------------------------------------------------------

function Tracker:ToggleSection(name)
	local c = ns.db.collapsedSections
	c[name] = (not c[name]) and true or nil
	self:Refresh()
end

function Tracker:ToggleIdle()
	ns.db.idleOpen = not ns.db.idleOpen
	self:Refresh()
end

function Tracker:SetCollapsed(collapsed)
	ns.db.collapsed = collapsed and true or false
	self:Refresh()
end

function Tracker:IsOpen()
	return self.frame ~= nil and self.frame:IsShown()
end

local HIDDEN_INSTANCES = { party = true, raid = true, pvp = true, arena = true }

local function inHiddenInstance()
	local inInstance, kind = IsInInstance()
	return inInstance and HIDDEN_INSTANCES[kind] == true
end

-- Something needs the player or is working.
function Tracker:HasActive()
	local c = ns.Store:Counts()
	return c.needs > 0 or c.working > 0
end

-- Whether the tracker should be on screen now: not turned off, and none of the
-- "hide" rules from the settings applies.
function Tracker:ShouldShow()
	local db = ns.db
	if not db.shown then
		return false
	end
	if db.hideInCombat and InCombatLockdown() then
		return false
	end
	if db.hideInInstance and inHiddenInstance() then
		return false
	end
	if db.onlyWhenActive and not self.forceShow and not self:HasActive() then
		return false
	end
	return true
end

-- Shows or hides the (insecure) frame to match ShouldShow.
function Tracker:ApplyVisibility()
	if not self.frame then
		return
	end
	self:UpdateAnchor() -- so a hidden tracker still knows where it sits
	if self.forceShow and self:HasActive() then
		self.forceShow = false -- it is active now; the rule takes over again
	end
	if self:ShouldShow() then
		self.frame:Show()
	else
		self.frame:Hide()
	end
end

-- A setting changed: show or hide, and draw again.
function Tracker:ConfigChanged()
	if not self.frame then
		return
	end
	self:ApplyVisibility()
	if self:IsOpen() then
		self:Refresh()
	end
end

function Tracker:SetOpen(open)
	ns.db.shown = open and true or false
	-- an explicit "show" wins over "only when something is active"
	self.forceShow = open and ns.db.onlyWhenActive and not self:HasActive() or false
	self:ApplyVisibility()
end

function Tracker:Toggle()
	self:SetOpen(not self:IsOpen())
end

function Tracker:SetLocked(locked)
	ns.db.locked = locked and true or false
	ns.Config:Notify("locked")
end

------------------------------------------------------------------------
-- position, scale, dragging
------------------------------------------------------------------------

-- Under Blizzard's tracker when it is on screen (and that is the chosen
-- placement), else top right. Only an anchor to its backdrop: we are never a
-- child of it.
function Tracker:UpdateAnchor()
	local f, db = self.frame, ns.db
	local mode, target
	if db.pos then
		mode = "saved"
	elseif db.anchor == "corner" then
		mode = "fallback"
	elseif db.anchor == "topleft" then
		mode = "topleft"
	else
		local otf = rawget(_G, "ObjectiveTrackerFrame")
		local nine = otf and otf.NineSlice
		-- Blizzard's tracker only counts when it is shown and its geometry reads as a plain
		-- number (never a secret or an error); anything else falls back to the fixed anchor.
		local shown = otf and pcall(otf.IsShown, otf) and otf:IsShown()
		if shown and nine and ns.PlainNumber(nine.GetBottom, nine) then
			mode, target = "tracker", nine
		else
			mode = "fallback"
		end
	end
	if mode == self.anchorMode and target == self.anchorTarget then
		return
	end
	self.anchorMode, self.anchorTarget = mode, target
	f:ClearAllPoints()
	if mode == "saved" then
		local p = db.pos
		f:SetPoint(p.point, UIParent, p.relPoint, p.x, p.y)
	elseif mode == "tracker" then
		f:SetPoint("TOPRIGHT", target, "BOTTOMRIGHT", -5, -ANCHOR_GAP)
	elseif mode == "topleft" then
		f:SetPoint("TOPLEFT", UIParent, "TOPLEFT", TOPLEFT_X, TOPLEFT_Y)
	else
		f:SetPoint("TOPRIGHT", UIParent, "TOPRIGHT", FALLBACK_X, FALLBACK_Y)
	end
end

-- After a drag: the point the engine left the frame on.
function Tracker:SavePosition()
	local point, _, relPoint, x, y = self.frame:GetPoint(1)
	if point then
		ns.db.pos = { point = point, relPoint = relPoint or point, x = x or 0, y = y or 0 }
		self.anchorMode = "saved"
	end
end

-- Pins the tracker where it is now (it may be anchored to Blizzard's tracker).
-- Offsets from the screen's bottom left corner are the frame's own left and top.
function Tracker:CapturePosition()
	local f = self.frame
	local left = ns.PlainNumber(f.GetLeft, f)
	local top = ns.PlainNumber(f.GetTop, f)
	if left and top then
		ns.db.pos = { point = "TOPLEFT", relPoint = "BOTTOMLEFT", x = left, y = top }
	else
		ns.db.pos = { point = "TOPRIGHT", relPoint = "TOPRIGHT", x = FALLBACK_X, y = FALLBACK_Y }
	end
	self.anchorMode = nil
end

function Tracker:ResetPosition()
	ns.db.pos = nil
	self.anchorMode = nil
	self:UpdateAnchor()
	ns.Config:Notify("position")
end

-- "topleft", "tracker" (under quest objectives), "corner" (top right) or "free" (dragged).
function Tracker:PositionMode()
	if ns.db.pos then
		return "free"
	end
	return ns.db.anchor
end

-- Choosing "free" unlocks; either of the others forgets the dragged position.
function Tracker:SetPositionMode(mode)
	local db = ns.db
	if mode == "free" then
		if not db.pos then
			self:CapturePosition()
		end
		db.locked = false
		ns.Config:Notify("locked")
	else
		db.anchor = (mode == "corner" or mode == "tracker") and mode or "topleft"
		db.pos = nil
	end
	self.anchorMode = nil
	self:UpdateAnchor()
end

-- Puts db.scale on the frame. Saved offsets are in the frame's own scale, so they
-- are rescaled to keep the tracker where it is on screen.
function Tracker:ApplyScale()
	local db = ns.db
	local scale = math.max(self.MIN_SCALE, math.min(self.MAX_SCALE, db.scale))
	local old = self.frameScale or scale
	if db.pos and old ~= scale then
		db.pos.x = db.pos.x * old / scale
		db.pos.y = db.pos.y * old / scale
	end
	db.scale = scale
	self.frame:SetScale(scale)
	self.frameScale = scale
	self.anchorMode = nil
	self:UpdateAnchor()
end

function Tracker:SetScale(scale)
	ns.db.scale = math.max(self.MIN_SCALE, math.min(self.MAX_SCALE, scale))
	self:ApplyScale()
	ns.Config:Notify("scale")
end

function Tracker:DragStart()
	if not ns.db.locked then
		self.frame:StartMoving()
		self.dragging = true
	end
end

function Tracker:DragStop()
	if self.dragging then
		self.dragging = false
		self.lastDragStop = GetTime()
		self.frame:StopMovingOrSizing()
		self:SavePosition()
	end
end

------------------------------------------------------------------------
-- creation
------------------------------------------------------------------------

-- Right-click on the "Agents" header: the few things worth a menu. Menu and
-- MenuUtil are Blizzard's own; the entries only call our functions.
function Tracker:OpenMenu(owner)
	local MenuUtil = rawget(_G, "MenuUtil")
	if not (MenuUtil and MenuUtil.CreateContextMenu) then
		return false
	end
	MenuUtil.CreateContextMenu(owner, function(_, root)
		root:CreateTitle("Ompcraft")
		root:CreateCheckbox("Locked", function()
			return ns.db.locked
		end, function()
			Tracker:SetLocked(not ns.db.locked)
		end)
		root:CreateCheckbox("Show idle agents", function()
			return ns.db.idleOpen
		end, function()
			Tracker:ToggleIdle()
		end)
		root:CreateDivider()
		root:CreateButton("Settings...", function()
			ns.Config:Open()
		end)
	end)
	return true
end


function Tracker:Create()
	Style.Resolve()
	local f = CreateFrame("Frame", "OmpcraftTracker", UIParent)
	self.frame = f
	f:SetSize(WIDTH, HEADER_H)
	f:SetFrameStrata("HIGH")
	f:SetClampedToScreen(true)
	f:SetMovable(true)
	f:SetDontSavePosition(true)

	-- "Agents" header, in the look of the tracker's own container header.
	local h = CreateFrame("Button", nil, f)
	self.header = h
	h:SetSize(WIDTH, HEADER_H)
	h:SetPoint("TOPLEFT", f, "TOPLEFT", 0, 0)
	h.bg = h:CreateTexture(nil, "BACKGROUND")
	h.bg:SetAtlas(Style.ATLAS.trackerHeader, true)
	h.bg:SetPoint("CENTER", h, "CENTER", 0, 0)
	h.text = self:NewText(h, Style.font.header)
	h.text:SetPoint("LEFT", h, "LEFT", 7, 0)
	h.text:SetText("Agents")
	h.min = atlasButton(h, 18, 19, Style.ATLAS.buttonHighlight)
	h.min:SetPoint("RIGHT", h, "RIGHT", -1, 0)
	h.status = self:NewText(h, Style.font.line)
	h.status:SetJustifyH("RIGHT")
	h.status:SetPoint("RIGHT", h.min, "LEFT", -4, 0)
	Style.Color(h.status, Style.color.Complete)
	h.dot = h:CreateTexture(nil, "OVERLAY")
	h.dot:SetSize(7, 7)
	h.dot:SetPoint("RIGHT", h.status, "LEFT", -4, 0)
	h.dot:Hide()
	local function toggle()
		Tracker:SetCollapsed(not ns.db.collapsed)
	end
	h:RegisterForClicks("LeftButtonUp", "RightButtonUp")
	h:SetScript("OnClick", ns.Guard("header-click", function(self, button)
		if button == "RightButton" then
			Tracker:OpenMenu(self)
		else
			toggle()
		end
	end))
	h.min:SetScript("OnClick", ns.Guard("header-click", toggle))
	self:WireDrag(h)

	self.message = self:NewText(f, Style.font.line)
	self.message:Hide()
	self.empty = self:NewText(f, Style.font.line)
	self.empty:Hide()
	self.moreText = self:NewText(f, Style.font.line)
	Style.Color(self.moreText, Style.color.Complete)
	self.moreText:Hide()

	-- Elapsed times only tick while the tracker is on screen.
	f:SetScript("OnShow", ns.Guard("tracker-show", function()
		ns.Link:ResetBackoff() -- opening it asks for fresh data soon
		Tracker:Refresh()
		if not Tracker.ticker then
			Tracker.ticker = C_Timer.NewTicker(1, ns.Guard("tracker-tick", function()
				Tracker:Refresh()
			end))
		end
	end))
	f:SetScript("OnHide", ns.Guard("tracker-hide", function()
		if Tracker.ticker then
			Tracker.ticker:Cancel()
			Tracker.ticker = nil
		end
		for i = 1, #Tracker.blocks do
			for _, line in ipairs(Tracker.blocks[i].lines) do
				line.group:Stop()
			end
		end
	end))
	f:Hide() -- OnShow (refresh + ticker) must fire when LOADED shows it
	return f
end

ns:Listen("LOADED", function()
	Tracker:Create()
	Tracker.frame:SetScale(ns.db.scale)
	Tracker.frameScale = ns.db.scale
	Tracker:ApplyVisibility()
end)

ns:Listen("SNAPSHOT", function()
	Tracker:ApplyVisibility()
	if Tracker:IsOpen() then
		Tracker:Refresh()
	end
end)

-- The hide rules: combat, and dungeons, raids and PvP.
for _, event in ipairs({ "PLAYER_REGEN_DISABLED", "PLAYER_REGEN_ENABLED", "PLAYER_ENTERING_WORLD", "ZONE_CHANGED_NEW_AREA" }) do
	ns:On(event, function()
		Tracker:ApplyVisibility()
	end)
end

ns:Listen("LINK", function()
	if Tracker:IsOpen() then
		Tracker:Refresh()
	end
end)
