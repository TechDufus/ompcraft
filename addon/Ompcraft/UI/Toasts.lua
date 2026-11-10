-- Ompcraft: toasts in Blizzard's alert-frame look. Up to three stacked near
-- the top centre, seven seconds each, held while in combat and flushed
-- afterwards. Finished agents get the achievement alert, "needs you" the
-- smaller criteria alert, failed and exited a muted version of it. Clicking
-- one opens the agent's detail page. Local only: a toast, a sound, a chat line.

local _, ns = ...

local Toasts = {}
ns.Toasts = Toasts

local Style = ns.Style
local A = Style.ATLAS

local MAX_VISIBLE = 3
local MAX_QUEUED = 20
local LIFETIME = 7
local FADE = 0.6
local TOP = 110
local PITCH = 106
local SLIDE = 30 -- pixels a toast slides in from the right
local SLIDE_TIME = 0.25
local SOUND_GAP = 1 -- seconds; a flush after combat plays one sound, not twenty

-- Geometry follows AlertFrameSystems.xml: AchievementAlertFrameTemplate (300x101)
-- and CriteriaAlertFrameTemplate (256x84).
local FULL = {
	w = 300, h = 101, bg = A.alertBackground, glow = A.alertGlow, shine = A.alertShine, shineSlide = 240,
	label = { w = 200, h = 12, x = 7, y = -23 }, nameW = 186, iconBox = 78, iconSize = 52, iconY = 0,
	iconPoint = { "TOPLEFT", -4, -15 }, markerSize = 38, overlay = { -1, 2 },
}
local MINI = {
	w = 256, h = 84, bg = A.alertMini, glow = A.alertMiniGlow, shine = A.alertMiniShine, shineSlide = 210,
	label = { w = 170, h = 12, x = 22, y = -18 }, nameW = 172, iconBox = 116, iconSize = 50, iconY = 3,
	iconPoint = { "LEFT", -18, -1 }, markerSize = 34, overlay = { -1, 2 }, mini = true,
}

local STYLES = {
	done = { geo = FULL, label = "Finished", sound = "IG_QUEST_LIST_COMPLETE", color = { 0.3, 0.85, 0.4 } },
	input = { geo = MINI, label = "Needs you", sound = "READY_CHECK", color = { 1, 0.69, 0 } },
	failed = { geo = MINI, label = "Failed", sound = "UI_BNET_TOAST", color = { 0.9, 0.25, 0.25 }, muted = true },
	exited = { geo = MINI, label = "Exited", sound = "UI_BNET_TOAST", color = { 0.7, 0.7, 0.7 }, muted = true },
}
-- Neutral, informational: the companion updated the addon, or a kind this
-- version does not know. No agent behind it, so a click only dismisses it.
local INFO_COLOR = { 0.3, 0.6, 1 }
STYLES.update = { geo = MINI, label = "Ompcraft update", sound = "UI_BNET_TOAST", color = INFO_COLOR, neutral = true }
local DEFAULT_STYLE = { geo = MINI, label = "Ompcraft", sound = "UI_BNET_TOAST", color = INFO_COLOR, neutral = true }

Toasts.STYLES = STYLES
Toasts.frames = {}
Toasts.queue = {}

local function release(frame)
	frame.active = false
	frame:Hide()
	Toasts:Pump()
end

local function onUpdate(frame)
	if not frame.active then
		return
	end
	local age = GetTime() - frame.shownAt
	if age >= LIFETIME then
		release(frame)
	elseif age > LIFETIME - FADE then
		frame:SetAlpha((LIFETIME - age) / FADE)
	end
end

-- A one-shot animation group on `target`: fades and slides use these.
local function group(target)
	local g = target:CreateAnimationGroup()
	return g
end

