# Sub2API 6 台服务器 HA 部署操作手册

本文记录当前这套 **3 台数据层 + 3 台应用层** 的实际部署方式，重点说明：

- 当前拓扑
- 下次如何重新部署
- `sub2api` 是否使用本地代码构建
- 本地构建后如何复制到服务器
- 数据层 / 应用层的启动顺序
- 验证方法

---

## 1. 当前拓扑

### 数据层（服务器 1~3）

| 服务器 | IP | 角色 |
|---|---|---|
| 服务器1 | `154.201.64.184` | PostgreSQL replica / Redis master / Sentinel / etcd |
| 服务器2 | `154.12.80.55` | PostgreSQL primary / Redis replica / Sentinel / etcd |
| 服务器3 | `156.225.18.73` | PostgreSQL replica / Redis replica / Sentinel / etcd |

### WireGuard 内网地址（当前已启用）

| 节点名 | 服务器 | 公网 IP | WG IP |
|---|---|---|---|
| `db1` | 服务器1 | `154.201.64.184` | `10.77.0.1` |
| `db2` | 服务器2 | `154.12.80.55` | `10.77.0.2` |
| `db3` | 服务器3 | `156.225.18.73` | `10.77.0.3` |
| `app4` | 服务器4 | `154.12.21.52` | `10.77.0.4` |
| `app5` | 服务器5 | `45.192.105.162` | `10.77.0.5` |
| `app6` | 服务器6 | `156.225.20.29` | `10.77.0.6` |

### 应用层（服务器 4~6）

| 服务器 | IP | 角色 |
|---|---|---|
| 服务器4 | `154.12.21.52` | `sub2api` + 本地 `pgproxy` |
| 服务器5 | `45.192.105.162` | `sub2api` + 本地 `pgproxy` |
| 服务器6 | `156.225.20.29` | `sub2api` + 本地 `pgproxy` |

### SSH 登录信息

> 注意：以下内容包含生产访问凭据，只应保存在你信任的私有仓库或私有环境中。

所有 6 台服务器当前统一使用：

- 端口：`22`
- 用户：`root`
- 密码：`XIANjian4SANyun`

逐台如下：

| 服务器 | IP | SSH 端口 | 用户 | 密码 |
|---|---|---|---|---|
| 服务器1 | `154.201.64.184` | `22` | `root` | `XIANjian4SANyun` |
| 服务器2 | `154.12.80.55` | `22` | `root` | `XIANjian4SANyun` |
| 服务器3 | `156.225.18.73` | `22` | `root` | `XIANjian4SANyun` |
| 服务器4 | `154.12.21.52` | `22` | `root` | `XIANjian4SANyun` |
| 服务器5 | `45.192.105.162` | `22` | `root` | `XIANjian4SANyun` |
| 服务器6 | `156.225.20.29` | `22` | `root` | `XIANjian4SANyun` |

### 当前高可用设计

- **PostgreSQL HA**：Patroni + etcd 三节点
- **Redis HA**：1 主 2 从 + 3 Sentinel
- **服务器间内部通信**：统一走 WireGuard `10.77.0.0/24`
- **应用访问 PostgreSQL**：不直接连多 host，而是先连每台应用机本地的 `pgproxy(HAProxy)`，再由 `pgproxy` 只转发到当前 Patroni 主库（后端已改用 WG IP）
- **应用访问 Redis**：应用直接使用 Redis Sentinel 自动发现主节点（Sentinel seed 已改用 WG IP）
- **公网 IP 的用途**：只保留给 SSH、应用外部访问入口、WireGuard peer endpoint

---

## 2. 这次部署是不是本地编译后复制到服务器？

**是。**

这次 `sub2api` 没有使用远程 `weishaw/sub2api:latest` 镜像，而是：

1. **在本地仓库使用本地代码构建 Docker 镜像**
2. **将构建好的镜像导出成 tar.gz**
3. **把镜像包复制到服务器**
4. **在服务器上 `docker load` 导入**
5. **再通过 `docker-compose up -d` 启动**

也就是说，应用层发版不是服务器现场 `git pull && docker build`，而是：

> **本地构建 → 本地打包镜像 → 复制到服务器 → 服务器导入镜像 → 启动容器**

这样做的好处：

- 避免服务器构建环境不一致
- 避免服务器拉依赖慢/失败
- 可以明确保证部署的是你本地当前代码

### 发布说明

从现在开始，这两个文档按下面的方式来使用：

- `deploy/HA_DEPLOYMENT_CN.md`：总手册。主要负责讲清楚整体架构、部署拓扑、HA、WireGuard、数据导入、故障切换这些背景信息。
- `deploy/RELEASE_RUNBOOK_CN.md`：发布手册。只要你准备上线、做 canary、全量发布、回滚，就看这个文档。
- `deploy/READ_WRITE_SPLIT_PILOT_CN.md`：读写分离试点方案。只要你准备推进 PostgreSQL 读写分离、reader proxy、试点菜单下沉到从库，就看这个文档。

所以请记住这条最重要的规则：

> **凡是发布相关的操作，一律以 `deploy/RELEASE_RUNBOOK_CN.md` 为准。**

这里说的“发布相关”，包括：

- 日常代码发布
- 带 migration 的发布
- 单节点 canary
- 全量发布
- 回滚

这里说的“读写分离试点相关”，包括：

- writer / reader 双连接设计
- reader proxy 的部署方式
- 哪些接口第一批走从库
- 哪些接口明确不能走从库
- 读写分离试点的验证与回滚

你可以把它简单理解成：

- `HA_DEPLOYMENT_CN.md` 是“整体说明书”
- `RELEASE_RUNBOOK_CN.md` 是“真正操作时要照着走的步骤手册”
- `READ_WRITE_SPLIT_PILOT_CN.md` 是“读写分离试点专项方案”

以后如果你只是想了解：

- 这套架构是怎么搭的
- PostgreSQL / Redis / WireGuard 是怎么工作的
- 故障切换是怎么演练的
- 数据是怎么导入的

