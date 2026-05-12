package queue

import (
	"github.com/redis/go-redis/v9"
)

var pushScript = redis.NewScript(`
local waiting = KEYS[1]
local delayed = KEYS[2]
local msgKey = KEYS[3]

local id = ARGV[1]
local msgRaw = ARGV[2]
local mode = ARGV[3]
local score = tonumber(ARGV[4])
local ttlSeconds = tonumber(ARGV[5]) or 0

redis.call('SET', msgKey, msgRaw)
if ttlSeconds > 0 then
    redis.call('EXPIRE', msgKey, ttlSeconds)
end
if mode == 'waiting' then
    redis.call('LPUSH', waiting, id)
else
    redis.call('ZADD', delayed, score, id)
end
return 1
`)

// moveScript moves due members from a zset source to the waiting list.
// It is used for:
// 1) delayed -> waiting
// 2) reserved (expired) -> timeout
// For each moved id, message status is updated to ARGV[2].
var moveScript = redis.NewScript(`
local from = KEYS[1]
local to = KEYS[2]
local msgPrefix = KEYS[3]

local now = tonumber(ARGV[1])
local status = ARGV[2]

local ids = redis.call('ZRANGEBYSCORE', from, '-inf', now, 'LIMIT', 0, 100)
if #ids == 0 then
    return 0
end

for _, id in ipairs(ids) do
    redis.call('ZREM', from, id)
    local msgKey = msgPrefix .. id
    local raw = redis.call('GET', msgKey)
    if raw then
        local ok, msg = pcall(cjson.decode, raw)
        if ok and type(msg) == 'table' then
            msg["status"] = status
            msg["updated_at"] = now
            local ttl = redis.call('TTL', msgKey)
            redis.call('SET', msgKey, cjson.encode(msg))
            if ttl > 0 then
                redis.call('EXPIRE', msgKey, ttl)
            end
            redis.call('LPUSH', to, id)
        else
            redis.call('DEL', msgKey)
        end
    else
        redis.call('LREM', to, 0, id)
    end
end
return #ids
`)

// moveTimeoutScript moves expired reserved messages to the timeout queue without
// updating message status. This avoids a race where a consumer finishes after
// the forwarder has already moved the message, leaving a stale timeout entry
// with a terminal status. The reloadScript checks status before re-queuing.
var moveTimeoutScript = redis.NewScript(`
local from = KEYS[1]
local to = KEYS[2]
local msgPrefix = KEYS[3]

local now = tonumber(ARGV[1])

local ids = redis.call('ZRANGEBYSCORE', from, '-inf', now, 'LIMIT', 0, 100)
if #ids == 0 then
    return 0
end

local moved = 0
for _, id in ipairs(ids) do
    redis.call('ZREM', from, id)
    local msgKey = msgPrefix .. id
    if redis.call('EXISTS', msgKey) == 1 then
        redis.call('LPUSH', to, id)
        moved = moved + 1
    end
end
return moved
`)

// popScript atomically claims one message from waiting:
// waiting (RPOP) -> reserved (ZADD with deadline), and status -> reserved.
// Returns claimed message id, or nil when waiting is empty.
var popScript = redis.NewScript(`
local waiting = KEYS[1]
local reserved = KEYS[2]
local msgPrefix = KEYS[3]

local now = tonumber(ARGV[1])
local deadline = tonumber(ARGV[2])

while true do
    local id = redis.call('RPOP', waiting)
    if not id then
        return nil
    end
    local msgKey = msgPrefix .. id
    local raw = redis.call('GET', msgKey)
    if raw then
        local ok, msg = pcall(cjson.decode, raw)
        if ok and type(msg) == 'table' then
            msg["status"] = "reserved"
            msg["updated_at"] = now
            local ttl = redis.call('TTL', msgKey)
            redis.call('SET', msgKey, cjson.encode(msg))
            if ttl > 0 then
                redis.call('EXPIRE', msgKey, ttl)
            end
            redis.call('ZADD', reserved, deadline, id)
            return id
        else
            redis.call('DEL', msgKey)
        end
    end
end
`)

