-- Test fixtures: a fake AddOns folder in a temp dir and a tiny stand-in for
-- the companion that publishes snapshots into it the way PROTOCOL 6 says.
-- All data is invented ("host-a", "demo-session", ...).

local Fx = {}
Fx.__index = Fx

------------------------------------------------------------------------
-- data.lua serializer: literals only, escaped as PROTOCOL 5 describes
------------------------------------------------------------------------

local function encString(s)
	local out = {}
	for i = 1, #s do
		local b = s:byte(i)
		if b == 92 then
			out[#out + 1] = "\\\\"
		elseif b == 34 then
			out[#out + 1] = '\\"'
		elseif b == 10 then
			out[#out + 1] = "\\n"
		elseif b == 124 then
			out[#out + 1] = "||" -- "|" is doubled so markup cannot be injected
		elseif b >= 32 and b <= 126 then
			out[#out + 1] = string.char(b)
		else
			out[#out + 1] = string.format("\\%03d", b)
		end
	end
	return '"' .. table.concat(out) .. '"'
end

local KEY_ORDER = {
	"v", "gen", "run", "at", "session", "nextSignal", "tern", "companion", "agents", "toasts",
	"id", "name", "host", "state", "since", "model", "cwd", "last", "kind", "agent", "text",
}
local RANK = {}
for i, k in ipairs(KEY_ORDER) do
	RANK[k] = i
end

local function isArray(t)
	return #t > 0 or next(t) == nil
end

local function enc(v, indent)
	local t = type(v)
	if t == "string" then
		return encString(v)
	elseif t == "number" then
		if v == math.floor(v) then
			return string.format("%.0f", v)
		end
		return string.format("%.17g", v)
	elseif t == "boolean" then
		return tostring(v)
	elseif t == "table" then
		local pad = string.rep("  ", indent + 1)
		local lines = {}
		if isArray(v) then
			for _, x in ipairs(v) do
				lines[#lines + 1] = pad .. enc(x, indent + 1) .. ","
			end
		else
			local keys = {}
			for k in pairs(v) do
				keys[#keys + 1] = k
			end
			table.sort(keys, function(a, b)
				local ra, rb = RANK[a] or 1000, RANK[b] or 1000
				if ra ~= rb then
					return ra < rb
				end
				return a < b
			end)
			for _, k in ipairs(keys) do
				lines[#lines + 1] = pad .. k .. " = " .. enc(v[k], indent + 1) .. ","
			end
		end
		if #lines == 0 then
			return "{}"
		end
		return "{\n" .. table.concat(lines, "\n") .. "\n" .. string.rep("  ", indent) .. "}"
	end
	error("cannot serialize " .. t)
end

function Fx.serialize(snapshot)
	return "OmpcraftInbox = " .. enc(snapshot, 0) .. "\n"
end

------------------------------------------------------------------------
-- fake AddOns folder
------------------------------------------------------------------------

local SLOT_TOC = table.concat({
	"## Interface: 16001",
	"## Title: |cff808080Ompcraft link slot|r",
	"## Notes: Data written by the ompcraft companion.",
	"## Group: Ompcraft",
	"## LoadOnDemand: 1",
	"data.lua",
	"",
}, "\n")

local WAV = "RIFF\36\0\0\0WAVEfmt \16\0\0\0\1\0\1\0\68\172\0\0\136\88\1\0\2\0\16\0data\0\0\0\0"

local function write(path, content)
	local f = io.open(path, "wb")
	if not f then
		return false
	end
	f:write(content)
	f:close()
	return true
end

local function slotName(i)
	return string.format("OmpcraftS%03d", i)
end

-- opts: slots (6), signals (40), install (true), ctlPresent (true), ctlAbsent (false)
function Fx.new(opts)
	opts = opts or {}
	local self = setmetatable({}, Fx)
	local dir = os.tmpname()
	os.remove(dir)
	assert(dir:match("^/tmp/"), "unexpected tmp path")
	self.dir = dir
	self.slots = opts.slots or 6
	self.signals = opts.signals or 40
	-- companion state
	self.run = 1762727000123
	self.gen = 0
	self.signalNext = 0
	self.session = 0
	self.tern = "ok"
	self.agents = opts.agents or {}
	self.toasts = opts.toasts or {}

	local dirs = { dir }
	if opts.install ~= false then
		dirs[#dirs + 1] = dir .. "/OmpcraftSignals"
		for i = 0, self.slots - 1 do
			dirs[#dirs + 1] = dir .. "/" .. slotName(i)
		end
	end
	assert(os.execute("mkdir -p '" .. table.concat(dirs, "' '") .. "'") == 0)

	if opts.install ~= false then
		write(dir .. "/OmpcraftSignals/OmpcraftSignals.toc", table.concat({
			"## Interface: 16001",
			"## Title: |cff808080Ompcraft link signals|r",
			"## Notes: Doorbell files written by the ompcraft companion.",
			"## Group: Ompcraft",
			"## LoadOnDemand: 1",
			"## X-Ompcraft-Link: 0",
			"## X-Ompcraft-Slots: " .. self.slots,
			"## X-Ompcraft-Signals: " .. self.signals,
			"",
		}, "\n"))
		if opts.ctlPresent ~= false then
			self:addControl()
		end
		if opts.ctlAbsent then
			write(dir .. "/OmpcraftSignals/ctl_absent.wav", WAV)
		end
		for i = 0, self.slots - 1 do
			write(dir .. "/" .. slotName(i) .. "/" .. slotName(i) .. ".toc", SLOT_TOC)
			write(dir .. "/" .. slotName(i) .. "/data.lua", "OmpcraftInbox = nil\n")
		end
	end
	return self
end

function Fx:addControl()
	write(self.dir .. "/OmpcraftSignals/ctl_present.wav", WAV)
end

function Fx:removeSlot(i)
	assert(self.dir:match("^/tmp/"))
	os.execute("rm -rf '" .. self.dir .. "/" .. slotName(i) .. "'")
end

function Fx:cleanup()
	assert(self.dir:match("^/tmp/"))
	os.execute("rm -rf '" .. self.dir .. "'")
end

function Fx:ring(n)
	if n < self.signals then
		write(string.format("%s/OmpcraftSignals/%04d.wav", self.dir, n), WAV)
	end
end

function Fx:writeSlot(i, text)
	return write(self.dir .. "/" .. slotName(i) .. "/data.lua", text)
end

-- PROTOCOL 6.2: write the snapshot into every slot the client has not loaded
-- in this UI session, then ring the doorbell the previous snapshot named.
-- `override` is merged over the snapshot (use it to inject odd values).
function Fx:publish(client, override)
	self.gen = self.gen + 1
	local snap = {
		v = 0,
		gen = self.gen,
		run = self.run,
		at = client:time(),
		session = self.session,
		nextSignal = self.signalNext + 1,
		tern = self.tern,
		companion = "0.1.0",
		agents = self.agents,
		toasts = self.toasts,
	}
	for k, v in pairs(override or {}) do
		snap[k] = v
	end
	local text = Fx.serialize(snap)
	for i = 0, self.slots - 1 do
		if not client.loaded[slotName(i)] then
			self:writeSlot(i, text)
		end
	end
	self:ring(self.signalNext)
	self.signalNext = self.signalNext + 1
	return snap
end

-- PROTOCOL 6.3: the companion saw a UI session start now.
function Fx:sessionStart(client, offset)
	self.session = client:time() + (offset or 0)
	return self:publish(client)
end

return Fx