那就看：

- `deploy/HA_DEPLOYMENT_CN.md`

如果你准备实际发布：

- 拉新代码
- 看 migration
- 构建镜像
- canary
- 全量
- 回滚

那就直接看：

- `deploy/RELEASE_RUNBOOK_CN.md`

如果你准备开始做 PostgreSQL 读写分离试点：

- 新增 reader proxy
- 增加 writer / reader 双连接
- 先把“仪表盘 + 使用记录”部分查询下沉到从库

那就直接看：

- `deploy/READ_WRITE_SPLIT_PILOT_CN.md`

---

## 3. 本次实际用到的文件

### 数据层文件

- `deploy/ha/db/docker-compose.yml`
- `deploy/ha/db/patroni/Dockerfile`
- `deploy/ha/db/patroni/entrypoint.sh`
- `deploy/ha/db/redis-start.sh`
- `deploy/ha/db/sentinel-start.sh`
- `deploy/ha/scripts/render-db-env.py`

### 应用层文件

- `deploy/ha/app/docker-compose.yml`
- `deploy/ha/app/haproxy.cfg`
- `deploy/ha/scripts/render-app-env.py`

### 代码改动（为 HA 支持做的）

- `backend/internal/config/config.go`
- `backend/internal/repository/redis.go`
- `backend/internal/setup/setup.go`

这些改动的目的：

- 支持 PostgreSQL `target_session_attrs`
- 支持 Redis Sentinel
- 让 `AUTO_SETUP` 启动流程也能识别 Redis Sentinel / 新的数据库连接方式

---

## 4. 当前关键环境变量

### 应用层关键变量

- `ADMIN_EMAIL=727965481@qq.com`
- `ADMIN_PASSWORD=XIANjian4`
- `DATABASE_HOST=pgproxy`
- `DATABASE_PORT=5432`
- `REDIS_SENTINEL_ENABLED=true`
- `REDIS_SENTINEL_MASTER_NAME=sub2api-redis`
- `REDIS_SENTINEL_ADDRS=154.201.64.184:26379,154.12.80.55:26379,156.225.18.73:26379`

### 数据层关键变量

- PostgreSQL 主业务账号：`sub2api`
- PostgreSQL 主业务密码：`XIANjian4SANyun`
- PostgreSQL 复制账号：`replicator`
- PostgreSQL 复制密码：`XIANjian4SANyun`
- PostgreSQL 数据库名：`sub2api`
- Redis master 名称：`sub2api-redis`
- Redis 密码：`XIANjian4SANyun`

### 当前应用管理员账号

- 管理员邮箱：`727965481@qq.com`
- 管理员密码：`XIANjian4`

---

## 5. 下次重新部署的标准顺序

下次部署请严格按这个顺序：

### 第一步：本地验证代码

在仓库根目录执行：

```bash
cd /media/fly/系统2/code/sanyun/temp/sub2api/backend
export GOPROXY=https://goproxy.cn,direct
export GOSUMDB=sum.golang.google.cn
go test ./...
```

如果测试不过，不要继续部署。

---

### 第二步：本地构建应用镜像

在仓库根目录执行：

```bash
cd /media/fly/系统2/code/sanyun/temp/sub2api
docker build -t sub2api:ha-20260407 -f Dockerfile .
```

如果只想沿用当前标签，也可以继续用：

```bash
sub2api:ha-20260407
```

如果你下次发新版，建议改成新标签，例如：

```bash
sub2api:ha-20260408
```

> 说明：这一步是在**本地机器**完成，不是在服务器上完成。服务器只负责接收镜像包并 `docker load`。

---

### 第三步：本地打包镜像

#### 打包应用层镜像

```bash
docker pull haproxy:3.0-alpine
docker save sub2api:ha-20260407 haproxy:3.0-alpine | gzip -1 > /tmp/sub2api-ha-bundle/app-images.tar.gz
```

#### 打包数据层镜像（仅当数据层有改动时）

```bash
docker build -f deploy/ha/db/patroni/Dockerfile -t db-patroni:latest deploy/ha/db/patroni
docker pull redis:8-alpine
docker pull quay.io/coreos/etcd:v3.5.15
docker save db-patroni:latest redis:8-alpine quay.io/coreos/etcd:v3.5.15 | gzip -1 > /tmp/sub2api-ha-bundle/db-images.tar.gz
```

---

### 第四步：生成环境文件

#### 生成 3 台数据层 `.env`

```bash
deploy/ha/scripts/render-db-env.py db1 154.201.64.184 master > /tmp/sub2api-ha-bundle/db1.env
deploy/ha/scripts/render-db-env.py db2 154.12.80.55 replica > /tmp/sub2api-ha-bundle/db2.env
deploy/ha/scripts/render-db-env.py db3 156.225.18.73 replica > /tmp/sub2api-ha-bundle/db3.env
```

#### 生成应用层 `.env`

```bash
deploy/ha/scripts/render-app-env.py > /tmp/sub2api-ha-bundle/app.env
```

---

## 6. 数据层重部署步骤（服务器 1~3）

### 6.1 同步文件

把 `deploy/ha/db/` 整个目录同步到每台机器：

```bash
rsync -az deploy/ha/db/ root@<DB_IP>:/opt/sub2api-ha/db/
scp /tmp/sub2api-ha-bundle/dbX.env root@<DB_IP>:/opt/sub2api-ha/db/.env
scp /tmp/sub2api-ha-bundle/db-images.tar.gz root@<DB_IP>:/opt/sub2api-ha/db-images.tar.gz
```

注意：

- **先 rsync 目录，再单独 scp `.env`**
- 不要反过来，否则 `.env` 可能被覆盖删掉

### 6.2 导入镜像并启动

每台数据服务器执行：

```bash
gunzip -c /opt/sub2api-ha/db-images.tar.gz | docker load
cd /opt/sub2api-ha/db
docker compose --env-file .env up -d
```

