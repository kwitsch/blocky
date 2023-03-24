#!lua name=blocky

local function blocky_ismember(keys, args)
    res = {}
    for i=1,table.getN(keys) do
        im=redis.call('sismember',keys[i],args[1])
        if im == true then
            ir = string.match(keys[i], '[^:]+$')
            if ir ~= nil then
                table.insert(res, ir)
            end
        end
    end

    return res
  end
  
  redis.register_function('blocky_ismember', blocky_ismember)