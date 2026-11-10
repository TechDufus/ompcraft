-- A small stand-in for the WoW: Forever client, just enough to run Ompcraft.
--
-- It is strict on purpose, so a mistaken API shows up as a test failure:
--   * reading an undefined global is an error (OmpcraftDB and OmpcraftInbox
--     may legitimately be nil; optional Blizzard globals are read with rawget);
--   * every widget method, script name, event name, font object, template,
--     atlas and texture file the addon may use is listed below, and CamelCase
--     methods that are not listed raise an error (lower-case keys are the
--     addon's own fields);
--   * data.lua files are run in a sandbox that may only assign OmpcraftInbox.
-- The lists mirror what addon/Ompcraft calls; each name was checked against
-- Blizzard's API documentation and UI source for build 1.60.1.70862.

local Stub = {}

------------------------------------------------------------------------
-- helpers
------------------------------------------------------------------------

local function deepcopy(v)
	if type(v) ~= "table" then
		return v
	end
	local out = {}
	for k, x in pairs(v) do
		out[k] = deepcopy(x)
	end
	return out
end
Stub.deepcopy = deepcopy

local function readFile(path)
	local f = io.open(path, "rb")
	if not f then
		return nil
	end
	local s = f:read("*a")
	f:close()
	return s
end

local function strict(tbl, label)
	return setmetatable(tbl, {
		__index = function(_, k)
			error(string.format("stub: %s.%s is not available", label, tostring(k)), 2)
		end,
	})
end

local KNOWN_EVENTS = {
	ADDON_LOADED = true,
	PLAYER_LOGIN = true,
	PLAYER_REGEN_ENABLED = true,
	PLAYER_REGEN_DISABLED = true,
	PLAYER_ENTERING_WORLD = true,
	ZONE_CHANGED_NEW_AREA = true,
}
local KNOWN_SCRIPTS = {
	OnEvent = true, OnUpdate = true, OnShow = true, OnHide = true, OnClick = true, OnEnter = true,
	OnLeave = true, OnDragStart = true, OnDragStop = true, OnMouseUp = true, OnMouseDown = true,
}
local GROUP_SCRIPTS = { OnFinished = true, OnPlay = true, OnStop = true }

-- Font objects that exist on the build (Blizzard_Fonts_Shared, Blizzard_ObjectiveTracker).
local FONT_OBJECTS = {
	"GameFontNormal", "GameFontNormalSmall", "GameFontHighlight", "GameFontHighlightSmall", "GameFontDisableSmall",
	"GameFontBlackTiny", "GameFontNormalLarge", "QuestFont", "QuestTitleFont", "QuestFontNormalLarge",
}
local TRACKER_FONTS = { "ObjectiveTrackerLineFont", "ObjectiveTrackerHeaderFont" }
local FONT_SIZE = {
	ObjectiveTrackerLineFont = 12, ObjectiveTrackerHeaderFont = 14, GameFontNormal = 13, GameFontNormalSmall = 10,
	GameFontHighlight = 13, GameFontHighlightSmall = 10, GameFontDisableSmall = 10, GameFontBlackTiny = 9,
	GameFontNormalLarge = 16, QuestFont = 13, QuestTitleFont = 24, QuestFontNormalLarge = 18,
}

-- Atlases Blizzard code applies via SetAtlas or atlas="..." on the forever branch.
local ATLASES = {}
for _, name in ipairs({
	"ui-questtracker-primary-objective-header", "UI-QuestTracker-Secondary-Objective-Header",
	"ui-questtrackerbutton-secondary-collapse", "ui-questtrackerbutton-secondary-expand",
	"ui-questtrackerbutton-secondary-collapse-pressed", "ui-questtrackerbutton-secondary-expand-pressed",
	"ui-questtrackerbutton-collapse-all", "ui-questtrackerbutton-expand-all",
	"ui-questtrackerbutton-collapse-all-pressed", "ui-questtrackerbutton-expand-all-pressed",
	"ui-questtrackerbutton-yellow-highlight", "ui-questtracker-tracker-check", "ui-questtracker-objective-fail",
	"ui-questtracker-objective-nub", "QuestBG-Parchment", "SideInProgressquesticon",
	"ui-achievement-alert-background", "ui-achievement-alert-glow-glow", "ui-achievement-alert-glow-shine",
	"ui-achievement-alert-background-mini", "ui-achievement-alert-background-mini-glow",
	"ui-achievement-alert-background-mini-glow-shine", "ui-achievement-iconframe",
}) do
	ATLASES[name:lower()] = true
end
Stub.ATLASES = ATLASES
local TEXTURE_FILES = {
	["Interface/GossipFrame/AvailableQuestIcon"] = true,
	["Interface/GossipFrame/ActiveQuestIcon"] = true,
	["Interface\\ChatFrame\\UI-ChatIM-SizeGrabber-Up"] = true,
	["Interface\\ChatFrame\\UI-ChatIM-SizeGrabber-Down"] = true,
	["Interface\\ChatFrame\\UI-ChatIM-SizeGrabber-Highlight"] = true,
}

local LAYERS = { BACKGROUND = true, BORDER = true, ARTWORK = true, OVERLAY = true, HIGHLIGHT = true }
local STRATA = { BACKGROUND = true, LOW = true, MEDIUM = true, HIGH = true, DIALOG = true, FULLSCREEN = true, TOOLTIP = true }
local ANCHORS = {
	TOPLEFT = true, TOP = true, TOPRIGHT = true, LEFT = true, CENTER = true, RIGHT = true,
	BOTTOMLEFT = true, BOTTOM = true, BOTTOMRIGHT = true,
}
local TOOLTIP_ANCHORS = { ANCHOR_RIGHT = true, ANCHOR_LEFT = true }
local SMOOTHING = { NONE = true, IN = true, OUT = true, IN_OUT = true, OUT_IN = true }
local LOOPING = { NONE = true, REPEAT = true, BOUNCE = true }
local BLEND = { DISABLE = true, BLEND = true, ALPHAKEY = true, ADD = true, MOD = true }

local function checkNumber(v, what)
	if type(v) ~= "number" then
		error(string.format("stub: %s expects a number, got %s", what, type(v)), 3)
	end
end

------------------------------------------------------------------------
-- widgets
------------------------------------------------------------------------

local Region, Frame, FontString, Texture, Tooltip, ScrollFrame, Panel = {}, {}, {}, {}, {}, {}, {}
local AnimGroup, Anim, AnimAlpha, AnimTranslation = {}, {}, {}, {}

function Region.SetPoint(w, point, a2, a3, a4, a5)
	local s = w.__s
	if not ANCHORS[point] then
		error("stub: bad anchor point " .. tostring(point), 2)
	end
	local rel, relPoint, x, y
	if type(a2) == "table" then
		rel, relPoint, x, y = a2, a3 or point, a4 or 0, a5 or 0
		if type(a3) == "number" then
			relPoint, x, y = point, a3, a4 or 0
		end
	elseif type(a2) == "number" then
		rel, relPoint, x, y = s.parent, point, a2, a3 or 0
	else
		rel, relPoint, x, y = s.parent, point, 0, 0
	end
	if relPoint and not ANCHORS[relPoint] then
		error("stub: bad relative anchor point " .. tostring(relPoint), 2)
	end
	for i, p in ipairs(s.points) do
		if p.point == point then
			s.points[i] = { point = point, rel = rel, relPoint = relPoint, x = x, y = y }
			return
		end
	end
	s.points[#s.points + 1] = { point = point, rel = rel, relPoint = relPoint, x = x, y = y }
end
function Region.SetAllPoints(w, target)
	w.__s.points = { { point = "TOPLEFT", rel = target, relPoint = "TOPLEFT", x = 0, y = 0 }, { point = "BOTTOMRIGHT", rel = target, relPoint = "BOTTOMRIGHT", x = 0, y = 0 } }
	w.__s.allPoints = target
end
function Region.ClearAllPoints(w)
	w.__s.points = {}
	w.__s.allPoints = nil
end
function Region.GetPoint(w, i)
	local p = w.__s.points[i or 1]
	if not p then
		return nil
	end
	return p.point, p.rel, p.relPoint, p.x, p.y
end
function Region.SetSize(w, width, height)
	checkNumber(width, "SetSize width")
	checkNumber(height, "SetSize height")
	w.__s.width, w.__s.height = width, height
end
function Region.SetWidth(w, width)
	checkNumber(width, "SetWidth")
	w.__s.width = width
end
function Region.SetHeight(w, height)
	checkNumber(height, "SetHeight")
	w.__s.height = height
end
function Region.GetWidth(w)
	return w.__s.width or 0
end
function Region.GetHeight(w)
	return w.__s.height or 0
end
function Region.GetBottom(w)
	return w.__s.bottom
end
function Region.GetLeft(w)
	return w.__s.left
end
function Region.GetTop(w)
	return w.__s.top
end
function Region.SetAlpha(w, alpha)
	checkNumber(alpha, "SetAlpha")
	w.__s.alpha = alpha
end
function Region.GetParent(w)
	return w.__s.parent
end
function Region.Show(w)
	local s = w.__s
	if s.shown then
		return
	end
	s.shown = true
	local fn = s.scripts and s.scripts.OnShow
	if fn then
		fn(w)
	end
end
function Region.Hide(w)
	local s = w.__s
	if not s.shown then
		return
	end
	s.shown = false
	local fn = s.scripts and s.scripts.OnHide
	if fn then
		fn(w)
	end
end
function Region.SetShown(w, shown)
	if shown then
		Region.Show(w)
	else
		Region.Hide(w)
	end
end
function Region.IsShown(w)
	return w.__s.shown
end
function Region.SetVertexColor(w, r, g, b, a)
	checkNumber(r, "SetVertexColor r")
	checkNumber(g, "SetVertexColor g")
	checkNumber(b, "SetVertexColor b")
	w.__s.vertex = { r, g, b, a }
end
function Region.CreateAnimationGroup(w)
	local g = w.__s.client:newWidget("AnimGroup", w)
	g.__s.anims = {}
	g.__s.looping = "NONE"
	g.__s.target = w
	return g
end

function Frame.SetScript(w, name, fn)
	if not KNOWN_SCRIPTS[name] then
		error("stub: unknown script " .. tostring(name), 2)
	end
	if fn ~= nil and type(fn) ~= "function" then
		error("stub: SetScript expects a function", 2)
	end
	w.__s.scripts[name] = fn
end
function Frame.RegisterEvent(w, event)
	if not KNOWN_EVENTS[event] then
		error("stub: unknown event " .. tostring(event), 2)
	end
	w.__s.events[event] = true
end
function Frame.SetFrameStrata(w, strata)
	if not STRATA[strata] then
		error("stub: bad frame strata " .. tostring(strata), 2)
	end
	w.__s.strata = strata
end
function Frame.SetFrameLevel(w, level)
	checkNumber(level, "SetFrameLevel")
	w.__s.level = level
end
function Frame.GetFrameLevel(w)
	return w.__s.level or 1
end
function Frame.SetToplevel(w, v)
	w.__s.toplevel = v
end
function Frame.SetClampedToScreen(w, v)
	w.__s.clamped = v
end
function Frame.SetMovable(w, v)
	w.__s.movable = v
end
function Frame.SetDontSavePosition(w, v)
	w.__s.dontSave = v
end
function Frame.SetScale(w, scale)
	checkNumber(scale, "SetScale")
	if scale <= 0 then
		error("stub: SetScale needs a positive scale", 2)
	end
	w.__s.scale = scale
end
function Frame.EnableMouse(w, v)
	w.__s.mouse = v
end
function Frame.RegisterForDrag(w, ...)
	w.__s.drag = { ... }
end
function Frame.StartMoving(w)
	if not w.__s.movable then
		error("stub: StartMoving on a frame that is not movable", 2)
	end
	w.__s.moving = true
end
function Frame.StopMovingOrSizing(w)
	w.__s.moving = false
	w.__s.sizing = nil
end
function Frame.SetResizable(w, v)
	w.__s.resizable = v and true or false
end
function Frame.SetResizeBounds(w, minW, minH, maxW, maxH)
	checkNumber(minW, "SetResizeBounds minW")
	checkNumber(minH, "SetResizeBounds minH")
	w.__s.resizeBounds = { minW, minH, maxW, maxH }
end
function Frame.StartSizing(w, point)
	if not w.__s.resizable then
		error("stub: StartSizing on a frame that is not resizable", 2)
	end
	if not ANCHORS[point] then
		error("stub: bad sizing point " .. tostring(point), 2)
	end
	w.__s.sizing = point
end
local function needBackdrop(w)
	if w.__s.template ~= "BackdropTemplate" then
		error("stub: backdrop methods need BackdropTemplate", 3)
	end
end
function Frame.SetBackdrop(w, info)
	needBackdrop(w)
	w.__s.backdrop = info
end
function Frame.SetBackdropColor(w, r, g, b, a)
	needBackdrop(w)
	checkNumber(r, "SetBackdropColor")
	w.__s.backdropColor = { r, g, b, a }
end
function Frame.SetBackdropBorderColor(w, r, g, b, a)
	needBackdrop(w)
	checkNumber(r, "SetBackdropBorderColor")
	w.__s.backdropBorderColor = { r, g, b, a }
end

function Panel.SetTitle(w, title)
	w.TitleContainer.TitleText:SetText(title)
end

function ScrollFrame.SetScrollChild(w, child)
	w.__s.scrollChild = child
	child.__s.parent = w
end
function ScrollFrame.GetVerticalScrollRange(w)
	local child = w.__s.scrollChild
	local viewport = w.__s.height or 420
	return math.max(0, (child and child.__s.height or 0) - viewport)
end
function ScrollFrame.SetVerticalScroll(w, v)
	checkNumber(v, "SetVerticalScroll")
	w.__s.vscroll = math.max(0, math.min(v, ScrollFrame.GetVerticalScrollRange(w)))
end
function ScrollFrame.GetVerticalScroll(w)
	return w.__s.vscroll or 0
end

local function fontKnown(w, name)
	local env = w.__s.client.env
	if type(name) ~= "string" or rawget(env, name) == nil then
		error("stub: unknown font object " .. tostring(name), 3)
	end
end

local function plain(text)
	return (text:gsub("|c%x%x%x%x%x%x%x%x", ""):gsub("|r", ""):gsub("||", "|"))
end

function FontString.SetText(w, text)
	local t = type(text)
	if t ~= "string" and t ~= "number" and t ~= "nil" then
		error("stub: SetText expects a string, got " .. t, 2)
	end
	w.__s.text = text ~= nil and tostring(text) or ""
end
function FontString.GetText(w)
	return w.__s.text
end
function FontString.SetFontObject(w, name)
	fontKnown(w, name)
	w.__s.font = name
end
function FontString.SetTextColor(w, r, g, b, a)
	checkNumber(r, "SetTextColor")
	w.__s.textColor = { r, g, b, a }
end
function FontString.SetJustifyH(w, j)
	if j ~= "LEFT" and j ~= "CENTER" and j ~= "RIGHT" then
		error("stub: bad justify " .. tostring(j), 2)
	end
	w.__s.justifyH = j
end
function FontString.SetJustifyV(w, j)
	if j ~= "TOP" and j ~= "MIDDLE" and j ~= "BOTTOM" then
		error("stub: bad vertical justify " .. tostring(j), 2)
	end
	w.__s.justifyV = j
end
function FontString.SetWordWrap(w, v)
	w.__s.wordWrap = v
end
function FontString.SetMaxLines(w, n)
	checkNumber(n, "SetMaxLines")
	w.__s.maxLines = n
end
-- A rough measure: characters of a 12 px font are about 6 px wide.
function FontString.GetStringHeight(w)
	local s = w.__s
	local text = plain(s.text or "")
	if text == "" then
		return 0
	end
	local size = FONT_SIZE[s.font] or 12
	local width = s.width or 200
	local perLine = math.max(1, math.floor(width / (size * 0.5)))
	local lines = 0
	for piece in (text .. "\n"):gmatch("(.-)\n") do
		lines = lines + math.max(1, math.ceil(#piece / perLine))
	end
	if s.wordWrap == false then
		lines = 1 -- the real client: a non-wrapping FontString shows one line, "\n" or not
	end
	if s.maxLines and s.maxLines > 0 then
		lines = math.min(lines, s.maxLines)
	end
	return lines * (size + 2)
end

function Texture.SetColorTexture(w, r, g, b, a)
	checkNumber(r, "SetColorTexture r")
	checkNumber(g, "SetColorTexture g")
	checkNumber(b, "SetColorTexture b")
	w.__s.color = { r, g, b, a }
	w.__s.atlas, w.__s.texture = nil, nil
end
function Texture.SetAtlas(w, atlas, useAtlasSize)
	if type(atlas) ~= "string" or not ATLASES[atlas:lower()] then
		error("stub: atlas not verified in the forever UI source: " .. tostring(atlas), 2)
	end
	w.__s.atlas, w.__s.texture, w.__s.color = atlas, nil, nil
	if useAtlasSize then
		w.__s.width, w.__s.height = w.__s.width or 64, w.__s.height or 64
	end
end
function Texture.SetTexture(w, file)
	if type(file) ~= "string" or not TEXTURE_FILES[file] then
		error("stub: texture file not verified in the forever UI source: " .. tostring(file), 2)
	end
	w.__s.texture, w.__s.atlas, w.__s.color = file, nil, nil
end
function Texture.SetDesaturated(w, v)
	w.__s.desaturated = v and true or false
end
function Texture.SetBlendMode(w, mode)
	if not BLEND[mode] then
		error("stub: bad blend mode " .. tostring(mode), 2)
	end
	w.__s.blend = mode
end
function Texture.SetTexCoord(w, a, b, c, d)
	checkNumber(a, "SetTexCoord")
	w.__s.texCoord = { a, b, c, d }
end

function Tooltip.SetOwner(w, owner, anchor)
	if not TOOLTIP_ANCHORS[anchor] then
		error("stub: bad tooltip anchor " .. tostring(anchor), 2)
	end
	w.__s.owner, w.__s.lines = owner, {}
end
function Tooltip.GetOwner(w)
	return w.__s.owner
end
function Tooltip.SetText(w, text, r, g, b, a, wrap)
	if type(text) ~= "string" then
		error("stub: tooltip SetText expects a string", 2)
	end
	w.__s.lines = { { text } }
end
function Tooltip.AddLine(w, text, r, g, b, wrap)
	if type(text) ~= "string" then
		error("stub: tooltip AddLine expects a string", 2)
	end
	w.__s.lines[#w.__s.lines + 1] = { text, wrap = wrap }
end
function Tooltip.AddDoubleLine(w, left, right)
	if type(left) ~= "string" or type(right) ~= "string" then
		error("stub: tooltip AddDoubleLine expects strings", 2)
	end
	w.__s.lines[#w.__s.lines + 1] = { left, right }
end

-- Animations: a group plays its children in order; the stub only tracks
-- whether it is playing and fires OnFinished after the total duration.
function AnimGroup.SetScript(w, name, fn)
	if not GROUP_SCRIPTS[name] then
		error("stub: unknown animation group script " .. tostring(name), 2)
	end
	w.__s.scripts[name] = fn
end
function AnimGroup.CreateAnimation(w, kind)
	if kind ~= "Alpha" and kind ~= "Translation" then
		error("stub: animation type not used by the addon: " .. tostring(kind), 2)
	end
	local a = w.__s.client:newWidget(kind == "Alpha" and "AnimAlpha" or "AnimTranslation", w)
	a.__s.kind, a.__s.duration, a.__s.delay, a.__s.order = kind, 0, 0, 1
	w.__s.anims[#w.__s.anims + 1] = a
	return a
end
function AnimGroup.SetLooping(w, mode)
	if not LOOPING[mode] then
		error("stub: bad looping " .. tostring(mode), 2)
	end
	w.__s.looping = mode
end
function AnimGroup.IsPlaying(w)
	return w.__s.playing == true
end
local function groupDuration(g)
	local perOrder, total = {}, 0
	for _, a in ipairs(g.__s.anims) do
		local o = a.__s.order
		perOrder[o] = math.max(perOrder[o] or 0, a.__s.delay + a.__s.duration)
	end
	for _, d in pairs(perOrder) do
		total = total + d
	end
	return total
end
function AnimGroup.Play(w)
	local s = w.__s
	s.playToken = (s.playToken or 0) + 1
	s.playing = true
	s.plays = (s.plays or 0) + 1
	local token = s.playToken
	if s.looping == "NONE" then
		s.client:addTimer(groupDuration(w), function()
			if s.playToken == token and s.playing then
				s.playing = false
				local fn = s.scripts.OnFinished
				if fn then
					fn(w)
				end
			end
		end)
	end
end
function AnimGroup.Stop(w)
	local s = w.__s
	s.playToken = (s.playToken or 0) + 1
	s.playing = false
end

function Anim.SetDuration(w, d)
	checkNumber(d, "SetDuration")
	w.__s.duration = d
end
function Anim.SetStartDelay(w, d)
	checkNumber(d, "SetStartDelay")
	w.__s.delay = d
end
function Anim.SetOrder(w, o)
	checkNumber(o, "SetOrder")
	w.__s.order = o
end
function Anim.SetSmoothing(w, mode)
	if not SMOOTHING[mode] then
		error("stub: bad smoothing " .. tostring(mode), 2)
	end
	w.__s.smoothing = mode
end
function AnimAlpha.SetFromAlpha(w, a)
	checkNumber(a, "SetFromAlpha")
	w.__s.fromAlpha = a
end
function AnimAlpha.SetToAlpha(w, a)
	checkNumber(a, "SetToAlpha")
	w.__s.toAlpha = a
end
function AnimTranslation.SetOffset(w, x, y)
	checkNumber(x, "SetOffset x")
	checkNumber(y, "SetOffset y")
	w.__s.offset = { x, y }
end

local ButtonOnly = {}
local CLICKS = { LeftButtonUp = true, LeftButtonDown = true, RightButtonUp = true, RightButtonDown = true, AnyUp = true, AnyDown = true }
function ButtonOnly.RegisterForClicks(w, ...)
	for _, name in ipairs({ ... }) do
		if not CLICKS[name] then
			error("stub: bad click type " .. tostring(name), 2)
		end
	end
	w.__s.clicks = { ... }
end

local METHODS = {
	Frame = { Region, Frame },
	Button = { Region, Frame, ButtonOnly },
	ScrollFrame = { Region, Frame, ScrollFrame },
	FontString = { Region, FontString },
	Texture = { Region, Texture },
	Tooltip = { Region, Frame, Tooltip },
	AnimGroup = { AnimGroup },
	AnimAlpha = { Anim, AnimAlpha },
	AnimTranslation = { Anim, AnimTranslation },
}

local widgetMeta = {
	__index = function(w, key)
		if type(key) ~= "string" then
			return nil
		end
		local first = key:byte(1)
		if first and first >= 65 and first <= 90 then -- CamelCase => engine method
			local kind = rawget(w, "__kind")
			local sets = METHODS[kind]
			for i = 1, #sets do
				local fn = sets[i][key]
				if fn then
					return fn
				end
			end
			-- methods that come from a Blizzard template's mixin
			if rawget(w, "__s").template == "ButtonFrameTemplate" and Panel[key] then
				return Panel[key]
			end
			error(string.format("stub: %s has no method %s", kind, key), 2)
		end
		return nil -- an addon-owned field
	end,
}

------------------------------------------------------------------------
-- the client
------------------------------------------------------------------------

local Client = {}
Client.__index = Client

-- opts: addonDir (folder with Ompcraft.toc), fixtureDir (the AddOns folder
-- holding OmpcraftSignals and OmpcraftSnnn), unix (initial unix time),
-- noTrackerFonts (the objective tracker's fonts are not loaded).
function Stub.new(opts)
	local c = setmetatable({}, Client)
	c.addonDir = assert(opts.addonDir)
	c.fixtureDir = assert(opts.fixtureDir)
	c.opts = opts
	c.now0 = 1000
	c.now = c.now0
	c.unix0 = opts.unix or 1762728000
	c.combat = false
	c.found = {} -- sound files found once stay present until a client restart
	c.saved = nil -- SavedVariables, as they would be on disk
	c.chat, c.soundsPlayed, c.soundFileCalls, c.stopped = {}, {}, {}, {}
	c.handle = 0
	c.timerSeq = 0
	c.timers, c.frames, c.loaded, c.loadLog, c.globalWrites = {}, {}, {}, {}, {}
	c.objectiveTracker = nil -- set to { shown = true, bottom = 400 } before login
	return c
end

function Client:time()
	return math.floor(self.unix0 + (self.now - self.now0) + 1e-6)
end

-- The addon's namespace table (the second vararg of each file).
function Client:ns()
	return self.addonNs
end

function Client:addonFiles()
	local toc = assert(readFile(self.addonDir .. "/Ompcraft.toc"), "Ompcraft.toc not found")
	local files = {}
	for line in toc:gmatch("[^\r\n]+") do
		if not line:match("^%s*#") and line:match("%S") then
			files[#files + 1] = (line:gsub("^%s+", ""):gsub("%s+$", ""):gsub("\\", "/"))
		end
	end
	return files
end

function Client:tocField(field)
	local toc = readFile(self.addonDir .. "/Ompcraft.toc")
	return toc and toc:match("## " .. field .. ":%s*([^\r\n]*)")
end

local function tocMeta(path, field)
	local toc = readFile(path)
	if not toc then
		return nil
	end
	local v = toc:match("## " .. field:gsub("%-", "%%-") .. ":%s*([^\r\n]*)")
	return v
end

function Client:addonToc(name)
	return self.fixtureDir .. "/" .. name .. "/" .. name .. ".toc"
end

local function makeSoundHandle(c)
	c.handle = c.handle + 1
	return c.handle
end

function Client:newWidget(kind, parent, template)
	local w = setmetatable({}, widgetMeta)
	rawset(w, "__kind", kind)
	rawset(w, "__s", {
		points = {},
		shown = true,
		scripts = {},
		events = {},
		parent = parent,
		template = template,
		children = {},
		client = self,
		anims = nil,
	})
	if parent then
		local list = parent.__s.children
		list[#list + 1] = w
	end
	return w
end

function Client:addTimer(seconds, fn, iterations, repeating)
	checkNumber(seconds, "C_Timer seconds")
	if type(fn) ~= "function" then
		error("stub: C_Timer callback must be a function", 3)
	end
	self.timerSeq = self.timerSeq + 1
	local t = { due = self.now + seconds, interval = seconds, fn = fn, seq = self.timerSeq, repeating = repeating, left = iterations }
	local obj = {
		Cancel = function()
			t.cancelled = true
		end,
		IsCancelled = function()
			return t.cancelled == true
		end,
	}
	t.obj = obj
	self.timers[#self.timers + 1] = t
	return obj
end

-- Builds a fresh Lua environment: a new UI session.
function Client:buildEnv()
	local c = self
	local env = {}
	self.env = env
	self.globalWrites = {}
	self.timers, self.frames = {}, {}
	self.loaded = { Ompcraft = true }

	for _, name in ipairs({
		"assert", "error", "getmetatable", "ipairs", "next", "pairs", "pcall", "rawequal", "rawget",
		"rawset", "select", "setmetatable", "tonumber", "tostring", "type", "unpack", "xpcall",
		"math", "string", "table",
	}) do
		env[name] = _G[name]
	end
	env._G = env

	env.time = function()
		return c:time()
	end
	env.GetTime = function()
		return c.now
	end
	env.InCombatLockdown = function()
		return c.combat
	end

	for _, name in ipairs(FONT_OBJECTS) do
		env[name] = {}
	end
	if not self.opts.noTrackerFonts then
		for _, name in ipairs(TRACKER_FONTS) do
			env[name] = {}
		end
		-- the tracker's own palette (Blizzard_ObjectiveTrackerShared.lua)
		env.OBJECTIVE_TRACKER_COLOR = {
			Normal = { r = 0.8, g = 0.8, b = 0.8 }, NormalHighlight = { r = 1, g = 1, b = 1 },
			Header = { r = 0.75, g = 0.61, b = 0 }, HeaderHighlight = { r = 1, g = 0.82, b = 0 },
			Complete = { r = 0.6, g = 0.6, b = 0.6 }, Failed = { r = 0.8, g = 0.1, b = 0.1 },
			FailedHighlight = { r = 1, g = 0.1, b = 0.1 },
		}
	end

	env.C_Timer = strict({
		After = function(seconds, fn)
			c:addTimer(seconds, fn, 1, false)
		end,
		NewTimer = function(seconds, fn)
			return c:addTimer(seconds, fn, 1, false)
		end,
		NewTicker = function(seconds, fn, iterations)
			return c:addTimer(seconds, fn, iterations, true)
		end,
	}, "C_Timer")

	-- addons
	env.C_AddOns = strict({
		DoesAddOnExist = function(name)
			return name == "Ompcraft" or readFile(c:addonToc(name)) ~= nil
		end,
		GetAddOnMetadata = function(name, field)
			return tocMeta(c:addonToc(name), field)
		end,
		IsAddOnLoaded = function(name)
			local loaded = c.loaded[name] == true
			return loaded, loaded
		end,
		LoadAddOn = function(name)
			if c.loaded[name] then
				return true
			end
			if not readFile(c:addonToc(name)) then
				return nil, "MISSING"
			end
			local source = readFile(c.fixtureDir .. "/" .. name .. "/data.lua")
			if not source then
				return nil, "CORRUPT"
			end
			-- Read from disk at this moment; literals only.
			local sandbox = setmetatable({}, {
				__index = function(_, k)
					error("data.lua read global " .. tostring(k))
				end,
				__newindex = function(t, k, v)
					if k ~= "OmpcraftInbox" then
						error("data.lua assigned global " .. tostring(k))
					end
					rawset(t, k, v)
				end,
			})
			local chunk, err = loadstring(source, "=" .. name .. "/data.lua")
			if not chunk then
				return nil, "CORRUPT", err
			end
			setfenv(chunk, sandbox)
			chunk()
			rawset(env, "OmpcraftInbox", rawget(sandbox, "OmpcraftInbox"))
			c.loaded[name] = true
			c.loadLog[#c.loadLog + 1] = { name = name, at = c.now }
			c:fire("ADDON_LOADED", name, false)
			return true
		end,
	}, "C_AddOns")

	-- sound
	env.SOUNDKIT = {
		UI_BNET_TOAST = 18019, READY_CHECK = 8960, IG_QUEST_LIST_COMPLETE = 878,
	}
	env.PlaySound = function(id)
		checkNumber(id, "PlaySound soundKitID")
		c.soundsPlayed[#c.soundsPlayed + 1] = id
		return true, makeSoundHandle(c)
	end
	env.PlaySoundFile = function(path, channel)
		if type(path) ~= "string" or channel ~= "Master" then
			error("stub: PlaySoundFile(path, \"Master\") expected", 2)
		end
		local rel = path:match("^[Ii]nterface\\[Aa]dd[Oo]ns\\(.+)$")
		local key = rel and rel:lower()
		local present = false
		if rel then
			if c.found[key] then
				present = true
			elseif readFile(c.fixtureDir .. "/" .. rel:gsub("\\", "/")) ~= nil then
				c.found[key] = true -- stays present until the client restarts
				present = true
			end
		end
		c.soundFileCalls[#c.soundFileCalls + 1] = { file = rel, present = present, at = c.now }
		if present then
			return true, makeSoundHandle(c)
		end
		return false
	end
	env.StopSound = function(handle)
		c.stopped[#c.stopped + 1] = handle
	end


	-- instances (IsInInstance: InstanceDocumentation.lua)
	c.instanceType = "none"
	env.IsInInstance = function()
		return c.instanceType ~= "none", c.instanceType
	end

	-- Blizzard's Settings API, as Blizzard_Settings_Shared defines it. Strict:
	-- anything the addon calls that is not here is an error, and the argument
	-- rules of the real functions are enforced.
	if not self.opts.noSettings then
		local VARTYPES = { boolean = true, string = true, number = true }
		local byVariable, categories = {}, {}
		local function newSetting(category, variable, name, vtype, default, get, set)
			if type(variable) ~= "string" or byVariable[variable] then
				error("stub: Settings variable missing or already registered: " .. tostring(variable), 3)
			end
			if type(name) ~= "string" then
				error("stub: a setting needs a string name", 3)
			end
			if not VARTYPES[vtype] then
				error("stub: bad setting variable type " .. tostring(vtype), 3)
			end
			if default ~= nil and type(default) ~= vtype then
				error("stub: default of " .. variable .. " is not a " .. vtype, 3)
			end
			if not category or not categories[category.id] then
				error("stub: setting registered on an unknown category", 3)
			end
			local callbacks = {}
			local st = { variable = variable, name = name, vtype = vtype, default = default, category = category }
			function st.GetValue(self)
				return get()
			end
			function st.GetVariable(self)
				return variable
			end
			function st.GetVariableType(self)
				return vtype
			end
			function st.SetValueChangedCallback(self, cb)
				if type(cb) ~= "function" then
					error("stub: SetValueChangedCallback expects a function", 2)
				end
				callbacks[#callbacks + 1] = cb
			end
			local function trigger(self, value)
				for _, cb in ipairs(callbacks) do
					cb(self, value)
				end
			end
			function st.SetValue(self, value)
				if type(value) ~= vtype then
					error(string.format("stub: %s expects a %s, got %s", variable, vtype, type(value)), 2)
				end
				if get() ~= value then
					set(value)
				end
				trigger(self, value)
			end
			function st.NotifyUpdate(self)
				trigger(self, get())
			end
			function st.SetValueToDefault(self)
				self:SetValue(default)
			end
			byVariable[variable] = st
			category.settings[#category.settings + 1] = st
			return strict(st, "Setting")
		end

		local function newCategory(name)
			if type(name) ~= "string" then
				error("stub: a category needs a name", 3)
			end
			local id = #categories + 1
			local category = { id = id, name = name, settings = {}, initializers = {}, registered = false }
			function category.GetID(self)
				return id
			end
			function category.GetName(self)
				return name
			end
			categories[id] = category
			local layout = {}
			function layout.AddInitializer(self, init)
				if type(init) ~= "table" or not init.template then
					error("stub: AddInitializer expects an initializer", 2)
				end
				category.initializers[#category.initializers + 1] = init
				return init
			end
			return strict(category, "Category"), strict(layout, "Layout")
		end

		local function assertCategory(category)
			if not category or not categories[category.id] then
				error("stub: unknown category", 3)
			end
		end

		local Settings = {
			VarType = { Boolean = "boolean", String = "string", Number = "number" },
			Default = { True = true, False = false },
		}
		function Settings.RegisterVerticalLayoutCategory(name)
			return newCategory(name)
		end
		function Settings.RegisterAddOnCategory(category)
			assertCategory(category)
			category.registered = true
		end
		function Settings.RegisterAddOnSetting(category, variable, variableKey, variableTbl, variableType, name, default)
			if type(variableTbl) ~= "table" then
				error("stub: 'variableTbl' argument must be a table.", 2)
			end
			if variableTbl[variableKey] == nil then
				variableTbl[variableKey] = default
			end
			return newSetting(category, variable, name, variableType, default, function()
				return variableTbl[variableKey]
			end, function(value)
				variableTbl[variableKey] = value
			end)
		end
		function Settings.RegisterProxySetting(category, variable, variableType, name, default, getValue, setValue)
			if type(getValue) ~= "function" or type(setValue) ~= "function" then
				error("stub: a proxy setting needs getValue and setValue", 2)
			end
			return newSetting(category, variable, name, variableType, default, getValue, setValue)
		end
		local function initializer(template, category, setting, options, tooltip)
			assertCategory(category)
			local init = { template = template, setting = setting, options = options, tooltip = tooltip }
			category.initializers[#category.initializers + 1] = init
			return init
		end
		function Settings.CreateCheckbox(category, setting, tooltip)
			if setting:GetVariableType() ~= "boolean" then
				error("stub: a checkbox needs a boolean setting", 2)
			end
			return initializer("SettingsCheckboxControlTemplate", category, setting, nil, tooltip)
		end
		function Settings.CreateSlider(category, setting, options, tooltip)
			if setting:GetVariableType() ~= "number" or options == nil then
				error("stub: a slider needs a number setting and options", 2)
			end
			return initializer("SettingsSliderControlTemplate", category, setting, options, tooltip)
		end
		function Settings.CreateDropdown(category, setting, options, tooltip)
			if options == nil then
				error("stub: a dropdown needs options", 2)
			end
			return initializer("SettingsDropdownControlTemplate", category, setting, options, tooltip)
		end
		function Settings.CreateSliderOptions(minValue, maxValue, rate)
			checkNumber(minValue, "CreateSliderOptions min")
			checkNumber(maxValue, "CreateSliderOptions max")
			local options = { minValue = minValue, maxValue = maxValue, steps = rate and (maxValue - minValue) / rate or 100, formatters = {} }
			function options.SetLabelFormatter(self, labelType, formatter)
				self.formatters[labelType] = formatter
			end
			return strict(options, "SliderOptions")
		end
		function Settings.CreateControlTextContainer()
			local container = { data = {} }
			function container.Add(self, value, label, tooltip)
				self.data[#self.data + 1] = { value = value, label = label, text = label, tooltip = tooltip }
			end
			function container.GetData(self)
				return self.data
			end
			return strict(container, "ControlTextContainer")
		end
		function Settings.GetSetting(variable)
			return byVariable[variable]
		end
		function Settings.NotifyUpdate(variable)
			local st = byVariable[variable]
			if st then
				st:NotifyUpdate()
			end
		end
		function Settings.OpenToCategory(categoryID, scrollToElementName)
			if c.combat then
				error("stub: C_SettingsUtil.OpenSettingsPanel is restricted in combat", 2)
			end
			if not categories[categoryID] or not categories[categoryID].registered then
				error("stub: opening a category that is not registered", 2)
			end
			c.openedCategory = categoryID
		end
		env.Settings = strict(Settings, "Settings")
		env.MinimalSliderWithSteppersMixin = { Label = { Left = 1, Right = 2, Top = 3, Min = 4, Max = 5 } }
		env.CreateSettingsListSectionHeaderInitializer = function(name, tooltip)
			if type(name) ~= "string" then
				error("stub: a section header needs a name", 2)
			end
			return { template = "SettingsListSectionHeaderTemplate", name = name, tooltip = tooltip }
		end
		env.CreateSettingsButtonInitializer = function(name, buttonText, buttonClick, tooltip, addSearchTags)
			if addSearchTags == nil then
				error("stub: CreateSettingsButtonInitializer asserts addSearchTags ~= nil", 2)
			end
			if type(name) ~= "string" or (type(buttonText) ~= "string" and type(buttonText) ~= "function") or type(buttonClick) ~= "function" then
				error("stub: bad settings button arguments", 2)
			end
			return { template = "SettingButtonControlTemplate", name = name, buttonText = buttonText, buttonClick = buttonClick, tooltip = tooltip }
		end
		c.settingsCategories = categories
	end

	-- Blizzard_Menu: MenuUtil.CreateContextMenu(owner, generator)
	if not self.opts.noMenu then
		env.MenuUtil = strict({
			CreateContextMenu = function(owner, generator)
				local root = { elements = {} }
				local function add(kind)
					return function(self, text, a, b)
						local e = { kind = kind, text = text, a = a, b = b }
						self.elements[#self.elements + 1] = e
						return e
					end
				end
				root.CreateTitle = add("title")
				root.CreateButton = add("button")
				root.CreateCheckbox = add("checkbox")
				root.CreateDivider = function(self)
					self.elements[#self.elements + 1] = { kind = "divider" }
				end
				generator(owner, strict(root, "RootDescription"))
				c.lastMenu = { owner = owner, elements = root.elements }
			end,
		}, "MenuUtil")
	end

	-- UI
	env.SlashCmdList = {}
	env.UISpecialFrames = {}
	env.UIParent = self:newWidget("Frame", nil)
	env.UIParent.__s.width, env.UIParent.__s.height = 1920, 1080
	env.GameTooltip = self:newWidget("Tooltip", env.UIParent)
	env.GameTooltip.__s.shown = false
	env.GameTooltip.__s.lines = {}
	env.DEFAULT_CHAT_FRAME = {
		AddMessage = function(_, text)
			if type(text) ~= "string" then
				error("stub: AddMessage expects a string", 2)
			end
			c.chat[#c.chat + 1] = text
		end,
	}

	-- Blizzard template helpers the addon may call (SharedUIPanelTemplates.lua)
	c.templateCalls = {}
	env.ButtonFrameTemplate_HidePortrait = function(f)
		c.templateCalls[#c.templateCalls + 1] = "HidePortrait"
		f.__s.portraitHidden = true
	end
	env.ButtonFrameTemplate_HideAttic = function(f)
		c.templateCalls[#c.templateCalls + 1] = "HideAttic"
	end
	env.ButtonFrameTemplate_HideButtonBar = function(f)
		c.templateCalls[#c.templateCalls + 1] = "HideButtonBar"
	end

	local TEMPLATES = { Frame = { BackdropTemplate = true, ButtonFrameTemplate = true }, Button = {}, ScrollFrame = { ScrollFrameTemplate = true } }
	env.CreateFrame = function(kind, name, parent, template)
		local allowed = TEMPLATES[kind]
		if not allowed then
			error("stub: CreateFrame kind " .. tostring(kind), 2)
		end
		if template ~= nil and not allowed[template] then
			error("stub: unknown frame template " .. tostring(template) .. " for " .. kind, 2)
		end
		local f = c:newWidget(kind, parent or env.UIParent, template)
		f.__name = name
		if name then
			rawset(env, name, f)
		end
		c.frames[#c.frames + 1] = f
		-- children the Blizzard templates create
		if template == "ButtonFrameTemplate" then
			rawset(f, "Inset", c:newWidget("Frame", f))
			rawset(f, "Bg", c:newWidget("Texture", f))
			rawset(f, "CloseButton", c:newWidget("Button", f))
			rawset(f, "NineSlice", c:newWidget("Frame", f))
			local title = c:newWidget("Frame", f)
			rawset(title, "TitleText", c:newWidget("FontString", title))
			title.TitleText.__s.text = ""
			rawset(f, "TitleContainer", title)
		elseif template == "ScrollFrameTemplate" then
			rawset(f, "ScrollBar", c:newWidget("Frame", f))
		end
		return f
	end

	-- Blizzard's objective tracker, when the test wants one.
	if self.objectiveTracker then
		local otf = c:newWidget("Frame", env.UIParent)
		otf.__s.shown = self.objectiveTracker.shown ~= false
		rawset(otf, "NineSlice", c:newWidget("Frame", otf))
		otf.NineSlice.__s.bottom = self.objectiveTracker.bottom
		env.ObjectiveTrackerFrame = otf
	end

	-- widgets that create children
	function Frame.CreateFontString(w, name, layer, inherits)
		if layer ~= nil and not LAYERS[layer] then
			error("stub: bad draw layer " .. tostring(layer), 2)
		end
		if inherits ~= nil and rawget(env, inherits) == nil then
			error("stub: unknown font object " .. tostring(inherits), 2)
		end
		local fs = c:newWidget("FontString", w)
		fs.__s.text = ""
		fs.__s.font = inherits
		return fs
	end
	function Frame.CreateTexture(w, name, layer)
		if layer ~= nil and not LAYERS[layer] then
			error("stub: bad draw layer " .. tostring(layer), 2)
		end
		return c:newWidget("Texture", w)
	end

	-- Everything above is stub plumbing; from here on, writes are the
	-- addon's globals and are recorded.
	setmetatable(env, {
		__index = function(_, k)
			if k == "OmpcraftDB" or k == "OmpcraftInbox" then
				return nil
			end
			error("stub: access to undefined global '" .. tostring(k) .. "'", 2)
		end,
		__newindex = function(t, k, v)
			c.globalWrites[k] = true
			rawset(t, k, v)
		end,
	})
	return env
end

-- Starts a UI session: loads the TOC's files in order, then fires
-- ADDON_LOADED and PLAYER_LOGIN.
function Client:login()
	self.env = nil
	self.loadLog = {}
	local env = self:buildEnv()
	if self.saved then
		rawset(env, "OmpcraftDB", deepcopy(self.saved))
	end
	self.addonNs = {}
	for _, rel in ipairs(self:addonFiles()) do
		local chunk, err = loadfile(self.addonDir .. "/" .. rel)
		if not chunk then
			error(err)
		end
		setfenv(chunk, env)
		chunk("Ompcraft", self.addonNs)
	end
	self.loginAt = self.now
	self:fire("ADDON_LOADED", "Ompcraft", false)
	self:fire("PLAYER_LOGIN")
	return self
end

-- /reload: SavedVariables are written out, then everything restarts. The
-- sound cache survives (same game process).
function Client:reload()
	local db = rawget(self.env, "OmpcraftDB")
	self.saved = db and deepcopy(db) or nil
	return self:login()
end

function Client:fire(event, ...)
	local frames = {}
	for i, f in ipairs(self.frames) do
		frames[i] = f
	end
	for _, f in ipairs(frames) do
		if f.__s.events[event] then
			local fn = f.__s.scripts.OnEvent
			if fn then
				fn(f, event, ...)
			end
		end
	end
end

function Client:setCombat(inCombat)
	self.combat = inCombat
	self:fire(inCombat and "PLAYER_REGEN_DISABLED" or "PLAYER_REGEN_ENABLED")
end

-- Runs virtual time forward, firing timers and OnUpdate scripts.
function Client:advance(seconds, step)
	step = step or 0.05
	local remaining = seconds
	while remaining > 1e-9 do
		local dt = math.min(step, remaining)
		remaining = remaining - dt
		self.now = self.now + dt
		self:runTimers()
		for _, f in ipairs(self.frames) do
			local s = f.__s
			local fn = s.scripts.OnUpdate
			if fn and s.shown then
				fn(f, dt)
			end
		end
	end
end

function Client:runTimers()
	while true do
		local best
		for _, t in ipairs(self.timers) do
			if not t.cancelled and t.due <= self.now + 1e-6 and (not best or t.due < best.due or (t.due == best.due and t.seq < best.seq)) then
				best = t
			end
		end
		if not best then
			break
		end
		if best.repeating then
			best.due = best.due + best.interval
			if best.left then
				best.left = best.left - 1
				if best.left <= 0 then
					best.cancelled = true
				end
			end
		else
			best.cancelled = true
		end
		best.fn(best.obj)
	end
	local live = {}
	for _, t in ipairs(self.timers) do
		if not t.cancelled then
			live[#live + 1] = t
		end
	end
	self.timers = live
end

-- Types a slash command line, e.g. c:slash("/oc status").
function Client:slash(line)
	local cmd, rest = line:match("^(/%S+)%s*(.*)$")
	local list = self.env
	for i = 1, 9 do
		local alias = rawget(list, "SLASH_OMPCRAFT" .. i)
		if alias == cmd then
			return list.SlashCmdList.OMPCRAFT(rest)
		end
	end
	error("stub: no slash command registered for " .. tostring(cmd))
end

-- The settings page as registered: { name, settings, initializers }.
function Client:settingsCategory()
	return self.settingsCategories and self.settingsCategories[1]
end

-- Changes a setting the way its control would.
function Client:setSetting(variable, value)
	local st = self.env.Settings.GetSetting(variable)
	if not st then
		error("stub: no such setting " .. tostring(variable))
	end
	st:SetValue(value)
end

function Client:getSetting(variable)
	return self.env.Settings.GetSetting(variable):GetValue()
end

function Client:clearChat()
	self.chat = {}
end

function Client:chatText()
	return table.concat(self.chat, "\n")
end

-- Names on the toast frames currently on screen, top first (the agent's name
-- when the toast knows it, else the toast text).
function Client:visibleToasts()
	local out = {}
	for i = 1, 3 do
		local f = rawget(self.env, "OmpcraftToast" .. i)
		if f and f.__s.shown then
			out[#out + 1] = f.name:GetText()
		end
	end
	return out
end

function Client:toastFrame(i)
	return rawget(self.env, "OmpcraftToast" .. i)
end

-- The tracker as drawn: one entry per element, top to bottom, read back from
-- the widgets themselves.
function Client:tracker()
	local out = {}
	for _, item in ipairs(self:ns().Tracker.shown) do
		local e = { kind = item.kind }
		if item.kind == "header" then
			e.text, e.status, e.dotShown = item.text:GetText(), item.status:GetText(), item.dot.__s.shown
		elseif item.kind == "agent" then
			e.name, e.right, e.state = item.title:GetText(), item.elapsed:GetText(), item.state
			e.agent, e.frame = item.agent, item.frame
			e.markerShown, e.marker = item.marker.__s.shown, item.marker.__s
			e.lines = {}
			for _, line in ipairs(item.lines) do
				e.lines[#e.lines + 1] = { text = line.text:GetText(), dash = line.dash:GetText(), pulsing = line.group:IsPlaying(), color = line.text.__s.textColor }
			end
		else
			e.text = item.text:GetText()
			e.collapsed, e.open, e.frame = item.collapsed, item.open, item.frame
			e.name = item.name
		end
		out[#out + 1] = e
	end
	return out
end

-- Texts drawn on the detail page, in order.
function Client:detailTexts()
	local out = {}
	local pool = self:ns().Detail.pool
	for i = 1, pool.fsUsed or 0 do
		local fs = pool.fs[i]
		out[#out + 1] = { text = fs:GetText(), font = fs.__s.font, color = fs.__s.textColor, y = fs.__s.points[1] and fs.__s.points[1].y, x = fs.__s.points[1] and fs.__s.points[1].x }
	end
	return out
end

function Client:detailTextures()
	local out = {}
	local pool = self:ns().Detail.pool
	for i = 1, pool.texUsed or 0 do
		out[#out + 1] = pool.tex[i].__s
	end
	return out
end

function Client:tooltipLines()
	local lines = {}
	for _, l in ipairs(self.env.GameTooltip.__s.lines) do
		lines[#lines + 1] = l[2] and (l[1] .. ": " .. l[2]) or l[1]
	end
	return lines
end

return Stub
