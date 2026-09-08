package repository

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const contentModerationRedisConfigKey = "sub2api:content_moderation:config"

type contentModerationConfigStore struct {
	rdb *redis.Client
}

func NewContentModerationConfigStore(rdb *redis.Client) service.ContentModerationConfigStore {
	return &contentModerationConfigStore{rdb: rdb}
}

func (s *contentModerationConfigStore) Get(ctx context.Context) (string, bool, error) {
	if s == nil || s.rdb == nil {
		return "", false, nil
	}
	value, err := s.rdb.Get(ctx, contentModerationRedisConfigKey).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (s *contentModerationConfigStore) Set(ctx context.Context, value string) error {
	if s == nil || s.rdb == nil {
		return nil
	}
	return s.rdb.Set(ctx, contentModerationRedisConfigKey, value, 0).Err()
}
