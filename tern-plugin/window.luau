--!strict
-- ompcraft Tern window plugin (protocol v0; see docs/PROTOCOL.md §3 and §8).
-- Every `interval` ms it posts this window's agent list to the ompcraft companion on loopback.
-- The companion answers at once; it never holds a request open (docs/RESEARCH.md §2.5).

local PROTO = 0
local MAX_EVENTS = 100
local REQUEST_TIMEOUT_MS = 2000
local RETRY_MS = 3000
local DETAIL_REFRESH_MS = 2000 -- transcript refresh while an agent is working or waiting
local DETAIL_MESSAGES = 8
local MAX_STEPS = 10
local MAX_REPLY_BYTES = 16384

local vm = string.format("%.0f-%d", tern.now() * 1000, math.random(1, 2147483647))

type Link = { url: string, token: string }

local link: Link? = nil
local interval = 500
local inflight: number? = nil -- id of the request in flight
local inflightAt = 0
local nextRequest = 0
local events: { any } = {}
local status = { leader = false, lastOk = 0, lastError = "not connected yet", lastDetailError = "" }

-- Pane, session and host ids can exceed 2^52: send them as integer strings, never as floats.
local function idString(n: any): string?
	if type(n) == "number" then
		return string.format("%.0f", n)
	elseif type(n) == "string" then
		return n
	end
	return nil
end

local function configDir(): string?
	local xdg = tern.getenv("XDG_CONFIG_HOME")
	if xdg and xdg ~= "" then
		return xdg
	end
	local home = tern.getenv("HOME") or tern.getenv("USERPROFILE")
	if not home or home == "" then
		return nil
	end
	return home .. "/.config"
end

local function readLink(): Link?
	local dir = configDir()
	if not dir then
		status.lastError = "no HOME or XDG_CONFIG_HOME"
		return nil
	end
	local path = dir .. "/ompcraft/link.json"
	local ok, text = pcall(tern.fs.read, path, 4096)
	if not ok then
		status.lastError = "companion not running (no " .. path .. ")"
		return nil
	end
	local okDecode, value = pcall(tern.json.decode, text)
	if not okDecode or type(value) ~= "table" or type(value.url) ~= "string" or type(value.token) ~= "string" then
		status.lastError = "unreadable link.json"
		return nil
	end
	return { url = value.url, token = value.token }
end

local function pushEvent(ev: { [string]: any })
	if #events >= MAX_EVENTS then
		table.remove(events, 1)
	end
	table.insert(events, ev)
end

local function orNull(v: any): any
	if v == nil then
		return tern.json.null
	end
	return v
end

-- Per-pane transcript cache (PROTOCOL §8.1): refetched when the agent's state or last message
-- changes, and at most every DETAIL_REFRESH_MS while it is working or waiting for input.
type DetailCache = { state: string, last: string, at: number, detail: { [string]: any } }
local details: { [string]: DetailCache } = {}

local function toolText(t: { [string]: any }): string
	for _, key in ipairs({ "intent", "title", "name" }) do
		local v = t[key]
		if type(v) == "string" and v ~= "" then
			return v
		end
	end
	return "tool"
end

