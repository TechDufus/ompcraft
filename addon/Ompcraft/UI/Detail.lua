-- Ompcraft: the agent detail page, in the style of the quest log. A ButtonFrame
-- window with parchment inside, the agent's last reply rendered from
-- detail.lines, its recent tool steps, and a footer. It closes with Esc.

local _, ns = ...

local Detail = {}
ns.Detail = Detail

local Style = ns.Style

local FRAME_W, FRAME_H = 420, 560 -- default size; a resize is remembered
local MIN_W, MIN_H, MAX_W, MAX_H = 340, 360, 760, 900
local CONTENT_INSET = 58 -- frame width minus the scrolling content's width
local CONTENT_W = FRAME_W - CONTENT_INSET -- width of the scrolling content
local HEADER_H = 40 -- the fixed band above the scroll area
local SIZER_UP = "Interface\\ChatFrame\\UI-ChatIM-SizeGrabber-Up" -- FloatingChatFrame.xml
local SIZER_DOWN = "Interface\\ChatFrame\\UI-ChatIM-SizeGrabber-Down"
local SIZER_HIGHLIGHT = "Interface\\ChatFrame\\UI-ChatIM-SizeGrabber-Highlight"
local MARKER = 16
local BULLET = "\226\128\162" -- "•"
local SUB_BULLET = "\226\128\147" -- "–"
local SEP = " \194\183 " -- " · "
local NOTE = "Full reply appears while this agent is active or recently finished."

Detail.WIDTH = FRAME_W

Detail.pool = { fs = {}, tex = {} }

------------------------------------------------------------------------
-- pools
------------------------------------------------------------------------

function Detail:ResetPools()
	self.pool.fsUsed, self.pool.texUsed = 0, 0
end

function Detail:FS(font)
	local p = self.pool
	p.fsUsed = p.fsUsed + 1
	local fs = p.fs[p.fsUsed]
	if not fs then
		fs = self.content:CreateFontString(nil, "ARTWORK", Style.font.body)
		fs:SetJustifyH("LEFT")
		fs:SetJustifyV("TOP")
		fs:SetWordWrap(true)
		p.fs[p.fsUsed] = fs
	end
	fs:SetFontObject(font)
	fs:SetWordWrap(true) -- pooled: code runs turn it off
	fs:SetAlpha(1)
	fs:ClearAllPoints()
	fs:Show()
	return fs
end

function Detail:Tex(layer)
	local p = self.pool
	p.texUsed = p.texUsed + 1
	local tex = p.tex[p.texUsed]
	if not tex then
		tex = self.content:CreateTexture(nil, layer or "ARTWORK")
		p.tex[p.texUsed] = tex
	end
	tex:SetDesaturated(false)
	tex:SetVertexColor(1, 1, 1)
	tex:ClearAllPoints()
	tex:Show()
	return tex
end

function Detail:HideUnused()
	local p = self.pool
	for i = p.fsUsed + 1, #p.fs do
		p.fs[i]:Hide()
		p.fs[i]:SetText("")
	end
	for i = p.texUsed + 1, #p.tex do
		p.tex[i]:Hide()
	end
end

------------------------------------------------------------------------
-- drawing primitives; each returns the height it used
------------------------------------------------------------------------

-- Wrapped text at (x, y), `width` wide.
function Detail:Text(text, font, color, x, y, width)
	local fs = self:FS(font)
	Style.Color(fs, color)
	fs:SetPoint("TOPLEFT", self.content, "TOPLEFT", x, -y)
	fs:SetWidth(width)
	fs:SetText(text)
	return fs, fs:GetStringHeight()
end

function Detail:Rule(y)
	local paper = Style.paper
	local t = self:Tex("ARTWORK")
	t:SetPoint("TOPLEFT", self.content, "TOPLEFT", 0, -(y + 4))
	t:SetSize(CONTENT_W, 1)
	t:SetColorTexture(paper.rule[1], paper.rule[2], paper.rule[3], paper.rule[4])
	return 10
end

local function inline(text)
	local p = Style.paper
	return ns.InlineMarkup(text, p.codeHex, p.boldHex)
end

