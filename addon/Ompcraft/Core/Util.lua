-- Ompcraft: small pure helpers (formatting, text safety, chat output).

local _, ns = ...

local format, floor, min = string.format, math.floor, math.min

-- "45s", "12m", "3h", "2d".
function ns.FormatElapsed(seconds)
	seconds = tonumber(seconds) or 0
	if seconds < 0 then
		seconds = 0
	end
	seconds = floor(seconds)
	if seconds < 60 then
		return format("%ds", seconds)
	elseif seconds < 3600 then
		return format("%dm", floor(seconds / 60))
	elseif seconds < 86400 then
		return format("%dh", floor(seconds / 3600))
	end
	return format("%dd", floor(seconds / 86400))
end

-- The companion doubles every "|" ("||" renders as one "|"). This makes the
-- same true of anything that arrives otherwise, and drops control characters
-- (other than newline), so a snapshot string can never form a markup escape.
-- Tabs become four spaces first: the game font has no tab glyph, and code
-- indentation must survive. With `keepMarks`, the inline sentinels \1 \2 \3 \4
-- (PROTOCOL 8.3) survive.
function ns.EscapePipes(s, keepMarks)
	if type(s) ~= "string" then
		return ""
	end
	s = s:gsub("\t", "    ")
	if keepMarks then
		s = s:gsub("[%z\5-\9\11-\31\127]", "")
	else
		s = s:gsub("[%z\1-\9\11-\31\127]", "")
	end
	s = s:gsub("||", "\5")
	s = s:gsub("|", "||")
	s = s:gsub("\5", "||")
	return s
end

-- Swaps the sentinels for colour codes: \1..\2 inline code, \3..\4 bold. The
-- text must already be pipe-safe. Every colour opened is closed again.
function ns.InlineMarkup(text, codeHex, boldHex)
	local out, open = {}, false
	for i = 1, #text do
		local ch = text:sub(i, i)
		if ch == "\1" or ch == "\3" then
			if open then
				out[#out + 1] = "|r"
			end
			out[#out + 1] = "|cff" .. (ch == "\1" and codeHex or boldHex)
			open = true
		elseif ch == "\2" or ch == "\4" then
			if open then
				out[#out + 1] = "|r"
				open = false
			end
		else
			out[#out + 1] = ch
		end
	end
	if open then
		out[#out + 1] = "|r"
	end
	return table.concat(out)
end

-- Truncates to at most `max` display characters, ending in "...". A "||" pair
-- counts as one character and is never split; multi-byte UTF-8 sequences are
-- never split.
function ns.Truncate(s, max)
	local len = #s
	local ends, n, i = {}, 0, 1
	while i <= len do
		local b = s:byte(i)
		local step = 1
		if b == 124 and s:byte(i + 1) == 124 then
			step = 2
		elseif b >= 240 then
			step = 4
		elseif b >= 224 then
			step = 3
		elseif b >= 192 then
			step = 2
		end
		i = i + step
		n = n + 1
		ends[n] = min(i - 1, len)
		if n > max then
			break
		end
	end
	if n <= max then
		return s
	end
	local keep = max - 3
	if keep < 1 then
		return "..."
	end
	return s:sub(1, ends[keep]) .. "..."
end

-- Calls `fn(...)` and returns its first result only when it is a plain number. Region
-- geometry is `SecretWhenAnchoringSecret`; a secret (or an error) gives nil, never a value
-- addon code could trip over by comparing or formatting it.
function ns.PlainNumber(fn, ...)
	local ok, v = pcall(fn, ...)
	if not ok or v == nil then
		return nil
	end
	local isSecret = rawget(_G, "issecretvalue")
	if type(isSecret) == "function" then
		local okSecret, secret = pcall(isSecret, v)
		if not okSecret or secret then
			return nil
		end
	end
	if type(v) ~= "number" then
		return nil
	end
	return v
end

-- Local-only chat line. `msg` must already be markup-safe.
function ns.Print(msg)
	local chat = DEFAULT_CHAT_FRAME
	if chat then
		chat:AddMessage(ns.PREFIX .. " " .. msg)
	end
end
