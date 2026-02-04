package rate

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultRedisPrefix = "cex:auth:rl:"

var rateLimitScript = redis.NewScript(`
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])

local current = redis.call("INCR", key)
if current == 1 then
  redis.call("PEXPIRE", key, window_ms)
end

local ttl = redis.call("PTTL", key)
if ttl < 0 then
  ttl = window_ms
end

if current > limit then
  return {0, ttl}
end
return {1, ttl}
`)

type RedisLimiter struct {
	client *redis.Client
	limit  int
	window time.Duration
	prefix string
}

func NewRedisLimiter(client *redis.Client, limit int, window time.Duration, prefix string) *RedisLimiter {
	if prefix == "" {
		prefix = defaultRedisPrefix
	}
	return &RedisLimiter{
		client: client,
		limit:  limit,
		window: window,
		prefix: prefix,
	}
}

func (l *RedisLimiter) Allow(ctx context.Context, key string, _ time.Time) (bool, time.Duration, error) {
	redisKey := l.prefix + key
	windowMS := int64(l.window / time.Millisecond)
	if windowMS <= 0 {
		return false, 0, fmt.Errorf("invalid rate limit window")
	}

	res, err := rateLimitScript.Run(ctx, l.client, []string{redisKey}, l.limit, windowMS).Result()
	if err != nil {
		return false, 0, err
	}

	vals, ok := res.([]interface{})
	if !ok || len(vals) != 2 {
		return false, 0, fmt.Errorf("unexpected redis response")
	}

	allowedInt, ok := vals[0].(int64)
	if !ok {
		return false, 0, fmt.Errorf("unexpected redis response")
	}

	ttlMS, ok := vals[1].(int64)
	if !ok {
		return false, 0, fmt.Errorf("unexpected redis response")
	}

	retryAfter := time.Duration(ttlMS) * time.Millisecond
	if retryAfter < 0 {
		retryAfter = 0
	}

	return allowedInt == 1, retryAfter, nil
}
