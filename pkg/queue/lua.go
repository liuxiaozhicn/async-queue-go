package queue

import "github.com/redis/go-redis/v9"

// moveScript atomically moves expired members from a sorted set to a list.
// KEYS[1]: source sorted set (e.g. delayed or reserved)
// KEYS[2]: destination list (e.g. waiting or timeout)
// ARGV[1]: current unix timestamp
// Returns: number of moved members
var moveScript = redis.NewScript(`
local from = KEYS[1]
local to   = KEYS[2]
local now  = ARGV[1]

local expired = redis.call('ZREVRANGEBYSCORE', from, now, '-inf', 'LIMIT', 0, 100)

if #expired == 0 then
    return 0
end

for i, job in ipairs(expired) do
    redis.call('ZREM', from, job)
    redis.call('LPUSH', to, job)
end

return #expired
`)

// popScript atomically pops from a list and adds to a sorted set (reserved queue).
// KEYS[1]: source list (waiting)
// KEYS[2]: destination sorted set (reserved)
// ARGV[1]: reserved score (deadline unix timestamp)
// Returns: the job data, or nil if the list is empty
var popScript = redis.NewScript(`
local data = redis.call('RPOP', KEYS[1])

if not data then
    return nil
end

redis.call('ZADD', KEYS[2], ARGV[1], data)

return data
`)
