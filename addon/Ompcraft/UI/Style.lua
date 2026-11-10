-- Ompcraft: the look. Fonts, colours, quest-style markers and atlas names, all
-- taken from what Blizzard's own UI uses on the Forever build (each name below
-- appears in the `forever` UI source). Fonts and colours that may be missing are
-- looked up with rawget on _G and fall back to the closest GameFont*.

local _, ns = ...

local Style = {}
ns.Style = Style

-- Atlases (SetAtlas / atlas="..." in Blizzard_ObjectiveTracker, Blizzard_FrameXML
-- AlertFrameSystems, Blizzard_UIPanels_Game, SharedXML/QuestUtils).
Style.ATLAS = {
	trackerHeader = "ui-questtracker-primary-objective-header", -- Blizzard_ObjectiveTrackerContainer.xml
	sectionHeader = "UI-QuestTracker-Secondary-Objective-Header", -- Blizzard_ObjectiveTrackerModule.xml
	sectionCollapse = "ui-questtrackerbutton-secondary-collapse", -- ...Module.lua SetAtlas
	sectionExpand = "ui-questtrackerbutton-secondary-expand",
	sectionCollapsePressed = "ui-questtrackerbutton-secondary-collapse-pressed", -- Module.xml
	sectionExpandPressed = "ui-questtrackerbutton-secondary-expand-pressed",
	collapseAll = "ui-questtrackerbutton-collapse-all", -- ...Container.lua SetAtlas
	expandAll = "ui-questtrackerbutton-expand-all",
	collapseAllPressed = "ui-questtrackerbutton-collapse-all-pressed",
	expandAllPressed = "ui-questtrackerbutton-expand-all-pressed",
	buttonHighlight = "ui-questtrackerbutton-yellow-highlight", -- Module.xml HighlightTexture
	stepDone = "ui-questtracker-tracker-check", -- BonusObjectiveTracker.lua SetAtlas
	stepError = "ui-questtracker-objective-fail",
	stepRunning = "ui-questtracker-objective-nub",
	parchment = "QuestBG-Parchment", -- QuestFrameTemplates.xml, ItemTextFrame.xml
	inProgress = "SideInProgressquesticon", -- QuestUtil.GetQuestIconActive
	alertBackground = "ui-achievement-alert-background", -- AlertFrameSystems.xml
	alertGlow = "ui-achievement-alert-glow-glow",
	alertShine = "ui-achievement-alert-glow-shine",
	alertMini = "ui-achievement-alert-background-mini",
	alertMiniGlow = "ui-achievement-alert-background-mini-glow",
	alertMiniShine = "ui-achievement-alert-background-mini-glow-shine",
	alertIconFrame = "ui-achievement-iconframe",
}

-- Texture files (QuestUtil.GetQuestIconOffer / GetQuestIconActive).
Style.TEXTURE = {
	offer = "Interface/GossipFrame/AvailableQuestIcon", -- yellow !
	turnIn = "Interface/GossipFrame/ActiveQuestIcon", -- yellow ?
}

-- Marker per agent kind (UI-BRIEF): yellow ! needs you, yellow ? finished and
-- unseen, grey ? working, red ! exited. Idle, seen agents get none.
local MARKERS = {
	needs = { file = Style.TEXTURE.offer },
	unseen = { file = Style.TEXTURE.turnIn },
	working = { atlas = Style.ATLAS.inProgress },
	exited = { file = Style.TEXTURE.offer, desaturate = true, color = { 1, 0.15, 0.15 } },
}

Style.hasMarker = function(kind)
	return MARKERS[kind] ~= nil
end

-- Points `tex` at the marker for `kind`, or hides it.
function Style.SetMarker(tex, kind, size)
	local m = MARKERS[kind]
	if not m then
		tex:Hide()
		return
	end
	tex:SetDesaturated(m.desaturate and true or false)
	local c = m.color
	tex:SetVertexColor(c and c[1] or 1, c and c[2] or 1, c and c[3] or 1)
	if m.atlas then
		tex:SetAtlas(m.atlas, false)
	else
		tex:SetTexture(m.file)
		tex:SetTexCoord(0, 1, 0, 1)
	end
	tex:SetSize(size, size)
	tex:Show()
