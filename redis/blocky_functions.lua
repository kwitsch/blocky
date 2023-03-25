#!lua name=blocky

local function blocky_cache_members(keys, args)
    local searchValue = args[1]
    local cachKey = keys[1]
    local hitKey = args[2]
    local force = args[4]
    local ttl = args[5]

    local hit = redis.call("HGET", cachKey, hitKey)
    redis.log(redis.LOG_WARNING,"HGET", cachKey, hitKey, hit)
    if not hit or force == '1' then
        local grpRes = {}
        for i, v in ipairs(keys) do
            redis.log(redis.LOG_WARNING,v)
            if i > 1 then
                local im = redis.call('SISMEMBER', v, searchValue)
                if im == 1 then
                    local ir = string.match(v, '[^:]+$')
                    if ir ~= nil then
                        table.insert(grpRes, ir)
                    end
                end
            end
        end
        if next(grpRes) == nil then
            redis.call('HSET', cachKey, hitKey, 0)
            redis.call('EXPIRE', cachKey, ttl, 'GT')
            return '0'
        else
            redis.call('HSET', cachKey, hitKey, 1)
            local groupKey = args[3]
            local groups =''
            local first=1
            for i, v in ipairs(grpRes) do
                if first ~= 1 then
                    groups = groups .. ','
                else
                    first = 0
                end
                groups = groups .. v
            end
            redis.call('HSET', cachKey, groupKey, groups)
            redis.call('EXPIRE', cachKey, ttl, 'GT')
            return '1'
        end
    end

    return hit
end

redis.register_function('blocky_cache_members', blocky_cache_members)