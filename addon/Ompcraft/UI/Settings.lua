-- Ompcraft: the settings page, in Options > AddOns > Ompcraft, built on Blizzard's
-- Settings API (a vertical-layout category with AddOn settings backed by
-- OmpcraftDB). Every change is applied live. The slash commands edit the same
-- fields and tell the page (Config:Notify) so an open page stays in step.

local _, ns = ...

local Config = {}
ns.Config = Config

local PREFIX = "OMPCRAFT_"

Config.vars = {} -- short key -> Settings variable name, for Notify

local POPUP_NOTE = "Pop-ups always wait until combat ends."

local function round(value, step)
	return math.floor(value / step + 0.5) * step
end

-- A value on the page changed: show or hide, and draw the tracker again.
local function changed()
	ns.Tracker:ConfigChanged()
end

-- Tells an open settings page that a field changed behind its back (a slash
-- command, the header menu).
function Config:Notify(key)
	local Settings = rawget(_G, "Settings")
	local variable = self.vars[key]
	if self.category and variable and Settings and Settings.NotifyUpdate then
		pcall(Settings.NotifyUpdate, variable)
	end
end

-- Opens the page. Blizzard's panel cannot be opened in combat.
function Config:Open()
	local Settings = rawget(_G, "Settings")
	if not (Settings and self.category) then
		ns.Print("the settings page is not available.")
		return false
	end
	if InCombatLockdown() then
		ns.Print("the settings cannot be opened in combat.")
		return false
	end
	local ok, err = pcall(Settings.OpenToCategory, self.category:GetID())
	if not ok then
		ns:RecordError("settings-open", err)
		ns.Print("could not open the settings: " .. ns.EscapePipes(tostring(err)))
		return false
	end
	return true
end