local function deriveDetail(messages: { any }): { [string]: any }
	local tools: { { [string]: any } } = {}
	local reply: string? = nil
	for _, m in ipairs(messages) do
		local msg = m :: any
		if type(msg.tools) == "table" then
			for _, t in ipairs(msg.tools) do
				if type(t) == "table" then
					table.insert(tools, t)
				end
			end
		end
		if msg.role == "assistant" and type(msg.text) == "string" and msg.text ~= "" then
			reply = msg.text
		end
	end

	local activity: string? = nil
	local todo: string? = nil
	for i = #tools, 1, -1 do
		local t = tools[i]
		if activity == nil and (t.status == "running" or t.status == "pending") then
			activity = toolText(t)
		end
		if todo == nil and t.name == "todo" and type(t.target) == "string" and t.target ~= "" then
			todo = t.target
		end
	end

	local steps = tern.json.array()
	for i = math.max(1, #tools - MAX_STEPS + 1), #tools do
		local t = tools[i]
		table.insert(steps, { name = t.name or "tool", text = toolText(t), status = orNull(t.status) })
	end

	if reply and #reply > MAX_REPLY_BYTES then
		reply = string.sub(reply, 1, MAX_REPLY_BYTES)
	end
	return { activity = orNull(activity), todo = orNull(todo), steps = steps, reply = orNull(reply) }
end

local MAX_FETCHES_PER_SYNC = 4
local fetchBudget = MAX_FETCHES_PER_SYNC

local function detailFor(cx: WindowCx, target: number, pane: string, state: string, last: string): { [string]: any }?
	local cached = details[pane]
	local now = tern.now()
	local active = state == "working" or state == "waiting_input"
	local stale = cached == nil
		or cached.state ~= state
		or cached.last ~= last
		or (active and now - cached.at >= DETAIL_REFRESH_MS)
	if not stale or fetchBudget <= 0 then
		-- Fetches are capped per sync so a window with many agents never runs past the hook's
		-- wall-time budget; the rest refresh on the next syncs.
		return cached and cached.detail or nil
	end
	fetchBudget -= 1
	local ok, messages = pcall(function()
		return cx.agents:transcript(target, { last = DETAIL_MESSAGES })
	end)
	if not ok or type(messages) ~= "table" then
		status.lastDetailError = "transcript " .. pane .. ": " .. tostring(messages)
		return cached and cached.detail or nil
	end
	local detail = deriveDetail(messages)
	details[pane] = { state = state, last = last, at = now, detail = detail }
	return detail
end

local function snapshotAgents(cx: WindowCx): { any }
	fetchBudget = MAX_FETCHES_PER_SYNC
	local out = tern.json.array()
	local present: { [string]: boolean } = {}
	for _, a in ipairs(cx.agents:list()) do
		local raw = a :: any
		local pane = idString(raw.pane) or ""
		present[pane] = true
		local last = type(raw.last_message) == "string" and raw.last_message or ""
		table.insert(out, {
			pane = pane,
			state = raw.state,
			title = orNull(raw.title),
			cwd = orNull(raw.cwd),
			model = orNull(raw.model),
			host = orNull(raw.host),
			session = orNull(idString(raw.session)),
			last_message = orNull(raw.last_message),
			delivery_error = orNull(raw.delivery_error),
			detail = orNull(detailFor(cx, raw.pane, pane, raw.state, last)),
		})
	end
	for pane in pairs(details) do
		if not present[pane] then
			details[pane] = nil
		end
	end
	return out
end

local function snapshotHosts(cx: WindowCx): { any }
	local out = tern.json.array()
	for _, h in ipairs(cx.hosts:list()) do
		table.insert(out, { id = h.id, name = h.name, kind = h.kind, state = h.state })
	end
	return out
end

local function snapshotSessions(cx: WindowCx): { any }
	local out = tern.json.array()
	for _, s in ipairs(cx.sessions:list()) do
		table.insert(out, { id = idString(s.id), name = s.name, host = s.host })
	end
	return out
end

local tick: (cx: WindowCx?) -> ()

local function schedule(ms: number)
	tern.timer(ms, function(cx)
		tick(cx)
	end)
end

local function send(cx: WindowCx)
	if not link then
		link = readLink()
		if not link then
			status.leader = false
			return schedule(RETRY_MS)
		end
	end
	local current = link :: Link

	local okBody, body = pcall(function()
		return {
			v = PROTO,
			window = cx:window_key(),
			vm = vm,
			hosts = snapshotHosts(cx),
			sessions = snapshotSessions(cx),
			agents = snapshotAgents(cx),
			events = (function()
				local list = tern.json.array()
				for _, ev in ipairs(events) do
					table.insert(list, ev)
				end
				return list
			end)(),
		}
	end)
	if not okBody then
		status.lastError = "reading agents failed: " .. tostring(body)
		return schedule(RETRY_MS)
	end
	local sentEvents = #events

	nextRequest += 1
	local id = nextRequest
	inflight = id
	inflightAt = tern.now()

	tern.fetch(current.url .. "/v1/sync", {
		method = "POST",
		headers = { Authorization = "Bearer " .. current.token, ["Content-Type"] = "application/json" },
		body = tern.json.encode(body),
		timeout_ms = REQUEST_TIMEOUT_MS,
	}, function(res: FetchResult, replyCx: WindowCx?)
		if inflight ~= id then
			return -- a late answer to a request we already gave up on
		end
		inflight = nil
		if res.error or res.status ~= 200 then
			status.lastError = res.error or ("HTTP " .. tostring(res.status))
			status.leader = false
			link = nil -- the companion may have restarted with a new port or token
			interval = RETRY_MS
			return
		end
		-- Drop exactly the events this request carried; newer ones stay queued.
		for _ = 1, math.min(sentEvents, #events) do
			table.remove(events, 1)
		end
		local okDecode, reply = pcall(tern.json.decode, res.body)
		if okDecode and type(reply) == "table" then
			status.leader = reply.leader == true
			if type(reply.interval_ms) == "number" and reply.interval_ms >= 100 then
				interval = reply.interval_ms
			end
			-- A one-shot notice from the companion (e.g. "ompcraft updated"), shown once.
			local notice = reply.notice
			if replyCx and type(notice) == "table" and type(notice.text) == "string" then
				local level = (notice.level == "success" or notice.level == "error") and notice.level or "info"
				local sub = type(notice.sub) == "string" and notice.sub or nil
				pcall(function()
					(replyCx :: WindowCx):toast(level, notice.text, sub)
				end)
			end
		end
		status.lastOk = tern.now()
		status.lastError = ""
	end)
	schedule(interval)
end

tick = function(cx: WindowCx?)
	if not cx then
		return schedule(interval)
	end
	if inflight ~= nil then
		if tern.now() - inflightAt < REQUEST_TIMEOUT_MS + 1000 then
			return schedule(interval)
		end
		inflight = nil -- the callback never came; give up on it and send fresh data
	end
	send(cx)
end

tern.on("agent_finished", function(ev: AgentFinishedEvent, _cx: WindowCx)
	pushEvent({ t = "finished", pane = idString(ev.pane), result = ev.result })
end)

tern.on("agent_state", function(ev: AgentStateEvent, _cx: WindowCx)
	pushEvent({ t = "state", pane = idString(ev.pane), old = orNull(ev.old), new = ev.new })
end)

tern.command({
	id = "status",
	title = "ompcraft: link status",
	run = function(cx: WindowCx)
		if status.lastError == "" then
			local role = status.leader and "sending agent status" or "standing by (another window is sending)"
			if status.lastDetailError ~= "" then
				role = role .. "; last transcript error: " .. status.lastDetailError
			end
			cx:toast("success", "ompcraft companion connected", role)
		else
			cx:toast("error", "ompcraft companion not connected", status.lastError)
		end
	end,
})

-- window_start does not fire again on `tern plugin reload`; a top-level timer does, with a fresh cx.
schedule(0)