如果是第一次部署，建议先检查 `.env` 是否真的在服务器上：

```bash
cd /opt/sub2api-ha/db
ls -la
cat .env
```

如果需要清理旧容器：

```bash
docker compose --env-file .env down --remove-orphans
docker compose --env-file .env up -d
```

---

## 7. 应用层重部署步骤（服务器 4~6）

### 7.1 同步文件

```bash
rsync -az deploy/ha/app/ root@<APP_IP>:/opt/sub2api-ha/app/
scp /tmp/sub2api-ha-bundle/app.env root@<APP_IP>:/opt/sub2api-ha/app/.env
scp /tmp/sub2api-ha-bundle/app-images.tar.gz root@<APP_IP>:/opt/sub2api-ha-app-images.tar.gz
```

### 7.2 导入镜像并启动

```bash
gunzip -c /opt/sub2api-ha-app-images.tar.gz | docker load
cd /opt/sub2api-ha/app
docker-compose down --remove-orphans || true
docker-compose up -d
```

如果是第一次部署，建议先检查：

```bash
cd /opt/sub2api-ha/app
ls -la
cat .env
```

这里应用层机器当前装的是 `docker-compose`，不是 `docker compose` 插件，所以仍然使用：

```bash
docker-compose up -d
```

---

## 8. 首次启动时会发生什么

应用第一次成功连上数据库和 Redis 后，会自动执行：

1. `AUTO_SETUP`
2. 初始化数据库结构
3. 创建/检查管理员账号
4. 生成并写入 `/app/data/config.yaml`
5. 创建 `.installed` 锁文件

如果数据库里已经有数据，日志里可能会出现：

- `Admin user already exists, skipping admin bootstrap`

这是正常行为，表示不会覆盖现有管理员密码。

---

## 9. 当前验证方法

### PostgreSQL HA 验证

在主库服务器（当前是 `154.12.80.55`）执行：

```bash
docker exec sub2api-patroni psql -U sub2api -d postgres -c "select client_addr, state, sync_state, application_name from pg_stat_replication;"
curl http://127.0.0.1:8008/leader
```

在副本服务器执行：

```bash
curl http://127.0.0.1:8008/replica
docker exec sub2api-patroni psql -U sub2api -d postgres -c "select pg_is_in_recovery();"
```

### Redis HA 验证

主从状态：

```bash
docker exec sub2api-redis redis-cli -a XIANjian4SANyun info replication
```

Sentinel 主节点发现：

```bash
docker exec sub2api-redis-sentinel redis-cli -p 26379 SENTINEL get-master-addr-by-name sub2api-redis
```

### 应用健康检查

每台应用服务器执行：

```bash
curl http://127.0.0.1:8080/health
docker ps
docker logs --tail 100 sub2api
```

### 登录验证

可在任一应用节点执行：

```bash
python3 - <<'PY'
import json, urllib.request
url='http://127.0.0.1:8080/api/v1/auth/login'
data=json.dumps({'email':'727965481@qq.com','password':'XIANjian4'}).encode()
req=urllib.request.Request(url, data=data, headers={'Content-Type':'application/json'})
with urllib.request.urlopen(req, timeout=15) as resp:
    print(resp.status)
    print(resp.read().decode())
PY
```

返回 `200` 且包含 `access_token` / `refresh_token` 即表示成功。

---

## 10. 这次踩过的坑（下次注意）

### 1）不要让应用直接把 `DATABASE_HOST` 写成逗号分隔 IP

虽然运行期代码已支持部分多 host 能力，但 `AUTO_SETUP` 启动链一开始并不兼容这一方式。最终采用了更稳的方式：

- 应用连本地 `pgproxy`
- `pgproxy` 再选当前 Patroni 主库

这次实际报错是：

```text
lookup 154.201.64.184,154.12.80.55,156.225.18.73: no such host
```

原因：应用启动阶段的 `AUTO_SETUP` 最开始不支持直接把多个 IP 塞进 `DATABASE_HOST`。

### 2）应用层的 `.env` 不能被 rsync 覆盖删掉

正确顺序：

1. `rsync deploy/ha/app/`
2. `scp app.env -> /opt/sub2api-ha/app/.env`

这次就真的踩到了这个坑：

- 先传了 `.env`
- 后执行 `rsync --delete`
- 结果 `.env` 被删掉

所以顺序必须固定。

### 3）数据层的 `.env` 也一样

正确顺序：

1. `rsync deploy/ha/db/`
2. `scp dbX.env -> /opt/sub2api-ha/db/.env`

数据层同样踩过一次，现象是：

- `docker compose` 启动时出现大量：

```text
The "XXX" variable is not set. Defaulting to a blank string.
```

本质就是 `.env` 没有被正确保留下来。

### 4）Patroni 容器必须以 `postgres` 用户执行

否则会报：

```text
initdb: error: cannot be run as root
```

修复方式：

- 在 `deploy/ha/db/patroni/Dockerfile` 里安装 `su-exec`
- 在 `deploy/ha/db/patroni/entrypoint.sh` 里 `chown`
- 最终用：

```bash
su-exec postgres patroni /run/patroni/patroni.yml
```

### 5）远端直接拉公共镜像不稳定

所以后面改成：

- 本地 pull / build
- 本地 `docker save`
- 远端 `docker load`

这样最稳。

这次实际遇到过：

```text
failed to resolve reference "docker.io/library/redis:8-alpine": ... i/o timeout
```

所以后面统一改成镜像本地打包上传。

### 6）Patroni 构建时，Alpine 的 pip 会被 PEP 668 拦住

这次实际错误：

```text
error: externally-managed-environment
```

修复方式是在 Patroni Dockerfile 中使用：

```bash
pip3 install --break-system-packages --no-cache-dir "patroni[etcd3]" "psycopg[binary]"
```

### 7）Patroni 的 etcd hosts 生成格式要小心

这次曾出现：

```text
ValueError: Invalid IPv6 URL
```

原因是 `etcd3.hosts` 生成格式不符合 Patroni 预期。

