package redislock

import "github.com/redis/go-redis/v9"

var (
	luaReleaseMultiple = redis.NewScript(`
	local num_key = table.getn(KEYS)
	local res
	for i = 1, num_key do
		if redis.call("get", KEYS[i]) == ARGV[1] then
			res = redis.call("del", KEYS[i]) 
		else 
			return 0 
		end
	end

	return res
	`)

	luaSetMultiple = redis.NewScript(`
	local num_key = table.getn(KEYS)
	local res
	local failed
	for i = 1, num_key do
		if not redis.call('set', KEYS[i], ARGV[1], 'px', ARGV[2], 'nx') then 
            res = i-1
            failed = 1
            break
        end
	end


	if failed == 1 then
		for i = 1, res do
			redis.call('del',KEYS[i])
		end
		return 0
	end

	return 1
	`)

	luaRefreshMultiple = redis.NewScript(`
	local num_key = table.getn(KEYS)
	local res
	for i = 1, num_key do
		if redis.call("get", KEYS[i]) == ARGV[1] then
			res = redis.call("pexpire", KEYS[1], ARGV[2]) 
		else 
			return 0 
		end
	end

	return res
	`)

	luaPTTLMultiple = redis.NewScript(`
	local num_key = table.getn(KEYS)

	for i = 1, num_key do
		if redis.call("get", KEYS[i]) == ARGV[1] then
			return redis.call("pttl", KEYS[1]) 
		else 
			return -3
		end
	end
	`)
)