local TABLE_INDENT = 8 -- px a table row is indented
local CODE_MAX_LINES = 12 -- lines of one code block shown
local CODE_MAX_CHARS = 110 -- characters of a code line shown
local ELLIPSIS = "\226\128\166" -- "…"
local DIM_HEX = "ff8f9aa3"

-- One code line, cut hard (code does not wrap meaningfully). The text is
-- already pipe-safe, and Truncate never splits a "||" pair.
local function codeLine(text)
	local cut = ns.Truncate(text, CODE_MAX_CHARS)
	if cut ~= text then
		cut = cut:sub(1, -4) .. ELLIPSIS
	end
	return cut
end

-- A run of consecutive code lines on one subtle dark band: at most
-- CODE_MAX_LINES lines, then one dim line saying how many were left out. Each
-- line is its own non-wrapping FontString (a non-wrapping FontString shows one
-- line however many "\n" its text has), stacked inside the band.
function Detail:CodeRun(lines, y)
	local paper = Style.paper
	local band = self:Tex("BACKGROUND")
	local shown = {}
	for i = 1, math.min(#lines, CODE_MAX_LINES) do
		shown[i] = codeLine(lines[i])
	end
	if #lines > CODE_MAX_LINES then
		shown[#shown + 1] = string.format("|c%s%s %d more lines (open in Tern)|r", DIM_HEX, ELLIPSIS, #lines - CODE_MAX_LINES)
	end
	local rowY, rowH = y + 4, nil
	for _, text in ipairs(shown) do
		local fs = self:FS(Style.font.codeLine)
		fs:SetWordWrap(false)
		Style.Color(fs, paper.codeText)
		fs:SetPoint("TOPLEFT", self.content, "TOPLEFT", 8, -rowY)
		fs:SetWidth(CONTENT_W - 16)
		fs:SetText(text ~= "" and text or " ") -- a blank code line still takes a row
		rowH = rowH or fs:GetStringHeight() -- one font, so every row is as tall as the first
		rowY = rowY + rowH
	end
	local h = rowY - y + 4
	band:SetPoint("TOPLEFT", self.content, "TOPLEFT", 0, -y)
	band:SetSize(CONTENT_W, h)
	band:SetColorTexture(paper.codeBand[1], paper.codeBand[2], paper.codeBand[3], paper.codeBand[4])
	return h + 6
end

-- Renders detail.lines; returns the new y.
function Detail:RenderLines(lines, y)
	local paper, font = Style.paper, Style.font
	local i, n = 1, #lines
	local first = true
	local trRow = 0 -- position in a run of consecutive table rows
	while i <= n do
		local l = lines[i]
		if l.k ~= "tr" then
			trRow = 0
		end
		if l.k == "code" then
			local run = {}
			while i <= n and lines[i].k == "code" do
				run[#run + 1] = lines[i].t
				i = i + 1
			end
			y = y + self:CodeRun(run, y)
		else
			if l.k == "h" then
				if not first then
					y = y + 4
				end
				local _, h = self:Text(inline(l.t), font.heading, paper.title, 0, y, CONTENT_W)
				y = y + h + 4
			elseif l.k == "p" then
				local _, h = self:Text(inline(l.t), font.body, paper.text, 0, y, CONTENT_W)
				y = y + h + 4
			elseif l.k == "li" then
				local indent = 6 + (l.d - 1) * 14
				local bullet = self:FS(font.body)
				Style.Color(bullet, paper.title)
				bullet:SetPoint("TOPLEFT", self.content, "TOPLEFT", indent, -y)
				bullet:SetWidth(12)
				bullet:SetText(l.d > 1 and SUB_BULLET or BULLET)
				local _, h = self:Text(inline(l.t), font.body, paper.text, indent + 14, y, CONTENT_W - indent - 14)
				y = y + h + 2
			elseif l.k == "tr" then
				-- a table row: body font, one line, cut like code; every second
				-- row of a run sits on a very faint band
				trRow = trRow + 1
				local fs = self:FS(font.body)
				fs:SetWordWrap(false)
				Style.Color(fs, paper.text)
				fs:SetPoint("TOPLEFT", self.content, "TOPLEFT", TABLE_INDENT, -y)
				fs:SetWidth(CONTENT_W - TABLE_INDENT)
				fs:SetText(inline(codeLine(l.t)))
				local h = fs:GetStringHeight()
				if trRow % 2 == 0 then
					local band = self:Tex("BACKGROUND")
					band:SetPoint("TOPLEFT", self.content, "TOPLEFT", 0, -(y - 1))
					band:SetSize(CONTENT_W, h + 2)
					band:SetColorTexture(paper.rowBand[1], paper.rowBand[2], paper.rowBand[3], paper.rowBand[4])
				end
				y = y + h + 2
			elseif l.k == "quote" then
				local fs, h = self:Text(inline(l.t), font.body, paper.dim, 12, y, CONTENT_W - 12)
				local bar = self:Tex("ARTWORK")
				bar:SetPoint("TOPLEFT", self.content, "TOPLEFT", 2, -y)
				bar:SetSize(2, h)
				bar:SetColorTexture(paper.dim[1], paper.dim[2], paper.dim[3], 0.8)
				y = y + h + 4
			elseif l.k == "hr" then
				y = y + self:Rule(y)
			elseif l.k == "gap" then
				y = y + 6
			elseif l.k == "more" then
				local _, h = self:Text(l.t, font.dim, paper.dim, 0, y, CONTENT_W)
				y = y + h + 4
			end
			i = i + 1
		end
		first = false
	end
	return y
end

function Detail:RenderSteps(steps, y)
	local paper, font = Style.paper, Style.font
	local _, h = self:Text("Recent steps", font.heading, paper.title, 0, y, CONTENT_W)
	y = y + h + 4
	for _, step in ipairs(steps) do
		local icon = self:Tex("ARTWORK")
		icon:SetAtlas(Style.StepAtlas(step.s), false)
		icon:SetSize(14, 14)
		icon:SetPoint("TOPLEFT", self.content, "TOPLEFT", 2, -(y + 1))
		local text = step.n
		if step.t ~= "" then
			text = (text ~= "" and (text .. SEP) or "") .. step.t
		end
		local color = paper.text
		if step.s == "error" or step.s == "failed" then
			color = { 0.55, 0.05, 0.05 }
		end
		local _, sh = self:Text(text, font.body, color, 22, y, CONTENT_W - 22)
		y = y + math.max(sh, 14) + 3
	end
	return y
end

------------------------------------------------------------------------
-- the page
------------------------------------------------------------------------

local function stateLine(a, kind, now)
	local label
	if kind == "needs" then
		label = "Needs you"
	elseif kind == "working" then
		label = "Working"
	elseif kind == "unseen" or (a.state == "idle" and a.doneAt > 0) then
		label = "Finished"
	elseif a.state == "exited" then
		label = "Exited"
	else
		label = "Idle"
	end
	local since = a.since
	if (kind == "needs" or kind == "working") and since and since > 0 then
		label = label .. SEP .. ns.FormatElapsed(now - since)
	elseif a.state == "idle" and a.doneAt > 0 then
		label = label .. SEP .. ns.FormatElapsed(now - a.doneAt) .. " ago"
	end
	return label
end

function Detail:Render()
	local f = self.frame
	local Store = ns.Store
	local a = Store:Find(self.agentId)
	local paper, font = Style.paper, Style.font
	local now = time()
	self:ResetPools()
	local y = 0

	if not a then
		f:SetTitle("Agent")
		self.marker:Hide()
		self.stateText:SetText("")
		self:Text("This agent is no longer listed.", font.body, paper.dim, 0, 0, CONTENT_W)
		self:HideUnused()
		self.content:SetHeight(40)
		return
	end

	f:SetTitle(ns.Truncate(a.name ~= "" and a.name or "?", 34))
	local kind = Store:Kind(a)
	Style.SetMarker(self.marker, kind, MARKER)
	self.stateText:SetText(stateLine(a, kind, now))
	Style.Color(self.stateText, paper.title)

	-- the reply
	local detail = a.detail
	if detail and #detail.lines > 0 then
		y = self:RenderLines(detail.lines, y)
	else
		local text = a.preview ~= "" and a.preview or a.last
		if text ~= "" then
			local _, h = self:Text(text, font.body, paper.text, 0, y, CONTENT_W)
			y = y + h + 6
		end
		local _, h = self:Text(NOTE, font.dim, paper.dim, 0, y, CONTENT_W)
		y = y + h + 6
	end

	if detail and #detail.steps > 0 then
		y = y + 4
		y = y + self:Rule(y)
		y = self:RenderSteps(detail.steps, y)
	end

	-- footer: model, host, session, cwd; then the stats
	y = y + 4
	y = y + self:Rule(y)
	local parts, seenPart = {}, {}
	for _, v in ipairs({ a.model, a.host, a.session, a.cwd }) do
		-- a session named after the directory would say the same thing twice
		if v ~= "" and not seenPart[v:lower()] then
			seenPart[v:lower()] = true
			parts[#parts + 1] = v
		end
	end
	if #parts > 0 then
		local _, h = self:Text(table.concat(parts, SEP), font.dim, paper.dim, 0, y, CONTENT_W)
		y = y + h + 2
	end
	if a.stats ~= "" then
		local _, h = self:Text(a.stats, font.dim, paper.dim, 0, y, CONTENT_W)
		y = y + h + 2
	end

	self:HideUnused()
	self.content:SetHeight(y + 8)
end

function Detail:Create()
	local f = CreateFrame("Frame", "OmpcraftDetail", UIParent, "ButtonFrameTemplate")
	self.frame = f
	local size = ns.db.detailSize
	local width, height = size and size.w or FRAME_W, size and size.h or FRAME_H
	f:SetSize(width, height)
	CONTENT_W = width - CONTENT_INSET
	f:SetPoint("CENTER", UIParent, "CENTER", -200, 0)
	f:SetFrameStrata("HIGH")
	f:SetToplevel(true)
	f:SetClampedToScreen(true)
	f:SetMovable(true)
	f:SetResizable(true)
	f:SetResizeBounds(MIN_W, MIN_H, MAX_W, MAX_H)
	f:EnableMouse(true)
	f:RegisterForDrag("LeftButton")
	f:SetScript("OnDragStart", ns.Guard("detail-drag", function(self)
		self:StartMoving()
	end))
	f:SetScript("OnDragStop", ns.Guard("detail-drag", function(self)
		self:StopMovingOrSizing()
	end))
	ButtonFrameTemplate_HidePortrait(f)
	ButtonFrameTemplate_HideAttic(f) -- no empty band between the title bar and the page
	ButtonFrameTemplate_HideButtonBar(f)

	-- Parchment inside the inset, on a child frame so it draws above it.
	local inset = f.Inset or f
	local paper = CreateFrame("Frame", nil, f)
	self.paper = paper
	paper:SetPoint("TOPLEFT", inset, "TOPLEFT", 3, -3)
	paper:SetPoint("BOTTOMRIGHT", inset, "BOTTOMRIGHT", -3, 3)
	paper:SetFrameLevel(inset:GetFrameLevel() + 1)
	paper.bg = paper:CreateTexture(nil, "BACKGROUND")
	paper.bg:SetAtlas(Style.ATLAS.parchment, false)
	paper.bg:SetAllPoints(paper)

	-- The fixed header band: marker and state line in the plain quest font (the
	-- decorative heading font makes "59s" read as "598"), then a rule. The
	-- scroll area starts below it, so nothing scrolls under the header.
	self.marker = paper:CreateTexture(nil, "ARTWORK")
	self.marker:SetPoint("TOPLEFT", paper, "TOPLEFT", 10, -10)
	self.marker:SetSize(MARKER, MARKER)
	self.stateText = paper:CreateFontString(nil, "ARTWORK", Style.font.body)
	self.stateText:SetPoint("LEFT", self.marker, "RIGHT", 6, 0)
	self.stateText:SetPoint("RIGHT", paper, "RIGHT", -10, 0)
	self.stateText:SetJustifyH("LEFT")
	self.stateText:SetWordWrap(false)
	local rule = Style.paper.rule
	self.headerRule = paper:CreateTexture(nil, "BORDER")
	self.headerRule:SetPoint("TOPLEFT", paper, "TOPLEFT", 10, -(HEADER_H - 4))
	self.headerRule:SetPoint("TOPRIGHT", paper, "TOPRIGHT", -10, -(HEADER_H - 4))
	self.headerRule:SetHeight(1)
	self.headerRule:SetColorTexture(rule[1], rule[2], rule[3], rule[4])

	local scroll = CreateFrame("ScrollFrame", "OmpcraftDetailScroll", paper, "ScrollFrameTemplate")
	self.scroll = scroll
	scroll:SetPoint("TOPLEFT", paper, "TOPLEFT", 10, -HEADER_H)
	scroll:SetPoint("BOTTOMRIGHT", paper, "BOTTOMRIGHT", -26, 8)
	local content = CreateFrame("Frame", nil, scroll)
	self.content = content
	content:SetSize(CONTENT_W, 10)
	scroll:SetScrollChild(content)

	-- A resize grip (the chat frame's own), bottom right.
	local grip = CreateFrame("Button", nil, f)
	self.grip = grip
	grip:SetSize(16, 16)
	grip:SetPoint("BOTTOMRIGHT", f, "BOTTOMRIGHT", -4, 4)
	grip:SetFrameLevel(paper:GetFrameLevel() + 10)
	grip.face = grip:CreateTexture(nil, "ARTWORK")
	grip.face:SetAllPoints(grip)
	grip.face:SetTexture(SIZER_UP)
	grip.glow = grip:CreateTexture(nil, "HIGHLIGHT")
	grip.glow:SetAllPoints(grip)
	grip.glow:SetTexture(SIZER_HIGHLIGHT)
	grip:SetScript("OnMouseDown", ns.Guard("detail-size", function()
		f:StartSizing("BOTTOMRIGHT")
		grip.face:SetTexture(SIZER_DOWN)
	end))
	grip:SetScript("OnMouseUp", ns.Guard("detail-size", function()
		f:StopMovingOrSizing()
		grip.face:SetTexture(SIZER_UP)
		Detail:SizeChanged(true)
	end))

	table.insert(UISpecialFrames, "OmpcraftDetail") -- Esc closes it
	f:SetScript("OnShow", ns.Guard("detail-show", function()
		Detail:Refresh()
	end))
	f:Hide()
	return f
end

-- The window was resized: lay the text out for the new width, and remember the
-- size. (Called when the grip is released.)
function Detail:SizeChanged(save)
	local f = self.frame
	local w, h = ns.PlainNumber(f.GetWidth, f), ns.PlainNumber(f.GetHeight, f)
	if not (w and h) then
		return
	end
	w = math.max(MIN_W, math.min(MAX_W, math.floor(w + 0.5)))
	h = math.max(MIN_H, math.min(MAX_H, math.floor(h + 0.5)))
	if save then
		ns.db.detailSize = { w = w, h = h }
	end
	CONTENT_W = w - CONTENT_INSET
	self.content:SetWidth(CONTENT_W)
	self:Refresh()
end

-- Opens the page for an agent and marks its current turn as seen.
function Detail:Open(id)
	if not self.frame then
		self:Create()
	end
	self.agentId = id
	self.scroll:SetVerticalScroll(0)
	self.frame:Show()
	self:Refresh()
end

-- Draws the page again from the latest snapshot; opening counts as seen, so
-- a turn that finishes while the page is open is seen too.
function Detail:Refresh()
	if not self.frame or not self.frame:IsShown() or not self.agentId then
		return
	end
	local a = ns.Store:Find(self.agentId)
	if a and ns.Store:MarkSeen(a) and ns.Tracker and ns.Tracker:IsOpen() then
		ns.Tracker:Refresh()
	end
	local keep = self.scroll:GetVerticalScroll()
	self:Render()
	self.scroll:SetVerticalScroll(math.min(keep, self.scroll:GetVerticalScrollRange()))
end

function Detail:IsOpen()
	return self.frame ~= nil and self.frame:IsShown()
end

ns:Listen("SNAPSHOT", function()
	Detail:Refresh()
end)