最终确认稳定可用的方式是：

```yaml
etcd3:
  hosts: 154.201.64.184:2379,154.12.80.55:2379,156.225.18.73:2379
```

### 8）应用第一次启动时，如果主库里没有 `sub2api` 数据库，会反复重启

这次实际错误：

```text
pq: database "sub2api" does not exist
```

修复方式有两层：

1. 代码里修正 `AUTO_SETUP` 的数据库测试逻辑，让它先连 `postgres` 库，再检查/创建 `sub2api`
2. 实际部署过程中，也可以手动在主库先建库：

```bash
docker exec sub2api-patroni psql -U sub2api -d postgres -c "CREATE DATABASE sub2api;"
```

### 9）应用启动时，Redis 检测必须支持 Sentinel，否则会误连 localhost:6379

这次实际错误：

```text
redis connection failed: ping failed: dial tcp [::1]:6379: connect: connection refused
```

原因：

- 运行期代码已经支持 Sentinel
- 但 `AUTO_SETUP` 的旧逻辑仍然用单机 Redis 检测

修复方式：

- 修改 `backend/internal/setup/setup.go`
- 让 `AUTO_SETUP` 也走 Redis Sentinel 的 `UniversalClient`

### 10）HAProxy 只允许 Patroni 主库进入后端池

当前 `deploy/ha/app/haproxy.cfg` 使用：

```text
option httpchk GET /leader
http-check expect status 200
```

这意味着：

- 主库 `/leader` 返回 200
- 副本返回 503
- HAProxy 自动只把流量发给主库

所以看到副本在 HAProxy 日志里显示：

```text
Layer7 wrong status, code: 503
```

这是**正常现象**，不是故障。

### 11）这次部署的实际大顺序

这次真实部署顺序如下：

1. 修改代码支持 PG/Redis HA
2. 本地跑 `go test ./...`
3. 本地构建 `sub2api` 镜像
4. 本地构建 Patroni 镜像
5. 本地 `docker save` 打包镜像
6. 先部署服务器 1~3 数据层
7. 验证 Patroni leader/replica、Redis master/slave、Sentinel
8. 再部署服务器 4~6 应用层
9. 修复 AUTO_SETUP 对数据库/Redis HA 的兼容问题
10. 重新构建应用镜像并重发应用层
11. 验证 `/health`
12. 验证管理员登录

---

## 11. 当前最终状态

- PostgreSQL：`db1` 主，`db2/db3` 从
- Redis：`154.201.64.184` 主，另外两台从
- 应用：`154.12.21.52` / `45.192.105.162` / `156.225.20.29` 均健康
- 管理员登录验证通过：
  - `727965481@qq.com`
  - `XIANjian4`

### 当前已验证通过的关键点

- `go test ./...` 通过
- 本地 Docker build 成功
- PostgreSQL 主从复制正常
- Redis Sentinel 主节点发现正常
- 三台应用节点 `/health` 正常
- 管理员登录接口返回 `200`
- 4/5/6 号机升级到 Docker `29.3.1` + Compose `v5.1.1` 后，`sub2api` 未受影响
- WireGuard 六节点组网正常，应用/数据库内部流量已切到 `10.77.0.x`

### 当前数据库保留状态

- 当前在线业务库：`sub2api`
- 导入前回滚快照库：`sub2api_before_prod_import_20260407`

如果需要快速回滚，可以基于这个快照库重新切回。

### 当前本地备份文件

部署机本地保留了两份 dump：

- 生产库导出：`/tmp/sub2api-prod.dump`
- 导入前当前集群快照：`/tmp/sub2api-cluster-before-prod-import.dump`

如果后续确认不再需要，可以手动删除。

---

## 12. 下次如果只改业务代码，不改数据层，最简流程

如果你只是更新 `sub2api` 代码，而不改 PostgreSQL / Redis / Sentinel / Patroni：

1. 本地 `go test ./...`
2. 本地 `docker build -t sub2api:<new-tag> -f Dockerfile .`
3. 本地 `docker save sub2api:<new-tag> haproxy:3.0-alpine | gzip -1 > /tmp/app-images.tar.gz`
4. 同步 `deploy/ha/app/`
5. 同步新的 `.env`（如果改了）
6. 上传 `app-images.tar.gz`
7. 服务器执行：

```bash
gunzip -c /tmp/app-images.tar.gz | docker load
cd /opt/sub2api-ha/app
docker-compose down --remove-orphans || true
docker-compose up -d
```

8. 验证 `/health`

---

## 13. Docker / Docker Compose 升级后的验证方法

如果以后再升级 4/5/6 号机 Docker 或 Compose，按下面命令验证：

```bash
for host in 154.12.21.52 45.192.105.162 156.225.20.29; do
  ssh root@$host '
    docker --version
    docker compose version || docker-compose --version
    docker ps --format "table {{.Names}}\t{{.Status}}" | grep sub2api
    curl -fsS http://127.0.0.1:8080/health
  '
done
```

本次验证结果：

- Docker：`29.3.1`
- Docker Compose：`v5.1.1`
- 三台应用节点全部 `healthy`

---

## 14. 生产库导入说明

生产库导入相关内容已经单独抽离到：

- `deploy/DB_IMPORT_ONCE_CN.md`

原因是：

- 这部分属于**一次性操作**
- 正常情况下，后续不会频繁重复执行
- 继续放在主手册里，会让 `HA_DEPLOYMENT_CN.md` 显得过重

所以从现在开始：

- 如果你要看 PostgreSQL HA、WireGuard、故障切换、整体部署结构，就继续看 `deploy/HA_DEPLOYMENT_CN.md`
- 如果你要回看“当时生产库是怎么导进来的”，就看 `deploy/DB_IMPORT_ONCE_CN.md`

---

## 15. PostgreSQL 主库故障切换演练记录（已实际执行）

本节记录一次真实的 Patroni 主库故障切换演练，方便后续复现。

### 15.1 演练前基线

演练前状态：

