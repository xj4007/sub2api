#!/bin/sh
set -eu

mkdir -p /data

if [ -s /data/sentinel.conf ] && grep -q '^sentinel myid ' /data/sentinel.conf; then
  exec redis-sentinel /data/sentinel.conf
fi

discover_master_host() {
  info=$(redis-cli -h 127.0.0.1 -p 6379 -a "${REDIS_PASSWORD}" INFO replication 2>/dev/null | tr -d '\r' || true)
  role=$(printf '%s\n' "$info" | awk -F: '/^role:/{print $2; exit}')
  if [ "$role" = "master" ]; then
    printf '%s\n' "${NODE_IP}"
    return 0
  fi

  master_host=$(printf '%s\n' "$info" | awk -F: '/^master_host:/{print $2; exit}')
  if [ -n "$master_host" ]; then
    printf '%s\n' "$master_host"
    return 0
  fi

  printf '%s\n' "${REDIS_MASTER_HOST}"
}

MASTER_HOST=$(discover_master_host)

cat > /data/sentinel.conf <<EOF
bind 0.0.0.0
port 26379
dir /data
sentinel resolve-hostnames yes
sentinel announce-hostnames yes
sentinel announce-ip ${NODE_IP}
sentinel announce-port 26379
sentinel monitor ${REDIS_SENTINEL_MASTER_NAME} ${MASTER_HOST} 6379 2
sentinel auth-pass ${REDIS_SENTINEL_MASTER_NAME} ${REDIS_PASSWORD}
sentinel down-after-milliseconds ${REDIS_SENTINEL_MASTER_NAME} 5000
sentinel failover-timeout ${REDIS_SENTINEL_MASTER_NAME} 60000
sentinel parallel-syncs ${REDIS_SENTINEL_MASTER_NAME} 1
EOF

exec redis-sentinel /data/sentinel.conf