// ackScript commits success:
// remove from reserved and mark message status as done.
var ackScript = redis.NewScript(`
local reserved = KEYS[1]
local msgKey = KEYS[2]

local id = ARGV[1]
local now = tonumber(ARGV[2])

redis.call('ZREM', reserved, id)
local raw = redis.call('GET', msgKey)
if not raw then
    return 1
end
local ok, msg = pcall(cjson.decode, raw)
if not ok or type(msg) ~= 'table' then
    return 1
end
msg["status"] = "done"
msg["updated_at"] = now
local ttl = redis.call('TTL', msgKey)
redis.call('SET', msgKey, cjson.encode(msg))
if ttl > 0 then
    redis.call('EXPIRE', msgKey, ttl)
end
return 1
`)

// failScript commits terminal failure:
// remove from reserved, push to failed, and mark status as failed.
var failScript = redis.NewScript(`
local reserved = KEYS[1]
local failed = KEYS[2]
local msgKey = KEYS[3]

local id = ARGV[1]
local now = tonumber(ARGV[2])

redis.call('ZREM', reserved, id)
local raw = redis.call('GET', msgKey)
if not raw then
    return 1
end
local ok, msg = pcall(cjson.decode, raw)
if not ok or type(msg) ~= 'table' then
    return 1
end
msg["status"] = "failed"
msg["updated_at"] = now
local ttl = redis.call('TTL', msgKey)
redis.call('SET', msgKey, cjson.encode(msg))
if ttl > 0 then
    redis.call('EXPIRE', msgKey, ttl)
end
redis.call('LPUSH', failed, id)
return 1
`)

// dropScript commits business discard:
// remove from reserved and mark status as dropped.
var dropScript = redis.NewScript(`
local reserved = KEYS[1]
local msgKey = KEYS[2]

local id = ARGV[1]
local now = tonumber(ARGV[2])

redis.call('ZREM', reserved, id)
local raw = redis.call('GET', msgKey)
if not raw then
    return 1
end
local ok, msg = pcall(cjson.decode, raw)
if not ok or type(msg) ~= 'table' then
    return 1
end
msg["status"] = "dropped"
msg["updated_at"] = now
local ttl = redis.call('TTL', msgKey)
redis.call('SET', msgKey, cjson.encode(msg))
if ttl > 0 then
    redis.call('EXPIRE', msgKey, ttl)
end
return 1
`)

// requeueScript commits immediate re-dispatch:
// remove from reserved, push to waiting, and mark status as waiting.
var requeueScript = redis.NewScript(`
local reserved = KEYS[1]
local waiting = KEYS[2]
local msgKey = KEYS[3]

local id = ARGV[1]
local now = tonumber(ARGV[2])

redis.call('ZREM', reserved, id)
local raw = redis.call('GET', msgKey)
if not raw then
    return 1
end
local ok, msg = pcall(cjson.decode, raw)
if not ok or type(msg) ~= 'table' then
    return 1
end
msg["status"] = "waiting"
msg["updated_at"] = now
local ttl = redis.call('TTL', msgKey)
redis.call('SET', msgKey, cjson.encode(msg))
if ttl > 0 then
    redis.call('EXPIRE', msgKey, ttl)
end
redis.call('LPUSH', waiting, id)
return 1
`)

// retryByIDScript re-schedules a message for another attempt.
// Inputs:
// - KEYS: waiting, reserved, delayed, timeout, failed, msgKey
// - ARGV: id, score, msgRaw, mode(waiting|delayed)
//
// Behavior:
// 1) Fast path for consumer retry: remove from reserved first.
// 2) Compatibility path for management retry: remove from active/dead-letter queues.
// 3) Rewrite message payload/status (msgRaw), preserve TTL, enqueue by mode.
//
// Returns:
// - 1: retried successfully
// - 0: target id not found in expected queues
var retryByIDScript = redis.NewScript(`
local waiting = KEYS[1]
local reserved = KEYS[2]
local delayed = KEYS[3]
local timeout = KEYS[4]
local failed = KEYS[5]
local msgKey = KEYS[6]

local id = ARGV[1]
local score = tonumber(ARGV[2])
local msgRaw = ARGV[3]
local mode = ARGV[4]

-- Fast path for consumer retry: message is expected in reserved.
local removedReserved = redis.call('ZREM', reserved, id)
if removedReserved > 0 then
    local ttl = redis.call('TTL', msgKey)
    redis.call('SET', msgKey, msgRaw)
    if ttl > 0 then
        redis.call('EXPIRE', msgKey, ttl)
    end
    if mode == 'waiting' then
        redis.call('LPUSH', waiting, id)
    else
        redis.call('ZADD', delayed, score, id)
    end
    return 1
end

-- Compatibility path for management retry by id.
local removed = 0
removed = removed + redis.call('LREM', waiting, 0, id)
removed = removed + redis.call('ZREM', delayed, id)
removed = removed + redis.call('LREM', timeout, 0, id)
removed = removed + redis.call('LREM', failed, 0, id)
if removed == 0 then
    return 0
end

local ttl = redis.call('TTL', msgKey)
redis.call('SET', msgKey, msgRaw)
if ttl > 0 then
    redis.call('EXPIRE', msgKey, ttl)
end
if mode == 'waiting' then
    redis.call('LPUSH', waiting, id)
else
    redis.call('ZADD', delayed, score, id)
end
return 1
`)

