#!/bin/sh
set -eu

mkdir -p /var/lib/postgresql/data /run/patroni
chown -R postgres:postgres /var/lib/postgresql /run/patroni

cat > /run/patroni/patroni.yml <<EOF
scope: ${PATRONI_SCOPE}
namespace: /service/
name: ${PATRONI_NAME}

restapi:
  listen: 0.0.0.0:8008
  connect_address: ${NODE_IP}:8008

etcd3:
  hosts: ${ETCD_HOSTS}

bootstrap:
  dcs:
    ttl: 30
    loop_wait: 10
    retry_timeout: 10
    maximum_lag_on_failover: 1048576
    postgresql:
      use_pg_rewind: true
      use_slots: true
      parameters:
        wal_level: replica
        hot_standby: "on"
        wal_keep_size: 256MB
        max_wal_senders: 10
        max_replication_slots: 10
        max_connections: ${POSTGRES_MAX_CONNECTIONS}
        shared_buffers: ${POSTGRES_SHARED_BUFFERS}
        effective_cache_size: ${POSTGRES_EFFECTIVE_CACHE_SIZE}
        maintenance_work_mem: ${POSTGRES_MAINTENANCE_WORK_MEM}
  initdb:
    - encoding: UTF8
    - data-checksums
  pg_hba:
    - host replication ${POSTGRES_REPLICATION_USER} 0.0.0.0/0 scram-sha-256
    - host all all 0.0.0.0/0 scram-sha-256
  users:
    ${POSTGRES_USER}:
      password: ${POSTGRES_PASSWORD}
      options:
        - createrole
        - createdb

postgresql:
  listen: 0.0.0.0:5432
  connect_address: ${NODE_IP}:5432
  data_dir: /var/lib/postgresql/data/pgdata
  bin_dir: /usr/local/bin
  pgpass: /tmp/pgpass
  authentication:
    superuser:
      username: ${POSTGRES_USER}
      password: ${POSTGRES_PASSWORD}
    replication:
      username: ${POSTGRES_REPLICATION_USER}
      password: ${POSTGRES_REPLICATION_PASSWORD}
  parameters:
    unix_socket_directories: /var/run/postgresql
    password_encryption: scram-sha-256

tags:
  nofailover: false
  noloadbalance: false
  clonefrom: false
  nosync: false
EOF

exec su-exec postgres patroni /run/patroni/patroni.yml
