-- Ompcraft: the downlink from the companion (PROTOCOL 7).
--
-- The companion writes a snapshot into every load-on-demand slot addon
-- (OmpcraftS000...) that has not been loaded yet, then creates a doorbell
-- sound file. The addon probes doorbells with PlaySoundFile, and loads the
-- next slot when one rings. Where doorbells cannot be trusted it loads a slot
-- on a timer instead.

local _, ns = ...

local Link = {}
ns.Link = Link

local SLOT_FORMAT = "OmpcraftS%03d"
local SIGNALS_ADDON = "OmpcraftSignals"
local SIGNAL_PATH = "Interface\\AddOns\\OmpcraftSignals\\%s.wav"

local TICK = 0.25 -- doorbell probe / scheduler period
local BOOTSTRAP_DELAY = 2 -- seconds after PLAYER_LOGIN
local TRUST_WINDOW = 15 -- |session - uiStart| in seconds (PROTOCOL 7.3)
local GAP_SHOWN, GAP_HIDDEN = 1, 3 -- min seconds between loads, doorbell mode
local TIMER_SHOWN, TIMER_HIDDEN = 5, 30 -- base seconds between loads, timer mode
local TIMER_SHOWN_MAX, TIMER_HIDDEN_MAX = 60, 120 -- back-off caps
local FALSE_RING_LIMIT = 2

Link.SLOT_FORMAT = SLOT_FORMAT
Link.OUT_OF_SLOTS_TEXT = "out of link slots \226\128\148 /reload to refresh"

-- state: "init" | "ok" | "not_installed" | "out_of_slots"
Link.state = "init"
Link.slots = 0
Link.signals = 0
Link.cursor = 0 -- every slot below this index is loaded or skipped
Link.skipped = {}
Link.loads = 0
Link.falseRings = 0 -- consecutive; drives the guard
Link.falseRingTotal = 0
Link.doorbellsSeen = 0
Link.timerLoads = 0
Link.timerMult = 1 -- timer back-off multiplier
Link.trusted = false
Link.bootstrapped = false
Link.timerReason = nil -- sticky: "selftest" | "false-ring"
Link.selfTest = nil

local function slotName(i)
	return string.format(SLOT_FORMAT, i)
end

Link.SlotName = slotName

-- True iff PlaySoundFile reports the file present (and stops the sound).
-- PROTOCOL 2.3.
-- Adapted from ForeverPlusPlus (MIT): ForeverPlusPlus_AgentChat/AgentChat.lua signalThere
local function probe(file)
	local ok, willPlay, handle = pcall(PlaySoundFile, string.format(SIGNAL_PATH, file), "Master")
	if not ok or not willPlay then
		return false
	end
	if handle then
		pcall(StopSound, handle)
	end
	return true
end

local function metaNumber(field)
	local ok, value = pcall(C_AddOns.GetAddOnMetadata, SIGNALS_ADDON, field)
	if ok then
		return tonumber(value)
	end
	return nil
end

function Link:SetState(state)
	if self.state == state then
		return
	end
	self.state = state
	if state == "out_of_slots" then
		ns.Print(self.OUT_OF_SLOTS_TEXT)
	end
	ns:Fire("LINK")
end

-- PROTOCOL 7.1 self-test: ctl_present must read present and ctl_absent absent.
function Link:SelfTest()
	local present = probe("ctl_present")
	local absent = probe("ctl_absent")
	local ok = present and not absent
	self.selfTest = { present = present, absent = absent, ok = ok, at = time() }
	if not ok then
		self.timerReason = self.timerReason or "selftest"
	elseif self.timerReason == "selftest" then
		self.timerReason = nil
	end
	return self.selfTest
end

-- Reads the installed link addons and runs the self-test. Safe to repeat.
function Link:Setup()
	local ok, exists = pcall(C_AddOns.DoesAddOnExist, SIGNALS_ADDON)
	if not ok or not exists then
		self.notInstalledReason = "OmpcraftSignals missing"
		self:SetState("not_installed")
		return
	end
	local slots = metaNumber("X-Ompcraft-Slots")
	self.signals = metaNumber("X-Ompcraft-Signals") or 0
	if not slots or slots < 1 then
		self.notInstalledReason = "bad X-Ompcraft-Slots"
		self:SetState("not_installed")
		return
	end
	self.slots = slots
	self.notInstalledReason = nil
	self:SelfTest()
	if self.state ~= "out_of_slots" then
		self:SetState("ok")
	end
end

-- "doorbell" or "timer", and why it is a timer.
function Link:Mode()
	if self.timerReason then
		return "timer", self.timerReason
	end
	if self.signals < 1 then
		return "timer", "no-signals"
	end
	if self.nextSignal == nil then
		return "timer", "no-snapshot"
	end
	if not self.trusted then
		return "timer", "untrusted"
	end
	if self.nextSignal >= self.signals then
		return "timer", "signals-exhausted"
	end
	return "doorbell"
end

local function panelOpen()
	return ns.Tracker ~= nil and ns.Tracker:IsOpen()
end

