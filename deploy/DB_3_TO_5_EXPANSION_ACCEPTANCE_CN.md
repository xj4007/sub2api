# PostgreSQL / Redis 数据层 3→5 扩容最终验收清单

> 适用范围：本次把 `sub2api` 数据层从 **3 台扩到 5 台** 的验收收尾。
>
> 目标：确认 **WireGuard / etcd / Patroni / PostgreSQL / Redis / Sentinel / app proxy** 已全部切到新拓扑，并满足可继续运行的生产口径。

---

## 1. 目标拓扑

### 1.1 数据层节点

| 节点 | 公网 IP | WG IP | 目标角色 |
|---|---|---|---|
| `db1` | `154.201.64.184` | `10.77.0.1` | PostgreSQL replica / Redis replica / Sentinel / etcd |
| `db2` | `154.12.80.55` | `10.77.0.2` | PostgreSQL leader / Redis replica / Sentinel / etcd |
| `db3` | `156.225.18.73` | `10.77.0.3` | PostgreSQL replica / Redis master / Sentinel / etcd |
| `db4` | `211.101.237.129` | `10.77.0.7` | PostgreSQL replica / Redis replica / Sentinel / etcd |
| `db5` | `101.237.129.94` | `10.77.0.8` | PostgreSQL replica / Redis replica / Sentinel / etcd |

### 1.2 应用层节点

| 节点 | 公网 IP | WG IP |
|---|---|---|
| `app4` | `154.12.21.52` | `10.77.0.4` |
| `app5` | `45.192.105.162` | `10.77.0.5` |
| `app6` | `156.225.20.29` | `10.77.0.6` |

---

## 2. 最终验收标准

只有下面全部成立，才算本次 3→5 扩容真正完成：

1. **WireGuard**：8 节点互通正常
2. **etcd**：5 成员全部 `started`
3. **PostgreSQL**：`db2` 为 leader，`db1/db3/db4/db5` 全部 `streaming`
4. **Redis**：`db3` 为 master，其余 4 台为在线 replica
5. **Sentinel**：5 个 Sentinel 能一致识别 `10.77.0.3:6379` 为 master
6. **应用层**：3 台 `sub2api` `/health` 全部为 `ok`
7. **应用代理配置**：3 台 `haproxy.cfg` 都包含 `db1~db5`
8. **应用 Redis Sentinel 地址**：3 台应用 `.env` 都包含 5 个 Sentinel 地址
9. **文档**：`deploy/HA_DEPLOYMENT_CN.md` 已同步为 5 台数据层版本

---

## 3. WireGuard 验收

### 3.1 新节点本机检查

在 `db4` / `db5` 上执行：

```bash
ip -brief addr show wg0
wg show
```

通过标准：

- 存在 `wg0`
- `db4` 地址为 `10.77.0.7/24`
- `db5` 地址为 `10.77.0.8/24`
- 能看到其他节点 peer

### 3.2 互通检查

在 `db4` 上执行：

```bash
for ip in 10.77.0.1 10.77.0.2 10.77.0.3 10.77.0.4 10.77.0.5 10.77.0.6 10.77.0.8; do
  ping -c 1 -W 2 $ip && echo ok:$ip || echo fail:$ip
done
```

在 `db5` 上执行：

```bash
for ip in 10.77.0.1 10.77.0.2 10.77.0.3 10.77.0.4 10.77.0.5 10.77.0.6 10.77.0.7; do
  ping -c 1 -W 2 $ip && echo ok:$ip || echo fail:$ip
done
```

通过标准：全部 `ok`。

---

## 4. etcd 验收

在当前 leader 所在数据节点（当前可在 `db2` 执行）执行：

```bash
docker exec sub2api-etcd etcdctl member list -w table
docker exec sub2api-etcd etcdctl endpoint health --cluster
```

通过标准：

- 成员数为 **5**
- `db1/db2/db3/db4/db5` 全部 `started`
- cluster health 全部 healthy

---

## 5. PostgreSQL / Patroni 验收

### 5.1 集群角色检查

在 `db2` 执行：

```bash
docker exec sub2api-patroni patronictl -c /run/patroni/patroni.yml list
```

通过标准：

- `db2` 为 `Leader`
- `db1/db3/db4/db5` 为 `Replica`
- 4 个副本全部为 `streaming`

### 5.2 主库复制视图检查

在 `db2` 执行：

```bash
docker exec sub2api-patroni psql -U sub2api -d postgres -c "select client_addr, state, sync_state, application_name from pg_stat_replication order by client_addr;"
```

通过标准：至少包含：

- `10.77.0.1 | streaming | async | db1`
- `10.77.0.3 | streaming | async | db3`
- `10.77.0.7 | streaming | async | db4`
- `10.77.0.8 | streaming | async | db5`

### 5.3 新副本本机检查

在 `db4` / `db5` 执行：

```bash
curl -fsS http://127.0.0.1:8008/replica
docker exec sub2api-patroni psql -U sub2api -d postgres -c "select pg_is_in_recovery();"
```

通过标准：

- `/replica` 返回 200
- `pg_is_in_recovery = true`

---

## 6. Redis / Sentinel 验收

### 6.1 Redis 主从检查

在 `db3` 执行：

```bash
docker exec sub2api-redis redis-cli -a XIANjian4SANyun info replication
```

通过标准：

- `role:master`
- `connected_slaves:4`
- 能看到 `10.77.0.1 / 10.77.0.2 / 10.77.0.7 / 10.77.0.8`

### 6.2 Sentinel 检查

在任一数据节点执行：

```bash
docker exec sub2api-redis-sentinel redis-cli -p 26379 SENTINEL masters
```

通过标准：

- master 为 `10.77.0.3:6379`
- `num-slaves=4`
- `num-other-sentinels=4`

---

## 7. 应用层验收

### 7.1 健康检查

在 3 台应用机执行：

```bash
curl -fsS http://127.0.0.1:8080/health
```

通过标准：全部返回：

```json
{"status":"ok"}
```

### 7.2 HAProxy 后端检查

在任一应用机执行：

```bash
cat /opt/sub2api-ha/app/haproxy.cfg
```

通过标准：

- writer backend 包含 `db1~db5`
- reader backend 包含 `db1~db5`

### 7.3 应用 Redis Sentinel 地址检查

```bash
grep REDIS_SENTINEL_ADDRS /opt/sub2api-ha/app/.env
```

通过标准：包含：

```text
10.77.0.1:26379,10.77.0.2:26379,10.77.0.3:26379,10.77.0.7:26379,10.77.0.8:26379
```

---

## 8. 建议保留的最终证据

建议把以下输出另存一份，作为本次扩容完成证据：

1. `etcdctl member list -w table`
2. `patronictl list`
3. 主库 `pg_stat_replication`
4. Redis master `info replication`
5. Sentinel `SENTINEL masters`
6. 三台应用 `/health`
7. 三台应用 `haproxy.cfg`

---

## 9. 最终结论模板

如果以上检查全部通过，可以记录为：

> 本次 `sub2api` 数据层已从 **3 节点成功扩展到 5 节点**。WireGuard 已扩到 8 节点互联；etcd 已扩到 5 成员；PostgreSQL 已形成 `1 leader + 4 streaming replica`；Redis 已形成 `1 master + 4 replica + 5 Sentinel`；3 台应用节点健康检查全部通过，应用侧代理与 Sentinel 地址已完成 5 节点切换。