- `db2 / 154.12.80.55`：**primary**
- `db1 / 154.201.64.184`：replica
- `db3 / 156.225.18.73`：replica
- 三台应用节点 `sub2api` 全部 `healthy`

基线验证命令：

```bash
ssh root@154.12.80.55 'curl http://127.0.0.1:8008'
ssh root@154.12.80.55 'docker exec sub2api-patroni psql -U sub2api -d postgres -c "select client_addr, state, sync_state, application_name from pg_stat_replication;"'

for host in 154.12.21.52 45.192.105.162 156.225.20.29; do
  ssh root@$host 'docker ps --format "table {{.Names}}\t{{.Status}}" | grep sub2api && curl -fsS http://127.0.0.1:8080/health'
done
```

### 15.2 演练触发方式

这次采用的是**受控故障**：

- 直接停止原主库上的 Patroni 容器

执行命令：

```bash
ssh root@154.12.80.55 'docker stop sub2api-patroni'
```

### 15.3 演练结果

切换结果：

- 原主 `db2` 停止后
- `db3 / 156.225.18.73` 自动当选为新主库
- timeline 从 `1` 变成 `2`

演练后新主查看结果：

```json
"role": "primary",
"name": "db3",
"timeline": 2
```

同时：

- `db1` 继续作为副本跟随新主 `db3`
- 三台应用节点 `/health` 持续正常
- 管理员登录接口返回 `200`

### 15.4 故障切换期间验证命令

查看新主：

```bash
ssh root@156.225.18.73 'curl -fsS http://127.0.0.1:8008'
```

查看新主复制关系：

```bash
ssh root@156.225.18.73 'docker exec sub2api-patroni psql -U sub2api -d postgres -c "select client_addr, state, sync_state, application_name from pg_stat_replication;"'
```

查看 surviving replica：

```bash
ssh root@154.201.64.184 'curl -fsS http://127.0.0.1:8008/replica'
```

验证应用健康：

```bash
for host in 154.12.21.52 45.192.105.162 156.225.20.29; do
  ssh root@$host 'curl -fsS http://127.0.0.1:8080/health'
done
```

验证管理员登录：

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

本次返回：`200`

### 15.5 原主恢复为副本

演练不是只看切主，还要确认旧主能回来。

恢复方式：

```bash
ssh root@154.12.80.55 'docker start sub2api-patroni'
```

恢复后验证：

```bash
ssh root@154.12.80.55 'curl -fsS http://127.0.0.1:8008/replica'
ssh root@154.12.80.55 'docker exec sub2api-patroni psql -U sub2api -d postgres -c "select pg_is_in_recovery();"'
```

期望结果：

- `role=replica`
- `pg_is_in_recovery = true`

### 15.6 演练结束后的最终状态

本次演练结束后，当前 PostgreSQL 角色如下：

- `db3 / 156.225.18.73`：**primary**
- `db1 / 154.201.64.184`：replica（streaming）
- `db2 / 154.12.80.55`：replica（streaming）

最终复制检查：

```bash
ssh root@156.225.18.73 'docker exec sub2api-patroni psql -U sub2api -d postgres -c "select client_addr, state, sync_state, application_name from pg_stat_replication order by client_addr;"'
```

本次实际结果：

- `154.12.80.55 / db2`：`streaming`
- `154.201.64.184 / db1`：`streaming`

### 15.7 本次演练结论

本次 PostgreSQL 主库故障切换演练已确认：

1. 停掉原主 Patroni 后，集群可自动选主
2. 应用层 `pgproxy` 能自动跟随新主
3. 三台 `sub2api` 在切换期间继续健康
4. 管理员登录可正常成功
5. 旧主恢复后可重新作为副本加入集群

也就是说，这套 PostgreSQL HA 当前**已经具备真实自动切主能力**。

---

## 16. WireGuard 异地组网部署记录（已实际执行）

本节记录当前 6 台服务器的 WireGuard 组网方式，以及 HA 栈如何切到 WG IP。

### 16.1 目标

目标是让以下内部通信全部改走 WireGuard：

- etcd peer/client
- Patroni REST / PostgreSQL connect_address
- Redis replication
- Redis Sentinel seed / announce
- HAProxy 到 PostgreSQL 后端
- app 到 Redis Sentinel

不再让这些内部链路依赖公网 IP。

### 16.2 当前 WG 网段

- 网段：`10.77.0.0/24`
- 监听端口：`51820/udp`

### 16.3 当前仓库新增文件

- `deploy/wireguard/inventory.json`
- `deploy/wireguard/install-wireguard.sh`
- `deploy/wireguard/render-wg-config.py`

这些文件作用分别是：

- `inventory.json`：定义 6 个节点的公网 IP 和 WG IP
- `install-wireguard.sh`：在 Ubuntu 服务器安装 `wireguard` / `wireguard-tools`
- `render-wg-config.py`：根据 inventory 和密钥生成每台机器的 `wg0.conf`

### 16.3.1 当前 6 台机器正在使用的 WireGuard 密钥

> **极高敏感信息**：以下是当前生产环境正在使用的 WireGuard 私钥/公钥。任何拿到这些私钥的人都可以伪装成对应节点加入内网。只能保存在你自己可控的私有仓库或加密环境中，绝对不要外传。