end

-- Marker kind used by a toast, same iconography.
Style.TOAST_MARKER = { input = "needs", done = "unseen", failed = "exited", exited = "exited" }

-- Step marks in the detail page (Blizzard's objective check, fail and nub).
function Style.StepAtlas(status)
	if status == "done" or status == "completed" or status == "success" then
		return Style.ATLAS.stepDone
	elseif status == "error" or status == "failed" then
		return Style.ATLAS.stepError
	end
	return Style.ATLAS.stepRunning
end

------------------------------------------------------------------------
-- Fonts and colours, resolved once the Blizzard UI is up.
------------------------------------------------------------------------

local FALLBACK_COLORS = {
	Normal = { 0.8, 0.8, 0.8 },
	NormalHighlight = { 1, 1, 1 },
	Header = { 0.75, 0.61, 0 },
	HeaderHighlight = { 1, 0.82, 0 },
	Complete = { 0.6, 0.6, 0.6 },
	Failed = { 0.8, 0.1, 0.1 },
	FailedHighlight = { 1, 0.1, 0.1 },
}

local function fontOr(name, fallback)
	if rawget(_G, name) ~= nil then
		return name
	end
	return fallback
end

local function parchmentColor(tableName, fr, fg, fb)
	local t = rawget(_G, tableName)
	local c = t and t.Parchment
	if c and c.GetRGB then
		return { c:GetRGB() }
	end
	return { fr, fg, fb }
end

function Style.Resolve()
	-- The tracker's own fonts (shadowed, 12 px lines and 14 px headers); the
	-- closest GameFont objects if the objective tracker is not loaded.
	Style.font = {
		line = fontOr("ObjectiveTrackerLineFont", "GameFontNormalSmall"),
		header = fontOr("ObjectiveTrackerHeaderFont", "GameFontNormal"),
		small = "GameFontNormalSmall",
		tiny = "GameFontBlackTiny",
		highlight = "GameFontHighlight",
		dim = "GameFontDisableSmall",
		body = fontOr("QuestFont", "GameFontHighlight"),
		title = fontOr("QuestTitleFont", "GameFontNormalLarge"),
		heading = fontOr("QuestFontNormalLarge", "GameFontNormalLarge"),
		codeLine = "GameFontHighlightSmall",
	}
	-- OBJECTIVE_TRACKER_COLOR is the tracker's palette (Normal 0.8 grey, gold
	-- headers, white on hover).
	local palette = rawget(_G, "OBJECTIVE_TRACKER_COLOR")
	Style.color = {}
	for key, fb in pairs(FALLBACK_COLORS) do
		local c = palette and palette[key]
		Style.color[key] = c and { c.r, c.g, c.b } or fb
	end
	-- Detail page text sits on parchment, so it is dark.
	Style.paper = {
		text = parchmentColor("MATERIAL_TEXT_COLOR_TABLE", 0.18, 0.12, 0.06),
		title = parchmentColor("MATERIAL_TITLETEXT_COLOR_TABLE", 0.35, 0.1, 0.02),
		dim = { 0.42, 0.34, 0.24 },
		codeHex = "0b4f7a", -- inline code on parchment
		boldHex = "5a1608", -- bold on parchment
		codeBand = { 0, 0, 0, 0.55 },
		codeText = { 0.82, 0.9, 0.96 },
		rule = { 0.35, 0.25, 0.1, 0.5 },
		rowBand = { 0.35, 0.25, 0.1, 0.1 }, -- every second table row: barely there
	}
	return Style
end

-- Applies a colour triple to a FontString.
function Style.Color(fs, c, alpha)
	fs:SetTextColor(c[1], c[2], c[3], alpha or 1)
end

Style.Resolve()
