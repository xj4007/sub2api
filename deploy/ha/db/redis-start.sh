#!/bin/sh
set -eu

mkdir -p /data

if [ "${REDIS_ROLE}" = "master" ]; then
  exec redis-server \
    --bind 0.0.0.0 \
    --port 6379 \
    --appendonly yes \
    --appendfsync everysec \
    --save 60 1 \
    --requirepass "${REDIS_PASSWORD}" \
    --masterauth "${REDIS_PASSWORD}" \
    --min-replicas-to-write 1 \
    --min-replicas-max-lag 10 \
    --maxclients "${REDIS_MAXCLIENTS}"
fi

exec redis-server \
  --bind 0.0.0.0 \
  --port 6379 \
  --appendonly yes \
  --appendfsync everysec \
  --save 60 1 \
  --replicaof "${REDIS_MASTER_HOST}" 6379 \
  --replica-announce-ip "${NODE_IP}" \
  --replica-announce-port 6379 \
  --requirepass "${REDIS_PASSWORD}" \
  --masterauth "${REDIS_PASSWORD}" \
  --maxclients "${REDIS_MAXCLIENTS}"
