#!/bin/sh
set -eu

mkdir -p /data

COMMON_ARGS="--bind 0.0.0.0 --port 6379 --appendonly yes --appendfsync everysec --save 60 1 --requirepass ${REDIS_PASSWORD} --masterauth ${REDIS_PASSWORD} --maxclients ${REDIS_MAXCLIENTS} --replica-announce-ip ${NODE_IP} --replica-announce-port 6379"

get_master_from_sentinel() {
  if [ -z "${REDIS_SENTINEL_ADDRS:-}" ] || [ -z "${REDIS_SENTINEL_MASTER_NAME:-}" ]; then
    return 1
  fi

  OLD_IFS=$IFS
  IFS=','
  for sentinel in ${REDIS_SENTINEL_ADDRS}; do
    host=${sentinel%:*}
    port=${sentinel#*:}
    if [ "$host" = "$sentinel" ]; then
      port=26379
    fi
    output=$(redis-cli -h "$host" -p "$port" SENTINEL get-master-addr-by-name "${REDIS_SENTINEL_MASTER_NAME}" 2>/dev/null | tr -d '\r' || true)
    set -- $(printf '%s\n' "$output" | sed '/^$/d')
    if [ "$#" -ge 2 ]; then
      printf '%s:%s\n' "$1" "$2"
      IFS=$OLD_IFS
      return 0
    fi
  done
  IFS=$OLD_IFS
  return 1
}

current_master=""
attempt=0
while [ "$attempt" -lt 15 ]; do
  if current_master=$(get_master_from_sentinel); then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done

if [ -n "$current_master" ]; then
  master_host=${current_master%:*}
  master_port=${current_master#*:}
  if [ "$master_host" = "${NODE_IP}" ]; then
    exec redis-server \
      ${COMMON_ARGS} \
      --min-replicas-to-write 1 \
      --min-replicas-max-lag 10
  fi
  exec redis-server \
    ${COMMON_ARGS} \
    --replicaof "$master_host" "$master_port"
fi

if [ "${REDIS_ROLE}" = "master" ]; then
  exec redis-server \
    ${COMMON_ARGS} \
    --min-replicas-to-write 1 \
    --min-replicas-max-lag 10
fi

exec redis-server \
  --replicaof "${REDIS_MASTER_HOST}" 6379 \
  ${COMMON_ARGS}