| 节点名 | WG IP | 私钥 | 公钥 |
|---|---|---|---|
| `db1` | `10.77.0.1` | `OBGPbWl26xOsyOAM1ARdzqO9AGQIcfNp+7xyN1KtuXw=` | `vArJzfH3zg+Lm2wTpQz0dXfw/wvs6ZnLN0WBfZpjYV8=` |
| `db2` | `10.77.0.2` | `+Hd+Erriox+zlgGGBNdeeiszi0STp1nKRRe5IgYNI1U=` | `aWIY9jmaetFvAsDWXj+v+rSTmJXL7zdvhEdSlmX5hVA=` |
| `db3` | `10.77.0.3` | `SN/L0Hd/LyRVRSzQOTvPDkqR8mBz4JTh2RW4xejhIko=` | `4dFCKDRsh0JE4inn3CL670vQn3inyIzHjJKTXnr+Vk8=` |
| `app4` | `10.77.0.4` | `WLBGforq2LbvoCc14BR1Q8bDh+YCK3WFDoNH6eRh6Xs=` | `yif/6wy/HU0yCzBCTJqbpsjzPNAhihYQuwjzsN4QoDo=` |
| `app5` | `10.77.0.5` | `eE9497nVD/R4ipcImSqBrslJ97YycZ+gBN5TcjlDFUk=` | `5jh2S14mGc+1k+0ndsZOvKyqchfSvORn+ijCOllsGBc=` |
| `app6` | `10.77.0.6` | `QPiq/oZuIxK0ZELhJ2KTBq8XscoIZg7ZbNT08XpI700=` | `po4sOJxRhwdbi9JxBw6TvbPLdawwuVZtudHbZnmwNhw=` |

如果后续新增服务器，需要做的是：

1. 给新服务器生成新的私钥/公钥
2. 在现有 6 台机器的 `wg0.conf` 中新增一个 `[Peer]`
3. 在新服务器的 `wg0.conf` 中把现有 6 台都写成 `[Peer]`
4. 给新服务器分配一个新的 WG IP（例如 `10.77.0.7`）
5. 重载所有相关节点的 WireGuard 配置

### 16.4 本次实际做了什么

#### 第一步：在 6 台服务器安装 WireGuard

每台执行了：

```bash
apt-get update
apt-get install -y wireguard wireguard-tools
```

#### 第二步：在每台机器生成密钥

每台在 `/etc/wireguard/` 下生成：

- `privatekey`
- `publickey`

#### 第三步：生成并下发 `wg0.conf`

每台都生成全互联配置：

- `AllowedIPs` 为对端的 `/32`
- `Endpoint` 仍然使用对端公网 IP + `51820`
- 真正的数据流量走 WG IP
- 当前 6 台机器的 `wg0` 配置统一把 `MTU` 固定为 `1280`
- `deploy/wireguard/render-wg-config.py` 已去掉 `SaveConfig = true`，避免运行中的配置被 `wg-quick` 回写覆盖，后续应继续以仓库渲染结果为准

也就是说，当前这套 WireGuard 配置的生成口径是：

- **6 台机器的 `wg0` 都使用 `MTU = 1280`**
- **配置文件不再写 `SaveConfig = true`**
- **如需调整 peer / endpoint / MTU，应修改仓库里的 inventory / 渲染脚本后重新生成并下发，而不是直接依赖机器上运行时回写**

#### 第四步：启动 `wg-quick@wg0`

实际执行：

```bash
systemctl enable --now wg-quick@wg0
```

#### 第五步：验证 WG 联通

通过 `ping 10.77.0.x` 和 `wg show` 验证 6 节点组网成功。

### 16.5 HA 栈是怎么切到 WG IP 的

#### 数据层

`deploy/ha/scripts/render-db-env.py` 已改成输出：

- `NODE_IP=10.77.0.x`
- `ETCD_INITIAL_CLUSTER=db1=http://10.77.0.1:2380,...`
- `ETCD_HOSTS=10.77.0.1:2379,10.77.0.2:2379,10.77.0.3:2379`
- `REDIS_MASTER_HOST=10.77.0.1`

这意味着：

- etcd 集群成员互相认识的是 WG IP
- Patroni 的 `connect_address` 用的是 WG IP
- PostgreSQL 复制源地址用的是 WG IP
- Redis / Sentinel 内部链路用的是 WG IP

#### 应用层

`deploy/ha/app/haproxy.cfg` 已改成：

```text
server db1 10.77.0.1:5432 check port 8008
server db2 10.77.0.2:5432 check port 8008
server db3 10.77.0.3:5432 check port 8008
```

`deploy/ha/scripts/render-app-env.py` 已改成：

```text
REDIS_SENTINEL_ADDRS=10.77.0.1:26379,10.77.0.2:26379,10.77.0.3:26379
```

所以现在：

- app 访问 PostgreSQL：`sub2api -> 本机 pgproxy -> 10.77.0.x`
- app 访问 Redis Sentinel：直接访问 `10.77.0.1/2/3:26379`

### 16.6 数据层切换到 WG 的注意点

WireGuard 切换最敏感的是 etcd。

这次不是粗暴重启，而是先在线更新 etcd member peer URL：

```bash
etcdctl member update <member-id> --peer-urls=http://10.77.0.x:2380
```

然后才按节点滚动重启 `db1 -> db2 -> db3`。

这一步非常关键，否则 etcd 可能还把老公网地址当成员地址。

### 16.7 应用层切换到 WG 时踩到的坑

4/5/6 号机升级 Docker 后，老的 `docker-compose` v1 在重建容器时会报：

```text
KeyError: 'ContainerConfig'
```

以及旧 project 名残留时会报 container name conflict。

修复方式：

1. 不再使用 `docker-compose` v1
2. 改用新版 `docker compose`
3. 如有旧残留容器，先：

```bash
docker compose down --remove-orphans
docker ps -a --format '{{.Names}}' | grep sub2api-pgproxy | xargs -r docker rm -f
docker compose up -d
```

### 16.8 当前已验证通过的 WG 结果

#### 1）WG 接口状态

6 台机器都有：

- `wg0`
- 对端 handshake 正常

#### 2）PostgreSQL 客户端地址已经变成 WG 地址

在当前主库里看到：

- app 连接源地址：`10.77.0.4`
- app 连接源地址：`10.77.0.5`
- app 连接源地址：`10.77.0.6`

#### 3）PostgreSQL 复制地址已经变成 WG 地址

当前主库看到副本地址：

- `10.77.0.2`
- `10.77.0.3`

#### 4）三台应用节点健康

三台 `/health` 全部返回：

```json
{"status":"ok"}
```

#### 5）管理员登录成功

