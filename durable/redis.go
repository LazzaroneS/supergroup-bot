package durable

import (
	"context"
	"github.com/MixinNetwork/supergroup/config"
	"github.com/go-redis/redis/v8"
	"log"
	"os"
)

type Redis struct {
	*redis.Client
}

func NewRedis(ctx context.Context) *Redis {
	rdb := redis.NewClient(&redis.Options{
		Addr:     config.RedisAddr,
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	err := rdb.Set(ctx, "test", "ok", 0).Err()
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	val, err := rdb.Get(ctx, "test").Result()
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	if val != "ok" {
		log.Println(err)
		os.Exit(1)
	}
	return &Redis{rdb}
}

func (r *Redis) QGet(ctx context.Context, key string) string {
	val, err := r.Get(ctx, key).Result()
	if err == redis.Nil {
		return ""
	}
	return val
}

func (r *Redis) QSet(ctx context.Context, key, val string) error {
	return r.Set(ctx, key, val, redis.KeepTTL).Err()
}
