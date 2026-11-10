-- Ompcraft: /oc and /ompcraft, plus the addon compartment hooks.

local _, ns = ...

local say = ns.Print

local HELP = {
	"/oc                toggle the tracker",
	"/oc show | hide    show or hide the tracker",
	"/oc lock | unlock  stop or allow dragging the tracker (locked by default)",
	"/oc reset          forget a dragged spot (back to the chosen Position)",
	"/oc scale <0.6-1.5> set the tracker size (default 1)",
	"/oc status         link mode, slots, snapshot and self-test",
	"/oc selftest       re-check the link controls and print the result",
	"/oc errors         recent Lua errors caught by Ompcraft",
	"/oc config         open the settings (also /oc settings)",
}

local function yesno(v)
	return v and "yes" or "no"
end

local function describeSelfTest(t)
	if not t then
		return "not run"
	end
	return string.format("%s (ctl_present found: %s, ctl_absent found: %s)", t.ok and "pass" or "FAIL", yesno(t.present), yesno(t.absent))
end

local function printStatus()
	local Link, Store = ns.Link, ns.Store
	local mode, why = Link:Mode()
	local text = Link:StatusText()
	say(string.format("link: %s (%s)", text, Link.state))
	say(string.format("mode: %s%s", mode, why and (" - " .. why) or ""))
	say(string.format("slots: %d/%d used", Link.loads, Link.slots))
	say(string.format("nextSignal: %s of %d", Link.nextSignal and tostring(Link.nextSignal) or "none", Link.signals))
	if Store.hasSnapshot then
		local age = Store:Age()
		say(string.format("snapshot: gen %s, run %s, age %s", tostring(Store.gen), tostring(Store.run), age and (ns.FormatElapsed(age)) or "unknown"))
	else
		say("snapshot: none yet")
	end
	say("self-test: " .. describeSelfTest(Link.selfTest))
	say(string.format("doorbells: %d, timer loads: %d, false rings: %d", Link.doorbellsSeen, Link.timerLoads, Link.falseRingTotal))
	local T = ns.Tracker
	if T and T.frame then
		local left = ns.PlainNumber(T.frame.GetLeft, T.frame)
		local top = ns.PlainNumber(T.frame.GetTop, T.frame)
		say(string.format("position: %s at %s, %s", tostring(T.anchorMode), left and string.format("%.0f", left) or "?",
			top and string.format("%.0f", top) or "?"))
	end
	if Link.lastLoadError then
		say("last load error: " .. ns.EscapePipes(Link.lastLoadError))
	end
end

ns.PrintStatus = printStatus

-- Re-runs the link controls and prints the result (the slash command and the
-- settings button).
function ns.RunSelfTest()
	say("self-test: " .. describeSelfTest(ns.Link:Retest()))
end

local function printErrors()
	if #ns.errors == 0 then
		say("no errors recorded.")
		return
	end
	say(string.format("%d error(s) recorded, newest last:", ns.errorCount))
	for i = 1, #ns.errors do
		local e = ns.errors[i]
		local times = e.count > 1 and (" x" .. e.count) or ""
		say(string.format("[%s]%s %s", e.where, times, ns.EscapePipes(e.msg)))
	end
end

local commands = {
	show = function()
		ns.Tracker:SetOpen(true)
	end,
	hide = function()
		ns.Tracker:SetOpen(false)
	end,
	lock = function()
		ns.Tracker:SetLocked(true)
		say("tracker locked.")
	end,
	unlock = function()
		ns.Tracker:SetLocked(false)
		say("tracker unlocked; drag it to move.")
	end,
	reset = function()
		ns.Tracker:ResetPosition()
		say("tracker position reset.")
	end,
	scale = function(arg)
		local scale = tonumber(arg)
		local lo, hi = ns.Tracker.MIN_SCALE, ns.Tracker.MAX_SCALE
		if not scale or scale < lo or scale > hi then
			say(string.format("usage: /oc scale <%s-%s> (now %s)", lo, hi, tostring(ns.db.scale)))
			return
		end
		ns.Tracker:SetScale(scale)
		say("tracker scale " .. tostring(ns.db.scale) .. ".")
	end,
	status = printStatus,
	selftest = function()
		ns.RunSelfTest()
	end,
	config = function()
		ns.Config:Open()
	end,
	settings = function()
		ns.Config:Open()
	end,
	errors = printErrors,
	help = function()
		for i = 1, #HELP do
			say(HELP[i])
		end
	end,
}

SLASH_OMPCRAFT1 = "/oc"
SLASH_OMPCRAFT2 = "/ompcraft"
SlashCmdList["OMPCRAFT"] = ns.Guard("slash", function(msg)
	local cmd, arg = (msg or ""):match("^%s*(%S*)%s*(.-)%s*$")
	cmd = cmd:lower()
	if cmd == "" then
		ns.Tracker:Toggle()
	elseif commands[cmd] then
		commands[cmd](arg)
	else
		say("unknown command; try /oc help")
	end
end)

------------------------------------------------------------------------
-- Addon compartment (TOC: AddonCompartmentFunc*). Called as
-- (addonName, buttonName) for clicks and (addonName, button) for hover.
------------------------------------------------------------------------

function Ompcraft_OnAddonCompartmentClick(_, buttonName)
	ns.Guard("compartment-click", function()
		if buttonName == "RightButton" then
			ns.Config:Open()
		else
			ns.Tracker:Toggle()
		end
	end)()
end

function Ompcraft_OnAddonCompartmentEnter(_, button)
	ns.Guard("compartment-enter", function()
		local counts = ns.Store:Counts()
		GameTooltip:SetOwner(button, "ANCHOR_LEFT")
		GameTooltip:SetText("Ompcraft", 1, 0.82, 0)
		if ns.Store.hasSnapshot then
			GameTooltip:AddDoubleLine("Needs you", tostring(counts.needs), 1, 0.69, 0, 1, 1, 1)
			GameTooltip:AddDoubleLine("Working", tostring(counts.working), 0.6, 0.6, 0.6, 1, 1, 1)
			GameTooltip:AddDoubleLine("Finished, unseen", tostring(counts.unseen), 0.3, 0.85, 0.4, 1, 1, 1)
		end
		GameTooltip:AddDoubleLine("Link", (ns.Link:StatusText()), 0.6, 0.6, 0.6, 1, 1, 1)
		GameTooltip:AddLine("Click: show or hide the tracker", 0.6, 0.6, 0.6)
		GameTooltip:AddLine("Right-click: settings", 0.6, 0.6, 0.6)
		GameTooltip:Show()
	end)()
end

function Ompcraft_OnAddonCompartmentLeave()
	GameTooltip:Hide()
end