管理员 `727965481@qq.com / XIANjian4` 登录返回 `200`

#### 6）HAProxy PostgreSQL 后端确认已全部改成 WG IP

三台应用节点部署后的 `/opt/sub2api-ha/app/haproxy.cfg` 都已确认：

```text
server db1 10.77.0.1:5432 check port 8008
server db2 10.77.0.2:5432 check port 8008
server db3 10.77.0.3:5432 check port 8008
```

也就是说，现在 PostgreSQL 实际访问链路已经明确是：

```text
sub2api -> 本机 pgproxy(HAProxy) -> 10.77.0.1/10.77.0.2/10.77.0.3
```

#### 7）主库看到的 app 连接源地址也是 WG IP

在当前主库里实际看到的客户端来源地址为：

- `10.77.0.4`
- `10.77.0.5`
- `10.77.0.6`

这进一步证明：

- app 到 PostgreSQL 的内部链路已经不再走公网 IP
- 现在 app 到 PostgreSQL 的真实流量是走 WireGuard 内网

### 16.9 WG 下的 PostgreSQL 故障切换演练结果

本次已经在 WG 改造完成后再次做了主库切换演练。

过程：

- 停掉 `db2` 的 Patroni
- `db1` 自动升主
- 三台应用保持 `/health` 正常
- 管理员登录继续成功
- 最后把 `db2` 拉回副本

故障切换演练刚结束时的状态：

- `db1 / 10.77.0.1`：**primary**
- `db2 / 10.77.0.2`：replica（streaming）
- `db3 / 10.77.0.3`：replica（streaming）

这说明：

- WireGuard 改造后，HA 仍然正常
- PostgreSQL 自动切主能力没有被破坏
- 应用层在 WG 内网地址下继续可用

### 16.9.1 当前最新运行状态补充（读写分离试点期间）

后续在推进 PostgreSQL 读写分离试点时，又发现了一个新的运行状态变化，需要单独记录：

- `db2` 后来不再是简单的 `catchup`
- 它实际暴露出了复制异常
- 一度进入 Patroni 管理下的 **reinit / creating replica** 流程
- 主库也确实对 `db2` 执行了 `pg_basebackup`

这说明：

> `db2` 当时不是“未知挂死”，而是 Patroni 正在按受控方式重建这个副本。

也就是说，当时对 `db2` 的正确理解不是：

- “它只是慢一点，等等就好”

而是：

- “它处于 Patroni 管理下的可解释、可恢复的重建状态”

在 `db2` 完全恢复成稳定 `streaming` 之前，当时的实践建议是：

- **不要把 reader 流量依赖在 db2 上**
- 优先使用健康副本 `db3`

后续实际结果是：

- 先定位到 WG 大包传输 / 复制链路异常
- 把 6 台机器的 WireGuard `MTU` 收紧到 `1280`
- 对 `db2` 做 cleaner rebuild
- 最终 `db2` 已恢复成稳定 `streaming`

所以当前最新状态已经不再是“重建中”，而是：

- `db1`：Leader
- `db2`：Replica（streaming）
- `db3`：Replica（streaming）

也就是说，现在 `db2` 已重新回到可用副本集合中。

### 16.9.2 副本进入 catchup 后的处理规则

这里需要特别明确一条运维口径：

当前建议不是：

- 一看到 `catchup` 就立刻重建

也不是：

- 无限等待它自己恢复

而是：

1. **先观察它是否还能自愈**
   - 看 `pg_last_wal_receive_lsn()` / `pg_last_wal_replay_lsn()` 是否持续前进
   - 看 walreceiver 是否稳定
   - 看 lag 是否持续缩小

2. **如果已经确认无法恢复，再执行重建**
   - 例如长时间停在旧 LSN
   - walreceiver 周期性 timeout 重连
   - lag 不下降
   - 已经不适合作为 reader / failover 候选

3. **重建时使用 Patroni 管理下的 `reinit`**

也就是说，当前应该把这条规则理解成：

> **副本进入 catchup 后，先判断它是否还能自行恢复；如果已经确认无法恢复，再执行 reinit。**

### 16.9.3 关于自动 failover 的安全判断

需要特别注意：

- `creating replica` 状态的副本，不应视为可用 failover 候选
- `catchup` 状态本身，也不要被误认为“天然安全，不会被提升”

最安全的运维口径是：

> **只把稳定 `streaming` 的副本当作可信 failover 候选。**

按当前最新状态理解：

- `db2`：已恢复为健康 streaming 副本
- `db3`：健康，可作为可靠副本

而在 `db2` 出问题的那个阶段，正确的临时口径仍然是：

- 不把 `db2` 当作可信 reader / failover 候选

### 16.9.4 当时对 db2 重建卡住的判断

目前已经查到的情况是：

- `db2` 长时间停在 `creating replica`
- Patroni 日志持续显示 `reinitialize in progress`
- 数据目录几乎还是空的
- 容器内 `pg_basebackup` 进程存在
- 复制认证手工测试可以通过
- 但主库上看不到稳定持续的 `pg_basebackup` / `replicator` 会话来自 `db2`

这说明当前更像是：

> `db2` 的 reinit 已经启动，但重建流程卡在 **basebackup 启动或建立稳定传输之前的阶段**。

所以现在的判断不是“完全未知”，而是：

- 不是简单 catchup
- 也不是健康 streaming
- 而是一个**处于 Patroni 管理下、但当前仍未完成的副本重建问题**

### 16.10 下次如果某台服务器公网 IP 改了，应该怎么处理

如果公网 IP 变了，现在不需要再修改 PostgreSQL / etcd / Sentinel / HAProxy 里的内部地址，**只需要处理 WireGuard endpoint**。

也就是主要修改对应节点的 `wg0.conf`：

- 对端 `Endpoint = 新公网IP:51820`

因为内部业务地址已经固定在：

- `10.77.0.1`
- `10.77.0.2`
- `10.77.0.3`
- `10.77.0.4`
- `10.77.0.5`
- `10.77.0.6`