function Toasts:GetFrame(i)
	local f = self.frames[i]
	if f then
		return f
	end
	f = CreateFrame("Frame", "OmpcraftToast" .. i, UIParent)
	f:SetFrameStrata("DIALOG")
	f:EnableMouse(true)
	f.slot = i

	f.bg = f:CreateTexture(nil, "BACKGROUND")
	f.label = f:CreateFontString(nil, "BACKGROUND", "GameFontBlackTiny")
	f.label:SetJustifyH("CENTER")
	f.name = f:CreateFontString(nil, "BACKGROUND", "GameFontHighlight")
	f.name:SetJustifyH("CENTER")
	f.name:SetMaxLines(2) -- the name may take a second line before it is cut
	f.name:SetWordWrap(true)
	f.stats = f:CreateFontString(nil, "BACKGROUND", "GameFontNormalSmall")
	f.stats:SetJustifyH("CENTER")
	f.stats:SetWordWrap(false)

	f.icon = CreateFrame("Frame", nil, f)
	f.icon.marker = f.icon:CreateTexture(nil, "ARTWORK")
	f.icon.marker:SetPoint("CENTER", f.icon, "CENTER", 0, 0)
	f.icon.frame = f.icon:CreateTexture(nil, "OVERLAY")
	f.icon.frame:SetAtlas(A.alertIconFrame, true)

	f.glow = f:CreateTexture(nil, "OVERLAY")
	f.glow:SetBlendMode("ADD")
	f.glow:Hide()
	f.shine = f:CreateTexture(nil, "OVERLAY")
	f.shine:SetBlendMode("ADD")
	f.shine:Hide()

	-- Entrance: fade and slide in, then the glow flashes and the shine sweeps.
	f.slideIn = group(f)
	local fade = f.slideIn:CreateAnimation("Alpha")
	fade:SetFromAlpha(0)
	fade:SetToAlpha(1)
	fade:SetDuration(SLIDE_TIME)
	fade:SetOrder(1)
	local slide = f.slideIn:CreateAnimation("Translation")
	slide:SetOffset(-SLIDE, 0)
	slide:SetDuration(SLIDE_TIME)
	slide:SetSmoothing("OUT")
	slide:SetOrder(1)
	f.slideIn:SetScript("OnFinished", ns.Guard("toast-slide", function()
		f:ClearAllPoints()
		f:SetPoint("TOP", UIParent, "TOP", 0, -(TOP + (f.slot - 1) * PITCH))
	end))

	f.glowAnim = group(f.glow)
	local up = f.glowAnim:CreateAnimation("Alpha")
	up:SetFromAlpha(0)
	up:SetToAlpha(1)
	up:SetDuration(0.2)
	up:SetOrder(1)
	local down = f.glowAnim:CreateAnimation("Alpha")
	down:SetFromAlpha(1)
	down:SetToAlpha(0)
	down:SetDuration(0.5)
	down:SetOrder(2)
	f.glowAnim:SetScript("OnFinished", ns.Guard("toast-glow", function()
		f.glow:Hide()
	end))

	f.shineAnim = group(f.shine)
	local sIn = f.shineAnim:CreateAnimation("Alpha")
	sIn:SetFromAlpha(0)
	sIn:SetToAlpha(1)
	sIn:SetDuration(0.2)
	sIn:SetOrder(1)
	f.shineSweep = f.shineAnim:CreateAnimation("Translation")
	f.shineSweep:SetDuration(0.85)
	f.shineSweep:SetOrder(2)
	local sOut = f.shineAnim:CreateAnimation("Alpha")
	sOut:SetFromAlpha(1)
	sOut:SetToAlpha(0)
	sOut:SetStartDelay(0.35)
	sOut:SetDuration(0.5)
	sOut:SetOrder(2)
	f.shineAnim:SetScript("OnFinished", ns.Guard("toast-shine", function()
		f.shine:Hide()
	end))

	f:SetScript("OnMouseUp", ns.Guard("toast-click", function(self)
		local id = self.agentId
		release(self)
		if id and ns.Detail then
			ns.Detail:Open(id)
		end
	end))
	f:SetScript("OnUpdate", ns.Guard("toast-update", onUpdate))
	f:Hide()
	self.frames[i] = f
	return f
end

-- Lays the frame's parts out for a style, the way the Blizzard templates do.
local function layout(f, style)
	local g = style.geo
	f:SetSize(g.w, g.h)

	f.bg:SetAtlas(g.bg, true)
	f.bg:ClearAllPoints()
	f.bg:SetPoint(g.mini and "LEFT" or "CENTER", f, g.mini and "LEFT" or "CENTER", 0, 0)
	f.bg:SetDesaturated(style.muted and true or false)
	f.bg:SetVertexColor(style.muted and 0.85 or 1, style.muted and 0.85 or 1, style.muted and 0.85 or 1)

	f.label:ClearAllPoints()
	f.label:SetSize(g.label.w, g.label.h)
	f.label:SetPoint("TOP", f, "TOP", g.label.x, g.label.y)
	f.label:SetText(style.label)

	f.name:ClearAllPoints()
	f.name:SetWidth(g.nameW)
	if g.mini then
		f.name:SetPoint("CENTER", f, "CENTER", 27, style.neutral and -10 or -7)
	else
		f.name:SetPoint("TOP", f.label, "BOTTOM", 0, 0)
	end
	f.stats:ClearAllPoints()
	f.stats:SetWidth(g.nameW)
	f.stats:SetPoint("TOP", f.name, "BOTTOM", 0, -2)

	f.icon:ClearAllPoints()
	f.icon:SetSize(g.iconBox, g.iconBox)
	f.icon:SetPoint(g.iconPoint[1], f, g.iconPoint[1], g.iconPoint[2], g.iconPoint[3])
	f.icon.marker:ClearAllPoints()
	f.icon.marker:SetPoint("CENTER", f.icon, "CENTER", 0, g.iconY)
	f.icon.frame:ClearAllPoints()
	f.icon.frame:SetPoint("CENTER", f.icon, "CENTER", g.overlay[1], g.overlay[2])

	f.glow:SetAtlas(g.glow, true)
	f.glow:ClearAllPoints()
	f.glow:SetPoint("CENTER", f, "CENTER", 0, 0)
	f.shine:SetAtlas(g.shine, true)
	f.shine:ClearAllPoints()
	f.shine:SetPoint(g.mini and "TOPLEFT" or "BOTTOMLEFT", f, g.mini and "TOPLEFT" or "BOTTOMLEFT", 0, g.mini and 0 or 8)
	f.shineSweep:SetOffset(g.shineSlide, 0)
