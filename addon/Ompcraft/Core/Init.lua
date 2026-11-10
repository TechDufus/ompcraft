-- Ompcraft: namespace, event dispatcher, error ring buffer, saved variables.
--
-- Forever hides Lua errors by default, so every handler, timer and script
-- callback runs under pcall and failures land in a small ring buffer that
-- "/oc errors" prints.

local addonName, ns = ...

ns.addonName = addonName
ns.PREFIX = "|cff7fbfffompcraft|r:"
ns.uiStart = 0 -- unix seconds, set at ADDON_LOADED

------------------------------------------------------------------------
-- Error ring buffer
------------------------------------------------------------------------

local MAX_ERRORS = 10
ns.errors = {}
ns.errorCount = 0

function ns:RecordError(where, err)
	err = tostring(err)
	self.errorCount = self.errorCount + 1
	local last = self.errors[#self.errors]
	if last and last.where == where and last.msg == err then
		last.count = last.count + 1
		last.at = time()
		return
	end
	self.errors[#self.errors + 1] = { where = where, msg = err, at = time(), count = 1 }
	if #self.errors > MAX_ERRORS then
		table.remove(self.errors, 1)
	end
end

-- Wraps fn so an error is recorded instead of thrown.
function ns.Guard(where, fn)
	return function(...)
		local ok, err = pcall(fn, ...)
		if not ok then
			ns:RecordError(where, err)
		end
	end
end

------------------------------------------------------------------------
-- WoW events and internal messages
------------------------------------------------------------------------

local frame = CreateFrame("Frame")
ns.frame = frame
local eventHandlers = {}
local messageHandlers = {}

function ns:On(event, fn)
	local list = eventHandlers[event]
	if not list then
		list = {}
		eventHandlers[event] = list
		frame:RegisterEvent(event)
	end
	list[#list + 1] = fn
end

frame:SetScript("OnEvent", function(_, event, ...)
	local list = eventHandlers[event]
	if not list then
		return
	end
	for i = 1, #list do
		local ok, err = pcall(list[i], ...)
		if not ok then
			ns:RecordError(event, err)
		end
	end
end)

-- Internal pub/sub between modules.
function ns:Listen(message, fn)
	local list = messageHandlers[message]
	if not list then
		list = {}
		messageHandlers[message] = list
	end
	list[#list + 1] = fn
end

function ns:Fire(message, ...)
	local list = messageHandlers[message]
	if not list then
		return
	end
	for i = 1, #list do
		local ok, err = pcall(list[i], ...)
		if not ok then
			ns:RecordError("msg:" .. message, err)
		end
	end
end

------------------------------------------------------------------------
-- Saved variables
------------------------------------------------------------------------

local DB_VERSION = 4

-- Fills defaults and repairs wrong types. Never discards a newer db.
function ns.MigrateDB(db)
	if type(db) ~= "table" then
		db = {}
	end
	local old = type(db.version) == "number" and db.version or 0
	if old < 2 then
		-- v2 replaced the boxed panel with the tracker: its old two-line and
		-- idle switches are gone, it is locked by default, and it runs at 1.0.
		db.detail, db.showIdle = nil, nil
		db.locked = true
		db.scale = 1
	end
	if old < 3 then
		-- v3: the tracker anchors under Blizzard's objectives. A position saved by the
		-- boxed panel (or by v2 inheriting it) is not one the player chose for the tracker.
		db.pos = nil
	end
	if type(db.shown) ~= "boolean" then
		db.shown = true
	end
	if type(db.collapsed) ~= "boolean" then
		db.collapsed = false
	end
	if type(db.locked) ~= "boolean" then
		db.locked = true
	end
	if type(db.scale) ~= "number" or db.scale < 0.6 or db.scale > 1.5 then
		db.scale = 1
	end
	if type(db.lastToast) ~= "number" then
		db.lastToast = 0
	end
	-- agent id -> doneAt of the turn the player has seen (PROTOCOL 8.6)
	if type(db.seen) ~= "table" then
		db.seen = {}
	end
	-- section name -> true
	if type(db.collapsedSections) ~= "table" then
		db.collapsedSections = {}
	end
	-- the one "N idle" line is open (earlier builds kept a table per section)
	if type(db.idleOpen) ~= "boolean" then
		db.idleOpen = false
	end
	-- v4: settings. Placement, what to show, alerts. Everything the player chose
	-- before (lock, scale, a dragged position, the idle line) is left as it was.
	if db.anchor ~= "tracker" and db.anchor ~= "corner" and db.anchor ~= "topleft" then
		-- where the tracker sits while there is no dragged position: a new install
		-- starts top left; an older save keeps the placement it has always had
		db.anchor = old == 0 and "topleft" or "tracker"
	end
	local size = db.detailSize
	if size ~= nil and (type(size) ~= "table" or type(size.w) ~= "number" or type(size.h) ~= "number"
		or size.w < 340 or size.w > 760 or size.h < 360 or size.h > 900) then
		db.detailSize = nil -- the agent page's remembered size
	end
	if type(db.maxHeight) ~= "number" or db.maxHeight < 200 or db.maxHeight > 800 then
		db.maxHeight = 440
	end
	for key, default in pairs({ grouped = true, onlyWhenActive = false, hideInCombat = false, hideInInstance = false }) do
		if type(db[key]) ~= "boolean" then
			db[key] = default
		end
	end
	local function group(name, keys)
		if type(db[name]) ~= "table" then
			db[name] = {}
		end
		for _, key in ipairs(keys) do
			if type(db[name][key]) ~= "boolean" then
				db[name][key] = true
			end
		end
	end
	group("show", { "needs", "working", "unseen", "exited", "idle" }) -- agent states
	group("lines", { "activity", "todo", "stats", "preview" }) -- objective lines
	group("alerts", { "input", "done", "failed", "update", "sound", "chat" })
	local pos = db.pos
	if pos ~= nil then
		if type(pos) ~= "table" or type(pos.point) ~= "string" or type(pos.relPoint) ~= "string"
			or type(pos.x) ~= "number" or type(pos.y) ~= "number" then
			db.pos = nil
		end
	end
	if old < DB_VERSION then
		db.version = DB_VERSION
	end
	return db
end

ns:On("ADDON_LOADED", function(name)
	if name ~= addonName then
		return -- also fires for every slot addon the link loads
	end
	ns.uiStart = time()
	OmpcraftDB = ns.MigrateDB(OmpcraftDB)
	ns.db = OmpcraftDB
	ns:Fire("LOADED")
end)
