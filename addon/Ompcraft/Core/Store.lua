-- Ompcraft: holds the latest accepted snapshot, derives each agent's kind
-- (needs you, working, finished-unseen, ...) and decides which toasts to show.

local _, ns = ...

local Store = {}
ns.Store = Store

local STALE_AFTER = 300 -- seconds without a fresh snapshot (PROTOCOL 7.7)
local MAX_AGENTS = 200
local MAX_TOASTS = 50
local MAX_LINES = 400 -- detail.lines kept per agent
local MAX_STEPS = 10
local MAX_SEEN = 200 -- saved "seen" entries before dead agents are pruned
local TOAST_REPLAY_SLACK = 30 -- seconds before uiStart that still count as new

Store.STALE_AFTER = STALE_AFTER

-- Order of the agent groups (PROTOCOL 5).
local STATE_RANK = { waiting_input = 1, working = 2, idle = 3, exited = 4 }

local LINE_KINDS = { h = true, p = true, li = true, code = true, quote = true, hr = true, gap = true, more = true, tr = true }

Store.agents = {}
Store.byId = {}
Store.hasSnapshot = false

local function str(v)
	if type(v) == "number" then
		v = tostring(v)
	end
	return ns.EscapePipes(v)
end

local function num(v)
	if type(v) == "number" and v == v then
		return v
	end
	return nil
end