end

local function freeFrame()
	for i = 1, MAX_VISIBLE do
		local f = Toasts:GetFrame(i)
		if not f.active then
			return f
		end
	end
	return nil
end

local function playSound(style)
	if not ns.db.alerts.sound then
		return
	end
	local now = GetTime()
	if Toasts.lastSound and now - Toasts.lastSound < SOUND_GAP then
		return
	end
	Toasts.lastSound = now
	local id = SOUNDKIT and SOUNDKIT[style.sound]
	if id then
		pcall(PlaySound, id)
	end
end

function Toasts:Display(frame, toast)
	local style = STYLES[toast.kind] or DEFAULT_STYLE
	local a = (not style.neutral) and ns.Store:Find(toast.agent) or nil
	local name = (a and a.name ~= "") and a.name or toast.text

	layout(frame, style)
	frame.active = true
	frame.shownAt = GetTime()
	frame.toast = toast
	frame.agentId = a and a.id or nil
	frame.style = style

	Style.SetMarker(frame.icon.marker, Style.TOAST_MARKER[toast.kind] or "working", style.geo.markerSize)
	-- the name (or message) gets the whole text area, on up to two lines
	frame.name:SetMaxLines(2)
	frame.name:SetWordWrap(true)
	frame.name:SetText(ns.Truncate(name, 64))
	local stats = ""
	if toast.kind == "done" and a and a.stats ~= "" then
		stats = a.stats
	end
	frame.stats:SetText(stats)
	frame.stats:SetShown(stats ~= "")

	-- slide in from the right: start offset, the animation brings it home
	frame:ClearAllPoints()
	frame:SetPoint("TOP", UIParent, "TOP", SLIDE, -(TOP + (frame.slot - 1) * PITCH))
	frame:SetAlpha(1)
	frame:Show()
	frame.slideIn:Stop()
	frame.slideIn:Play()
	frame.glow:Show()
	frame.glowAnim:Stop()
	frame.glowAnim:Play()
	frame.shine:Show()
	frame.shineAnim:Stop()
	frame.shineAnim:Play()

	if ns.db.alerts.chat then
		ns.Print(toast.text)
	end
	playSound(style)
end

-- Shows queued toasts while a frame is free. Does nothing in combat.
function Toasts:Pump()
	if InCombatLockdown() then
		return
	end
	while #self.queue > 0 do
		local frame = freeFrame()
		if not frame then
			return
		end
		self:Display(frame, table.remove(self.queue, 1))
	end
end

function Toasts:Enqueue(toast)
	local q = self.queue
	q[#q + 1] = toast
	while #q > MAX_QUEUED do
		table.remove(q, 1)
	end
	self:Pump()
end

-- Number of toasts on screen and waiting.
function Toasts:Counts()
	local visible = 0
	for i = 1, #self.frames do
		if self.frames[i].active then
			visible = visible + 1
		end
	end
	return visible, #self.queue
end

-- Which "Pop-up" switch a toast kind answers to; kinds this version does not
-- know follow the updates switch, like the neutral look they get.
local ALERT_SWITCH = { input = "input", done = "done", failed = "failed", exited = "failed", update = "update" }

ns:Listen("TOAST", function(toast)
	if ns.db.alerts[ALERT_SWITCH[toast.kind] or "update"] then
		Toasts:Enqueue(toast)
	end
end)

ns:On("PLAYER_REGEN_ENABLED", function()
	Toasts:Pump()
end)