-- Index of the lowest slot not yet loaded, or nil (PROTOCOL 7.2).
function Link:NextFree()
	local i = self.cursor
	while i < self.slots and (self.skipped[i] or C_AddOns.IsAddOnLoaded(slotName(i))) do
		i = i + 1
	end
	self.cursor = i
	if i >= self.slots then
		return nil
	end
	return i
end

-- Loads the next free slot and applies what it carries. `why` is
-- "bootstrap", "doorbell" or "timer". Returns true if a slot was loaded.
function Link:LoadNext(why)
	if self.state ~= "ok" then
		return false
	end
	local index = self:NextFree()
	if not index then
		self:SetState("out_of_slots")
		return false
	end
	local name = slotName(index)

	OmpcraftInbox = nil
	local loaded, reason = C_AddOns.LoadAddOn(name)
	local snapshot = OmpcraftInbox
	OmpcraftInbox = nil

	self.lastLoadAt = GetTime()
	self.lastLoadWhy = why
	self.loads = self.loads + 1
	if not loaded then
		-- Do not spin on a slot the client cannot load.
		self.skipped[index] = true
		self.lastLoadError = name .. ": " .. tostring(reason)
		snapshot = nil
	end

	local accepted = false
	if snapshot ~= nil then
		local ok, result = pcall(ns.Store.Apply, ns.Store, snapshot)
		if ok then
			accepted = result
		else
			ns:RecordError("apply", result)
		end
	end

	if why == "timer" then
		self.timerLoads = self.timerLoads + 1
	end
	if accepted then
		local Store = ns.Store
		self.nextSignal = Store.nextSignal
		self.trusted = Store.session ~= nil and math.abs(Store.session - ns.uiStart) <= TRUST_WINDOW
		self.falseRings = 0
		self.timerMult = 1
	else
		if why == "timer" then
			self.timerMult = math.min(self.timerMult * 2, 64) -- 64 * 5 s is past both caps
		elseif why == "doorbell" then
			self.falseRings = self.falseRings + 1
			self.falseRingTotal = self.falseRingTotal + 1
			if self.falseRings >= FALSE_RING_LIMIT and not self.timerReason then
				self.timerReason = "false-ring"
				ns.Print("doorbells rang without news; switching to timer mode for this session.")
			end
		end
	end

	if not self:NextFree() then
		self:SetState("out_of_slots")
	else
		ns:Fire("LINK")
	end
	return true
end

-- Seconds between timer loads: the base doubles after every timer load that
-- brought nothing newer, up to a cap; it resets on news and when the panel opens.
function Link:TimerInterval(open)
	local base, cap = TIMER_SHOWN, TIMER_SHOWN_MAX
	if not open then
		base, cap = TIMER_HIDDEN, TIMER_HIDDEN_MAX
	end
	return math.min(base * self.timerMult, cap)
end

function Link:ResetBackoff()
	self.timerMult = 1
end

-- Runs every TICK seconds.
function Link:Tick()
	if self.state ~= "ok" or not self.bootstrapped then
		return
	end
	local open = panelOpen()
	local since = GetTime() - (self.lastLoadAt or -math.huge)
	if self:Mode() == "doorbell" then
		if since < (open and GAP_SHOWN or GAP_HIDDEN) then
			return
		end
		if probe(string.format("%04d", self.nextSignal)) then
			self.doorbellsSeen = self.doorbellsSeen + 1
			self:LoadNext("doorbell")
		end
	elseif since >= self:TimerInterval(open) then
		self:LoadNext("timer")
	end
end

-- Starts the scheduler once the link is usable.
function Link:EnsureRunning()
	if self.state ~= "ok" then
		return
	end
	if not self.ticker then
		self.ticker = C_Timer.NewTicker(TICK, ns.Guard("link-tick", function()
			Link:Tick()
		end))
	end
	if not self.bootstrapScheduled then
		self.bootstrapScheduled = true
		C_Timer.After(BOOTSTRAP_DELAY, ns.Guard("link-bootstrap", function()
			Link.bootstrapped = true
			Link:LoadNext("bootstrap")
		end))
	end
end

function Link:Start()
	self:Setup()
	if self.state == "not_installed" then
		ns.Print("companion not installed (no OmpcraftSignals addon found).")
		return
	end
	self:EnsureRunning()
end

-- Re-runs the install check and controls (for "/oc selftest").
function Link:Retest()
	local before = self.state
	self:Setup()
	if before == "not_installed" and self.state == "ok" then
		self:EnsureRunning()
	end
	return self.selfTest
end

-- Short link status for the panel header: text and tone.
function Link:StatusText()
	if self.state == "not_installed" then
		return "not installed", "bad"
	elseif self.state == "out_of_slots" then
		return "out of slots", "bad"
	end
	local Store = ns.Store
	if Store:IsStale() then
		return "stale", "warn"
	elseif Store.tern ~= "ok" then
		return "Tern down", "warn"
	elseif self:Mode() == "timer" then
		return "timer", "info"
	end
	return "live", "good"
end

-- A line that explains a broken link, or nil.
function Link:Message()
	if self.state == "not_installed" then
		return "companion not installed"
	elseif self.state == "out_of_slots" then
		return self.OUT_OF_SLOTS_TEXT
	end
	return nil
end

ns:On("PLAYER_LOGIN", function()
	Link:Start()
end)
