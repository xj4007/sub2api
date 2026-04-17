#!/usr/bin/env python3
from __future__ import annotations

import sys


WG_IP_BY_NODE = {
    "db1": "10.77.0.1",
    "db2": "10.77.0.2",
    "db3": "10.77.0.3",
}

PUBLIC_IP_TO_NODE = {
    "154.201.64.184": "db1",
    "154.12.80.55": "db2",
    "156.225.18.73": "db3",
}

REDIS_SENTINEL_ADDRS = ",".join(f"{ip}:26379" for ip in WG_IP_BY_NODE.values())


def resolve_node(value: str) -> str:
    node = PUBLIC_IP_TO_NODE.get(value, value)
    if node not in WG_IP_BY_NODE:
        valid = ", ".join(sorted(WG_IP_BY_NODE))
        raise SystemExit(f"unknown node '{value}', expected one of: {valid} or known public IP")
    return node


def main() -> None:
    if len(sys.argv) != 4:
        raise SystemExit("usage: render-db-env.py <db-node|public-ip> <public-ip|ignored> <master|replica>")

    node_arg, fallback_ip_arg, redis_role = sys.argv[1:4]
    node = resolve_node(node_arg)

    if node_arg not in WG_IP_BY_NODE:
        node = resolve_node(fallback_ip_arg)

    if redis_role not in {"master", "replica"}:
        raise SystemExit("redis role must be 'master' or 'replica'")

    node_ip = WG_IP_BY_NODE[node]
    etcd_hosts = ",".join(f"{WG_IP_BY_NODE[name]}:2379" for name in ("db1", "db2", "db3"))
    etcd_cluster = ",".join(
        f"{name}=http://{WG_IP_BY_NODE[name]}:2380" for name in ("db1", "db2", "db3")
    )

    lines = [
        f"NODE_IP={node_ip}",
        f"ETCD_NAME={node}",
        f"ETCD_INITIAL_CLUSTER={etcd_cluster}",
        f"ETCD_HOSTS={etcd_hosts}",
        "PATRONI_SCOPE=sub2api-pg",
        f"PATRONI_NAME={node}",
        "POSTGRES_USER=sub2api",
        "POSTGRES_PASSWORD=XIANjian4SANyun",
        "POSTGRES_REPLICATION_USER=replicator",
        "POSTGRES_REPLICATION_PASSWORD=XIANjian4SANyun",
        "POSTGRES_MAX_CONNECTIONS=1024",
        "POSTGRES_SHARED_BUFFERS=1GB",
        "POSTGRES_EFFECTIVE_CACHE_SIZE=4GB",
        "POSTGRES_MAINTENANCE_WORK_MEM=128MB",
        f"REDIS_ROLE={redis_role}",
        "REDIS_MASTER_HOST=10.77.0.1",
        "REDIS_PASSWORD=XIANjian4SANyun",
        "REDIS_MAXCLIENTS=50000",
        "REDIS_SENTINEL_MASTER_NAME=sub2api-redis",
        f"REDIS_SENTINEL_ADDRS={REDIS_SENTINEL_ADDRS}",
    ]
    print("\n".join(lines))


if __name__ == "__main__":
    main()