local function cleanDetail(raw)
	if type(raw) ~= "table" then
		return nil
	end
	local lines, steps = {}, {}
	if type(raw.lines) == "table" then
		for i = 1, math.min(#raw.lines, MAX_LINES) do
			local l = raw.lines[i]
			if type(l) == "table" then
				local depth = num(l.d) or 1
				lines[#lines + 1] = {
					k = LINE_KINDS[l.k] and l.k or "p", -- a kind from the future reads as a paragraph
					t = ns.EscapePipes(l.t, true), -- keeps the \1-\4 sentinels
					d = math.max(1, math.min(4, math.floor(depth))),
				}
			end
		end
	end
	if type(raw.steps) == "table" then
		for i = 1, math.min(#raw.steps, MAX_STEPS) do
			local s = raw.steps[i]
			if type(s) == "table" then
				steps[#steps + 1] = { n = str(s.n), t = str(s.t), s = type(s.s) == "string" and s.s or "" }
			end
		end
	end
	if #lines == 0 and #steps == 0 then
		return nil
	end
	return { lines = lines, steps = steps }
end

local function cleanAgent(raw, index)
	if type(raw) ~= "table" then
		return nil
	end
	local state = type(raw.state) == "string" and raw.state or ""
	return {
		id = str(raw.id),
		name = str(raw.name),
		host = str(raw.host),
		session = str(raw.session),
		section = str(raw.section),
		state = state,
		since = num(raw.since),
		model = str(raw.model),
		cwd = str(raw.cwd),
		last = str(raw.last),
		activity = str(raw.activity),
		todo = str(raw.todo),
		stats = str(raw.stats),
		doneAt = num(raw.doneAt) or 0,
		preview = str(raw.preview),
		detail = cleanDetail(raw.detail),
		index = index,
	}
end

local function agentBefore(a, b)
	local ra, rb = STATE_RANK[a.state] or 5, STATE_RANK[b.state] or 5
	if ra ~= rb then
		return ra < rb
	end
	local sa, sb = a.since or 0, b.since or 0
	if sa ~= sb then
		return sa > sb -- newest first
	end
	return a.index < b.index
end

local function cleanAgents(raw)
	local list = {}
	if type(raw) ~= "table" then
		return list
	end
	for i = 1, math.min(#raw, MAX_AGENTS) do
		local a = cleanAgent(raw[i], i)
		if a then
			list[#list + 1] = a
		end
	end
	table.sort(list, agentBefore)
	return list
end

local function cleanToasts(raw)
	local list = {}
	if type(raw) ~= "table" then
		return list
	end
	for i = 1, math.min(#raw, MAX_TOASTS) do
		local t = raw[i]
		local id = type(t) == "table" and num(t.id)
		if id then
			list[#list + 1] = { id = id, kind = type(t.kind) == "string" and t.kind or "", agent = str(t.agent), text = str(t.text) }
		end
	end
	table.sort(list, function(a, b)
		return a.id < b.id
	end)
	return list
end

-- Applies a raw OmpcraftInbox table. Returns true if it was accepted
-- (PROTOCOL 7.3): v == 0 and either a new `run` or a higher `gen`.
function Store:Apply(raw)
	if type(raw) ~= "table" or raw.v ~= 0 then
		return false
	end
	local run, gen = num(raw.run), num(raw.gen)
	if not run or not gen then
		return false
	end
	if self.run ~= nil and run == self.run and gen <= self.gen then
		return false
	end

	-- Build everything first so a malformed snapshot changes nothing.
	local agents = cleanAgents(raw.agents)
	local toasts = cleanToasts(raw.toasts)
	local byId = {}
	for i = 1, #agents do
		byId[agents[i].id] = agents[i]
	end

	self.run, self.gen = run, gen
	self.at = num(raw.at)
	self.session = num(raw.session)
	self.nextSignal = num(raw.nextSignal)
	self.tern = type(raw.tern) == "string" and raw.tern or "down"
	self.companion = str(raw.companion)
	self.agents = agents
	self.byId = byId
	self.hasSnapshot = true
	self.appliedAt = time()

	self:PruneSeen()
	self:Notify(toasts)
	ns:Fire("SNAPSHOT")
	return true
end

-- PROTOCOL 7.6: show toasts newer than lastToast, skip replays from before
-- this UI session, then raise lastToast.
function Store:Notify(toasts)
	local db = ns.db
	local last = db.lastToast
	local newest = last
	local cutoff = ns.uiStart - TOAST_REPLAY_SLACK
	for i = 1, #toasts do
		local t = toasts[i]
		if t.id > last then
			if t.id / 1000 >= cutoff then
				ns:Fire("TOAST", t)
			end
			if t.id > newest then
				newest = t.id
			end
		end
	end
	db.lastToast = newest
end

function Store:Find(id)
	return self.byId[id]
end

------------------------------------------------------------------------
-- Seen tracking (PROTOCOL 8.6)
------------------------------------------------------------------------

-- Finished, not yet looked at: idle, with a finish time newer than the one the
-- player has seen.
function Store:IsUnseen(a)
	return a.state == "idle" and a.doneAt > 0 and a.doneAt > (ns.db.seen[a.id] or 0)
end

-- Marks the agent's current turn as seen. Returns true if that changed anything.
function Store:MarkSeen(a)
	if a.doneAt > 0 and a.doneAt > (ns.db.seen[a.id] or 0) then
		ns.db.seen[a.id] = a.doneAt
		return true
	end
	return false
end

-- Forgets agents that are gone, once the table has grown.
function Store:PruneSeen()
	local seen, count = ns.db.seen, 0
	for _ in pairs(seen) do
		count = count + 1
	end
	if count <= MAX_SEEN then
		return
	end
	for id in pairs(seen) do
		if not self.byId[id] then
			seen[id] = nil
		end
	end
end

-- "needs" | "working" | "unseen" | "idle" | "exited" | "other"
function Store:Kind(a)
	if a.state == "waiting_input" then
		return "needs"
	elseif a.state == "working" then
		return "working"
	elseif a.state == "exited" then
		return "exited"
	elseif a.state == "idle" then
		return self:IsUnseen(a) and "unseen" or "idle"
	end
	return "other"
end

function Store:Counts()
	local c = { needs = 0, working = 0, unseen = 0, total = #self.agents }
	for i = 1, #self.agents do
		local kind = self:Kind(self.agents[i])
		if c[kind] then
			c[kind] = c[kind] + 1
		end
	end
	return c
end

-- Seconds since the companion wrote the current snapshot, or nil.
function Store:Age()
	if not self.hasSnapshot or not self.at then
		return nil
	end
	return time() - self.at
end

function Store:IsStale()
	local age = self:Age()
	return age == nil or age > STALE_AFTER
end
