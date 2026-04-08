# Sub2API 生产数据库一次性导入记录

> 这份文档记录的是**一次性生产库导入操作**。
>
> 它和日常发布、日常部署不是一回事。正常情况下，这份文档后续不会频繁使用，只在你需要回看“当初生产库是怎么导进来的”时再参考。

---

## 1. 这份文档是做什么的

这份文档专门记录这次：

- 从旧生产 PostgreSQL 导出数据
- 导入到当前 PostgreSQL HA 集群
- 先做临时恢复演练
- 再正式替换在线库
- 最后验证应用与 migration 都正常

一句话理解：

> **这是一次性的生产数据导入手册，不是长期日常运维手册。**

---

## 2. 旧生产数据库信息

生产数据库机器：

- IP：`43.154.19.173`
- SSH 端口：`22`
- 用户：`root`
- 密码：`XIANjian4SANyun`
- 路径：`/www/dk_project/dk_app/sub2api-db`
- PostgreSQL 容器名：`sub2api-postgres`

生产库 `.env` 中当时的关键配置：

- `POSTGRES_USER=sub2api`
- `POSTGRES_PASSWORD=XIANjian4SANyun`
- `POSTGRES_DB=sub2api`

---

## 3. 当时为什么不能直接导入

当时导库前，先检查了 migration 差异：

- 旧生产库：`99` 条 migration
- 当前代码 / 当前集群：`110` 条 migration

所以不能简单理解为：

- dump 导进来就结束

真正安全的流程应该是：

1. 先把生产库恢复到临时库
2. 用当前版本应用验证能不能自动补 migration 到最新
3. 验证通过后，再正式替换在线库

这是因为：

- 旧生产库结构比当前代码落后
- 如果当前应用不能顺利补 migration，直接替换在线库会有风险

---

## 4. 生产库导出

在部署机本地执行：

```bash
ssh root@43.154.19.173 'docker exec sub2api-postgres pg_dump -U sub2api -d sub2api --format=custom --no-owner --no-privileges' > /tmp/sub2api-prod.dump
```

这一步的结果是生成：

- `/tmp/sub2api-prod.dump`

---

## 5. 先备份当前集群数据库

在正式导入前，先把当时 HA 集群里的 `sub2api` 库做一份快照备份。

在部署机本地执行：

```bash
docker run --rm --network host \
  -e PGPASSWORD='XIANjian4SANyun' \
  postgres:18-alpine \
  pg_dump -h 154.12.80.55 -U sub2api -d sub2api --format=custom --no-owner --no-privileges \
  > /tmp/sub2api-cluster-before-prod-import.dump
```

这一步生成：

- `/tmp/sub2api-cluster-before-prod-import.dump`

这份文件的作用是：

- 如果导入后要回滚，可以恢复导入前的测试/旧数据状态

---

## 6. 先做临时恢复演练（强烈建议）

这一步是为了降低正式导入风险。

### 6.1 创建临时库

```bash
docker run --rm --network host -e PGPASSWORD='XIANjian4SANyun' postgres:18-alpine \
  psql -h 154.12.80.55 -U sub2api -d postgres -c "DROP DATABASE IF EXISTS sub2api_import_check;"

docker run --rm --network host -e PGPASSWORD='XIANjian4SANyun' postgres:18-alpine \
  psql -h 154.12.80.55 -U sub2api -d postgres -c "CREATE DATABASE sub2api_import_check;"
```

### 6.2 恢复到临时库

```bash
docker run --rm --network host \
  -e PGPASSWORD='XIANjian4SANyun' \
  -v /tmp/sub2api-prod.dump:/dump/sub2api-prod.dump:ro \
  postgres:18-alpine \
  pg_restore -h 154.12.80.55 -U sub2api -d sub2api_import_check --clean --if-exists --no-owner --no-privileges /dump/sub2api-prod.dump
```

### 6.3 用当前版本应用验证 migration

然后用**当前版本应用镜像**连这个临时库启动一次，验证 migration 是否能从：

- `99` 补到 `110`

当时这一步已经实测通过，所以后面才继续正式导入。

---

## 7. 正式导入在线库

### 7.1 先停应用

在正式替换数据库前，先停掉 3 台应用节点上的 app 容器：

```bash
for host in 154.12.21.52 45.192.105.162 156.225.20.29; do
  ssh root@$host 'docker stop sub2api'
done
```

### 7.2 保留旧库快照并创建新库

