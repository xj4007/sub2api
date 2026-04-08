#!/bin/sh
set -eu

mkdir -p /data

cat > /data/sentinel.conf <<EOF
bind 0.0.0.0
port 26379
dir /data
sentinel resolve-hostnames yes
sentinel announce-hostnames yes
sentinel announce-ip ${NODE_IP}
sentinel announce-port 26379
sentinel monitor ${REDIS_SENTINEL_MASTER_NAME} ${REDIS_MASTER_HOST} 6379 2
sentinel auth-pass ${REDIS_SENTINEL_MASTER_NAME} ${REDIS_PASSWORD}
sentinel down-after-milliseconds ${REDIS_SENTINEL_MASTER_NAME} 5000
sentinel failover-timeout ${REDIS_SENTINEL_MASTER_NAME} 60000
sentinel parallel-syncs ${REDIS_SENTINEL_MASTER_NAME} 1
EOF

exec redis-sentinel /data/sentinel.conf