这就是这次改造最大的收益。

---

## 17. 灰故障处理策略（当前决定：先保持保守，不调激进自动切主）

这一节记录当前对“网络卡顿 / 数据库很慢但未完全宕机”场景的处理策略，避免后面忘记。

### 17.1 已确认的事实

#### PostgreSQL

- **WG 改造后，PostgreSQL 已再次做过主库故障切换演练**
- 已验证：
  - 停掉原主 Patroni 后可自动选出新主
  - 应用层继续健康
  - 管理员登录继续成功

#### Redis

- **WG 改造后，Redis / Sentinel 已切到 WG IP 并验证可用**
- 已验证：
  - Redis / Sentinel 内部链路走 WG IP
  - app 使用 WG IP 的 Sentinel seed
  - Sentinel 主节点发现正常
- **但目前没有单独做过一轮“WG 之后的 Redis 自动切主演练”**

### 17.2 当前 PostgreSQL / HAProxy / Redis 关键参数

#### Patroni

当前配置：

```text
ttl = 30
loop_wait = 10
retry_timeout = 10
maximum_lag_on_failover = 1048576
```

#### HAProxy（pgproxy）

当前配置：

```text
timeout connect 5s
timeout client 1m
timeout server 1m
option httpchk GET /leader
http-check expect status 200
```

#### Redis Sentinel

当前配置：

```text
down-after-milliseconds = 5000
failover-timeout = 60000
parallel-syncs = 1
quorum = 2（3 个 Sentinel 场景下的实际判断逻辑）
```

### 17.3 30 秒无返回、60 秒成功，这种情况 PostgreSQL 会不会自动切主？

**通常不会。**

原因是当前 PostgreSQL HA 不是按“某个 SQL 是否慢了 30 秒”来决定切主，而是按：

- Patroni 还能不能正常跑控制循环
- Patroni 还能不能和 etcd 正常续租 leader lock
- PostgreSQL 是否从 Patroni 的角度仍然健康

所以如果出现下面这种灰故障：

- 查询/写入 30 秒没有返回
- 甚至 60 秒后又成功了
- Patroni 进程没死
- PostgreSQL 没死
- Patroni 到 etcd 的链路还正常

那么这种情况下：

- **Patroni 往往不会自动切主**
- **HAProxy 也可能继续把流量发给这个主库**（只要 `/leader` 仍然返回 200）

也就是说：

> **“SQL 很慢” ≠ “一定自动切主”**

### 17.4 当前为什么先不调激进自动切主

当前决定：

> **先保持保守，不把 PostgreSQL 调成“30 秒慢就自动切主”**

原因：

1. 30~60 秒的灰故障可能是可恢复的慢故障
2. 如果 Patroni 调得太激进，会把“可恢复卡顿”变成“误切主”
3. 误切主会带来：
   - 应用抖动
   - 主从重新追赶
   - 异步复制下的数据丢失风险
   - 更复杂的恢复过程

所以当前策略是：

- **PostgreSQL failover 保守**
- **不因为 30 秒 SQL 慢就主动追求自动切主**

### 17.5 当前最安全的策略

当前决定采用以下策略：

#### 1）Patroni 先保持现状

先不调整：

- `ttl`
- `loop_wait`
- `retry_timeout`
- `maximum_lag_on_failover`

#### 2）继续用 `/leader` 判断 PostgreSQL 主角色

当前 HAProxy 的职责是：

- 判断谁是 Patroni 认可的主库
- 把写流量发给当前主库

它**不是**用来判断“数据库是不是足够快”。

所以当前认知要明确：

- `/leader = 200` 代表它还是主
- 不代表 SQL 一定快

#### 3）真正处理灰故障，优先靠代理和应用超时

当前更推荐把“慢故障”首先当成：

- 路由问题
- 连接超时问题
- 查询超时问题

而不是立刻把它当成“主库选举问题”。

所以后续如果真要优化，优先方向应该是：

- app 连接/查询超时
- HAProxy 检查与超时
- 监控和观测

而不是先去把 Patroni 调得更激进。

### 17.6 Redis 的灰故障策略

Redis Sentinel 和 PostgreSQL 不一样。

Sentinel 更偏向“从 Sentinel 的视角看 master 是否不可达”，而不是看 app 的体验。

也就是说：

- 某些客户端觉得 Redis 很慢
- 不代表 Sentinel 一定会切主

Sentinel 一般会在这些条件满足时才更可能 failover：

- master 在多个 Sentinel 视角里超过 `down-after-milliseconds` 不可达
- 达到 quorum
- 有多数 Sentinel 完成协调

因此当前也不建议先把 Redis Sentinel 调得更激进，尤其是在 **WG 改造后还没有单独做 Redis failover drill** 的前提下。

### 17.7 当前结论（记住这个就行）

当前我们的策略是：

1. **PostgreSQL：WG 后已验证自动切主正常**
2. **Redis：WG 后已验证可用，但还没单独演练自动切主**
3. **遇到 30~60 秒卡顿但最终成功的灰故障，先不追求激进自动切主**
4. **Patroni 保持保守参数**
5. **后续如果要优化，优先看 app timeout / HAProxy timeout / SQL 探测，而不是先改 Patroni 选主参数**

### 17.8 后续如果真的要调优，建议顺序

如果以后你确实发现“主库经常不挂但会卡很久”，建议按这个顺序做：

1. 先加观测：
   - Patroni `/leader` / `/health` / `/liveness`
   - etcd 延迟
   - PostgreSQL wait event / 锁等待 / 复制延迟
   - HAProxy backend flap
2. 再调 app 查询超时 / 连接超时
3. 再考虑增强 HAProxy 检查
4. 最后才考虑是否微调 Patroni timing

不要反过来上来就调 Patroni 更激进。

---

如果下次你要，我可以继续把这份手册再补成：

- **一键重部署脚本**
- **故障切换演练步骤**
- **生产 Postgres 数据导入步骤**