// cancelByIDScript cancels a message only when it is still delayed.
// Inputs:
// - KEYS: waiting, reserved, delayed, msgKey
// - ARGV: id, now
//
// Decision priority is queue position (source of truth):
// 1) delayed: cancel success, mark status canceled
// 2) reserved: reject (already in execution)
// 3) waiting: reject (already ready for dispatch)
//
// Returns:
// - 1: canceled
// - 0: not found or terminal state
// - -1: already ready for dispatch (waiting)
// - -2: already in execution (reserved)
var cancelByIDScript = redis.NewScript(`
local waiting = KEYS[1]
local reserved = KEYS[2]
local delayed = KEYS[3]
local msgKey = KEYS[4]

local id = ARGV[1]
local now = tonumber(ARGV[2])

-- 队列位置是唯一判定依据：先尝试从 delayed 移除
local removed = redis.call('ZREM', delayed, id)
if removed > 0 then
    local raw = redis.call('GET', msgKey)
    if raw then
        local ok, msg = pcall(cjson.decode, raw)
        if ok and type(msg) == 'table' then
            msg["status"] = "canceled"
            msg["updated_at"] = now
            local pttl = redis.call('PTTL', msgKey)
            redis.call('SET', msgKey, cjson.encode(msg))
            if pttl > 0 then
                redis.call('PEXPIRE', msgKey, pttl)
            end
        else
            redis.call('DEL', msgKey)
        end
    end
    return 1
end

-- 已在执行中
if redis.call('ZSCORE', reserved, id) then
    return -2
end

-- 已待调度中
if redis.call('LPOS', waiting, id) ~= nil then
    return -1
end

-- 不存在或已终态
return 0
`)

// reloadScript manually moves messages from source list (timeout/failed) back to waiting.
// It updates status to waiting and preserves message TTL.
var reloadScript = redis.NewScript(`
local source = KEYS[1]
local waiting = KEYS[2]
local msgPrefix = KEYS[3]
local now = tonumber(ARGV[1])
local skipStale = ARGV[2] == "1"

local count = 0
for i=1,100 do
    local id = redis.call('RPOPLPUSH', source, waiting)
    if not id then
        break
    end
    local msgKey = msgPrefix .. id
    local raw = redis.call('GET', msgKey)
    if not raw then
        redis.call('LREM', waiting, 0, id)
    else
        local ok, msg = pcall(cjson.decode, raw)
        if ok and type(msg) == 'table' then
            local s = msg["status"]
            if skipStale and (s == "done" or s == "dropped" or s == "canceled") then
                -- already processed or intentionally dropped, no need to retry
                redis.call('LREM', waiting, 0, id)
            else
                msg["status"] = "waiting"
                msg["updated_at"] = now
                local ttl = redis.call('TTL', msgKey)
                redis.call('SET', msgKey, cjson.encode(msg))
                if ttl > 0 then
                    redis.call('EXPIRE', msgKey, ttl)
                end
                count = count + 1
            end
        else
            redis.call('LREM', waiting, 0, id)
        end
    end
end
return count
`)

// nextSeqScript atomically increments sequence with rollover.
// KEYS[1]: sequence key
// KEYS[2]: sequence epoch key
// ARGV[1]: max sequence value
// Returns: {seq, epoch}
var nextSeqScript = redis.NewScript(`
local seqKey = KEYS[1]
local epochKey = KEYS[2]
local max = tonumber(ARGV[1])

local seq = redis.call('INCR', seqKey)
local epoch = redis.call('GET', epochKey)
if not epoch then
    epoch = '0'
    redis.call('SET', epochKey, epoch)
end

if seq > max then
    seq = 0
    redis.call('SET', seqKey, seq)
    epoch = redis.call('INCR', epochKey)
end

return {tostring(seq), tostring(epoch)}
`)