在当时主库 `154.12.80.55` 上执行：

```bash
docker exec sub2api-patroni psql -U sub2api -d postgres -c "select pid, usename, application_name, client_addr, state from pg_stat_activity where datname='sub2api';"
docker exec sub2api-patroni psql -U sub2api -d postgres -c "select pg_terminate_backend(pid) from pg_stat_activity where datname='sub2api' and pid <> pg_backend_pid();"
docker exec sub2api-patroni psql -U sub2api -d postgres -c "ALTER DATABASE sub2api RENAME TO sub2api_before_prod_import_20260407;"
docker exec sub2api-patroni psql -U sub2api -d postgres -c "CREATE DATABASE sub2api;"
```

这里做了两件事：

1. 把当时在线使用的旧库改名保留
2. 新建一个空的 `sub2api` 用来接收生产数据

---

### 7.3 把生产 dump 恢复到新的 `sub2api`

```bash
docker run --rm --network host \
  -e PGPASSWORD='XIANjian4SANyun' \
  -v /tmp/sub2api-prod.dump:/dump/sub2api-prod.dump:ro \
  postgres:18-alpine \
  pg_restore -h 154.12.80.55 -U sub2api -d sub2api --no-owner --no-privileges /dump/sub2api-prod.dump
```

---

### 7.4 先启动一台应用补 migration

不要三台一起起，先只启动一台：

```bash
ssh root@154.12.21.52 'docker start sub2api'
```

然后检查 migration：

```bash
ssh root@154.12.80.55 'docker exec sub2api-patroni psql -U sub2api -d sub2api -c "select count(*) from schema_migrations;"'
```

当时实际结果：

- 导入后初始是 `99`
- 启动当前版本 `sub2api` 后，自动补到 `110`

这一步非常关键，因为它证明：

- 新代码能正确接管旧生产数据
- migration 能自动补齐

---

### 7.5 再启动另外两台应用

```bash
ssh root@45.192.105.162 'docker start sub2api'
ssh root@156.225.20.29 'docker start sub2api'
```

---

## 8. 导入后的验证

### 8.1 验证 PostgreSQL 主从复制

```bash
ssh root@154.12.80.55 'docker exec sub2api-patroni psql -U sub2api -d postgres -c "select client_addr, state, sync_state, application_name from pg_stat_replication;"'
```

预期结果：

- `db1` / `db3` 都是 `streaming`

### 8.2 验证应用健康

```bash
for host in 154.12.21.52 45.192.105.162 156.225.20.29; do
  ssh root@$host 'docker ps --format "table {{.Names}}\t{{.Status}}" | grep sub2api && curl -fsS http://127.0.0.1:8080/health'
done
```

### 8.3 验证管理员登录

```bash
python3 - <<'PY'
import json, urllib.request
url='http://154.12.21.52:8080/api/v1/auth/login'
data=json.dumps({'email':'727965481@qq.com','password':'XIANjian4'}).encode()
req=urllib.request.Request(url, data=data, headers={'Content-Type':'application/json'})
with urllib.request.urlopen(req, timeout=15) as resp:
    print(resp.status)
    print(resp.read().decode())
PY
```

当时这一步返回：

- `200`

---

## 9. 这次导入后的最终结果

当时导入后的结果是：

- 生产数据已经成功导入当前 PostgreSQL HA 集群
- `users` 数量从原测试数据 `1` 变成生产数据 `8`
- migration 从 `99` 自动补齐到 `110`
- 三台应用恢复 healthy
- 管理员 `727965481@qq.com / XIANjian4` 登录成功

---

## 10. 这次导入后保留的回滚点

当时保留了两类回滚点：

### 10.1 数据库快照回滚点

- `sub2api_before_prod_import_20260407`

这是当时在线旧库改名后的保留库。

### 10.2 本地 dump 回滚文件

- `/tmp/sub2api-cluster-before-prod-import.dump`

如果以后确认完全不需要这批回滚点，可以再考虑删除。

---

## 11. 一句话总结

这次生产库导入之所以相对稳妥，是因为没有直接“导进去就完事”，而是严格分成了：

1. 先导出旧生产库
2. 先备份当前集群库
3. 先做临时恢复演练
4. 验证 migration 能自动补齐
5. 再正式替换在线库
6. 最后验证健康、复制和登录

这套流程本质上是一次性操作记录，后面正常情况下不会频繁重复使用。
