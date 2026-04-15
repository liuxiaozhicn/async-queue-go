package queue

import "github.com/redis/go-redis/v9"

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

var retryScript = redis.NewScript(`
local reserved = KEYS[1]
local delayed = KEYS[2]
local msgKey = KEYS[3]

local id = ARGV[1]
local score = tonumber(ARGV[2])
local msgRaw = ARGV[3]

redis.call('ZREM', reserved, id)
local ttl = redis.call('TTL', msgKey)
redis.call('SET', msgKey, msgRaw)
if ttl > 0 then
    redis.call('EXPIRE', msgKey, ttl)
end
redis.call('ZADD', delayed, score, id)
return 1
`)

var deleteByIDScript = redis.NewScript(`
local waiting = KEYS[1]
local reserved = KEYS[2]
local delayed = KEYS[3]
local timeout = KEYS[4]
local failed = KEYS[5]
local msgKey = KEYS[6]

local id = ARGV[1]

local removed = 0
removed = removed + redis.call('LREM', waiting, 0, id)
removed = removed + redis.call('ZREM', reserved, id)
removed = removed + redis.call('ZREM', delayed, id)
removed = removed + redis.call('LREM', timeout, 0, id)
removed = removed + redis.call('LREM', failed, 0, id)
redis.call('DEL', msgKey)
if removed > 0 then
    return 1
end
return 0
`)

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

local removed = 0
removed = removed + redis.call('LREM', waiting, 0, id)
removed = removed + redis.call('ZREM', reserved, id)
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
redis.call('ZADD', delayed, score, id)
return 1
`)

var reloadScript = redis.NewScript(`
local source = KEYS[1]
local waiting = KEYS[2]
local msgPrefix = KEYS[3]
local now = tonumber(ARGV[1])

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
            msg["status"] = "waiting"
            msg["updated_at"] = now
            local ttl = redis.call('TTL', msgKey)
            redis.call('SET', msgKey, cjson.encode(msg))
            if ttl > 0 then
                redis.call('EXPIRE', msgKey, ttl)
            end
        end
        count = count + 1
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