-- Builds and registers the category. Safe to call once; returns true on success.
function Config:Register()
	local Settings = rawget(_G, "Settings")
	if self.category then
		return true
	end
	if not (Settings and Settings.RegisterVerticalLayoutCategory and Settings.RegisterAddOnSetting) then
		return false, "the Settings API is not available"
	end
	local db, Tracker = ns.db, ns.Tracker
	local category, layout = Settings.RegisterVerticalLayoutCategory("Ompcraft")

	local function header(name, tooltip)
		layout:AddInitializer(CreateSettingsListSectionHeaderInitializer(name, tooltip))
	end

	-- A setting stored in db[...][field]; `key` names it for Notify and the variable.
	local function setting(key, tbl, field, varType, label, default)
		local variable = PREFIX .. key:upper()
		Config.vars[key] = variable
		return Settings.RegisterAddOnSetting(category, variable, field, tbl, varType, label, default)
	end

	local function checkbox(key, tbl, field, label, default, tooltip, onChange)
		local s = setting(key, tbl, field, Settings.VarType.Boolean, label, default)
		s:SetValueChangedCallback(onChange or changed)
		Settings.CreateCheckbox(category, s, tooltip)
	end

	local function slider(key, field, label, default, min, max, step, format, tooltip, onChange)
		local s = setting(key, db, field, Settings.VarType.Number, label, default)
		s:SetValueChangedCallback(onChange)
		local options = Settings.CreateSliderOptions(min, max, step)
		local mixin = rawget(_G, "MinimalSliderWithSteppersMixin")
		if mixin then
			options:SetLabelFormatter(mixin.Label.Right, function(value)
				return string.format(format, value)
			end)
		end
		Settings.CreateSlider(category, s, options, tooltip)
	end

	----------------------------------------------------------------------
	header("Placement")
	local position = Settings.RegisterProxySetting(category, PREFIX .. "POSITION", Settings.VarType.String, "Position", "topleft", function()
		return Tracker:PositionMode()
	end, function(value)
		Tracker:SetPositionMode(value)
	end)
	Config.vars.position = PREFIX .. "POSITION"
	position:SetValueChangedCallback(changed)
	local function positionOptions()
		local container = Settings.CreateControlTextContainer()
		container:Add("topleft", "Top left", "Fixed in the top left, below the default player frame.")
		container:Add("tracker", "Under quest objectives", "Sits just below Blizzard's objective tracker, and follows it.")
		container:Add("corner", "Top right", "Fixed in the top right corner of the screen.")
		container:Add("free", "Where I drag it", "Unlocks the tracker so you can drag it anywhere.")
		return container:GetData()
	end
	Settings.CreateDropdown(category, position, positionOptions, "Where the tracker sits. Choosing \"Where I drag it\" unlocks it; the others forget a dragged spot.")

	checkbox("locked", db, "locked", "Lock position", true, "Stops the tracker from being dragged.")
	slider("scale", "scale", "Scale", 1, 0.6, 1.5, 0.05, "%.2f", "The size of the tracker.", function(_, value)
		db.scale = math.floor(value * 100 + 0.5) / 100
		Tracker:ApplyScale()
		changed()
	end)
	slider("maxHeight", "maxHeight", "Max height", 440, 200, 800, 20, "%d px", "The tracker stops at this height and shows \"+N more\".", function(_, value)
		db.maxHeight = round(value, 20)
		changed()
	end)
	layout:AddInitializer(CreateSettingsButtonInitializer("Reset position", "Reset", function()
		Tracker:ResetPosition()
	end, "Forgets a dragged spot: the tracker goes back to the position chosen above.", false))

	----------------------------------------------------------------------
	header("Agent states", "Which agents the tracker lists.")
	checkbox("show_needs", db.show, "needs", "Needs you", true, "Agents waiting for an answer.")
	checkbox("show_working", db.show, "working", "Working", true)
	checkbox("show_unseen", db.show, "unseen", "Finished, not yet seen", true, "Agents that finished a turn you have not looked at. Off: they count as idle.")
	checkbox("show_exited", db.show, "exited", "Exited / failed", true)
	checkbox("show_idle", db.show, "idle", "Idle summary line", true, "The dim \"N idle\" line. Off: idle agents are not listed at all.")

	header("Detail lines", "What shows under an agent's title.")
	checkbox("lines_activity", db.lines, "activity", "Current activity", true)
	checkbox("lines_todo", db.lines, "todo", "Todo progress", true)
	checkbox("lines_stats", db.lines, "stats", "Turn time & cost", true)
	checkbox("lines_preview", db.lines, "preview", "Reply preview", true)

	header("Visibility")
	checkbox("grouped", db, "grouped", "Group by Tern session", true, "Off: one flat list under the Agents header, still ordered needs you, working, finished, exited.")
	checkbox("onlyWhenActive", db, "onlyWhenActive", "Only show the tracker when something needs me or is working", false,
		"Hides it entirely otherwise. /oc show, the header click and the AddOn button still bring it up.")
	checkbox("hideInCombat", db, "hideInCombat", "Hide during combat", false)
	checkbox("hideInInstance", db, "hideInInstance", "Hide in dungeons, raids and PvP", false)

	----------------------------------------------------------------------
	header("Alerts", POPUP_NOTE)
	checkbox("alerts_input", db.alerts, "input", "Pop-up: needs you", true, POPUP_NOTE)
	checkbox("alerts_done", db.alerts, "done", "Pop-up: finished", true, POPUP_NOTE)
	checkbox("alerts_failed", db.alerts, "failed", "Pop-up: failed or exited", true, POPUP_NOTE)
	checkbox("alerts_update", db.alerts, "update", "Pop-up: updates", true, "Notices from ompcraft itself, such as an update. " .. POPUP_NOTE)
	checkbox("alerts_sound", db.alerts, "sound", "Sound", true, "A soft sound with each pop-up.")
	checkbox("alerts_chat", db.alerts, "chat", "Chat line", true, "A line in your chat window with each pop-up. Only you see it.")

	----------------------------------------------------------------------
	header("Link", "How the game hears from the ompcraft companion.")
	layout:AddInitializer(CreateSettingsButtonInitializer("Link status", function()
		return (ns.Link:StatusText())
	end, function()
		ns.PrintStatus()
	end, "The state of the link now. Click to print the details in chat.", false))
	layout:AddInitializer(CreateSettingsButtonInitializer("Link self-test", "Run self-test", function()
		ns.RunSelfTest()
	end, "Checks the doorbell controls again and prints the result in chat.", false))

	Settings.RegisterAddOnCategory(category)
	self.category, self.layout = category, layout
	return true
end

local function register()
	if Config.category then
		return
	end
	local ok, result, why = pcall(Config.Register, Config)
	if not ok then
		ns:RecordError("settings", result)
	elseif not result then
		Config.unavailable = why
	end
end

ns:Listen("LOADED", register)
-- If the Settings API was not ready at ADDON_LOADED, try again at login.
ns:On("PLAYER_LOGIN", register)
