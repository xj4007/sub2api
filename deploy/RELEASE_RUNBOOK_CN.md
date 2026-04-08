# Sub2API 发布手册（精细版）

> 这份文档是 **真正执行发布时要照着走的操作手册**。
>
> `deploy/HA_DEPLOYMENT_CN.md` 负责介绍整体架构、部署拓扑、HA、WireGuard、故障切换、导库等背景信息；
> **只要涉及发布、canary、全量、回滚，一律以 `deploy/RELEASE_RUNBOOK_CN.md` 为准。**

这份文档主要解决下面几类事情：

- 日常业务代码发布
- 带数据库 migration 的发布
- 单节点 canary 验证
- 全量发布
- 回滚判断

---

## 1. 发布的基本思路

### 1.1 发布时间

默认建议：

- **尽量选低峰时段发布**

原因很简单：

- 低峰时段更容易观察异常
- 单节点 canary 的风险更低
- 万一需要回滚，影响也更小

### 1.2 当前适合的发布方式

你现在这套环境，最适合的发布方式是：

- **先发 1 台应用节点做 canary，确认没问题后再全量**

这里的“灰度”更准确地说是：

- **节点级 canary 发布**

不是下面这种更复杂的流量灰度：

- 按百分比分流
- 按 cookie/header 精细分流
- 按用户群分批放量

因为当前这套环境没有统一的流量型 LB，所以最稳的做法就是：

> **先上一台，验证通过，再发另外两台。**

### 1.3 当前应用节点

当前应用节点有 3 台：

- `154.12.21.52`
- `45.192.105.162`
- `156.225.20.29`

默认建议固定把这台作为 canary 节点：

- **`154.12.21.52`**

原因：

- 现在很多健康检查、登录验证、手工排查都已经习惯先看这台
- 以后固定一台做首发，也更容易形成操作习惯

### 1.4 当前发布方式到底是什么

当前发布不是在服务器上现场 `git pull && docker build`，而是：

1. 本地拉最新代码
2. 本地先测试
3. 本地构建 Docker 镜像
4. 本地 `docker save` 打包镜像
5. 把镜像包传到服务器
6. 服务器上 `docker load`
7. 先发单节点 canary
8. 验证通过后再全量

也就是说，真正控制发布结果的是：

> **本地构建 + 单节点验证 + 再全量**

---

## 2. 每次发布前，先判断是哪一种发布

每次准备发布之前，不要急着构建镜像，先判断这次属于哪一类：

1. **无 DB 改动**
2. **有兼容 DB 改动**
3. **有不兼容 DB 改动**

这一步是整个发布流程里最重要的判断点。

因为：

- 如果判断错了
- 后面的发布顺序就可能错
- 一旦数据库变更和应用代码顺序搞反，最容易出线上问题

---

## 3. 怎么判断这次有没有 DB 改动

这里不要只看：

> “我自己有没有改数据库？”

这不够。

每次发布前，必须一起检查下面 3 个来源：

1. **你这次自己改的代码**
2. **你刚从上游拉下来的代码**
3. **`backend/migrations/` 里有没有新增 migration 文件**

也就是说：

> **只要这次发布包含新的 migration 文件，就必须按“有 DB 改动”处理。**

### 3.1 每次必查目录

```bash
backend/migrations/
```

### 3.2 检查时要看什么

你至少要确认：

- 这次有没有新增 `.sql`
- 新增 migration 主要是在做什么

重点看它是下面哪一种：

- 加字段
- 加表
- 加索引
- 数据回填
- 修改约束
- 删字段 / 删表

### 3.3 最简单的判断规则

#### 情况 A：没有新增 migration

通常可以视为：

- **无 DB 改动**

#### 情况 B：有新增 migration，但属于兼容型

常见例子：

- `ADD COLUMN`，而且旧代码不会因为这个新字段出问题
- `CREATE TABLE`
- `CREATE INDEX`
- 增加可空字段
- 新增结构，但不破坏旧代码逻辑

通常可以视为：

- **有兼容 DB 改动**

#### 情况 C：有新增 migration，而且属于不兼容型

常见例子：

- `DROP COLUMN`
- `DROP TABLE`
- 修改字段类型，旧代码可能直接不兼容
- 新增严格约束，旧代码写入会失败
- 字段语义完全改变

通常可以视为：

- **有不兼容 DB 改动**

一句话理解：

- **能和旧代码短时间共存** → 兼容型
- **旧代码一跑就可能出问题** → 不兼容型

---

## 4. 三种发布流程

### 4.1 无 DB 改动

适用场景：

- 没有新增 migration
- 或者只是纯代码变化

执行顺序：

1. 本地拉最新代码
2. 本地测试
3. 本地构建镜像
4. 本地打包镜像
5. 先发布到 **单节点 canary**（默认先 `154.12.21.52`）
6. 验证 canary 节点没问题
7. 再发布剩余 2 台应用节点

一句话记住：

> **无 DB 改动：单节点 canary → 全量**

---

### 4.2 有兼容 DB 改动

适用场景：

- 有新的 migration
- 但 migration 属于兼容型
- 新旧代码可以短时间共存

执行顺序：

1. 本地拉最新代码
2. 检查 `backend/migrations/`
3. 确认 migration 属于兼容型
4. 本地测试
5. **先执行 schema 变更**
6. 本地构建镜像
7. 先发布到 **单节点 canary**
8. 验证 canary 节点没问题
9. 再发布剩余 2 台应用节点

为什么要先 schema 后发代码？

因为新代码很可能依赖新字段或新表。\
如果顺序反过来，应用先上，而数据库结构还没变，新代码就可能直接报错。

一句话记住：

> **有兼容 DB 改动：先 schema → 单节点 canary → 全量**

---

### 4.3 有不兼容 DB 改动

适用场景：

- migration 里包含不兼容变更
- 新旧代码不能安全共存

这种情况不要按普通发布来做。

执行原则只有一句：

> **不要直接灰度发布。**

必须改成 **expand / contract** 流程：

1. 先把数据库结构扩展成一个“新旧代码都还能跑”的中间态
2. 先让旧代码和新代码都能兼容这个中间态
3. 再发布新代码
4. 等全量稳定后，再清理旧字段、旧逻辑、旧约束

一句话记住：

> **有不兼容 DB 改动：先做 expand/contract，别直接灰度**

---

## 5. 发布前标准检查单

### 5.1 代码检查

发布前至少确认下面 3 件事：

- 已经拉了最新代码
- 已经检查了 `backend/migrations/`
- 已经判断清楚本次属于哪种发布类型

### 5.2 本地测试

至少执行：

```bash
cd /media/fly/系统2/code/sanyun/temp/sub2api/backend
export GOPROXY=https://goproxy.cn,direct
export GOSUMDB=sum.golang.google.cn
go test ./...
```

如果测试不过：

- **不要继续发布**

不要带着“线上再看看”的心态往前走。

### 5.3 本地构建

```bash
cd /media/fly/系统2/code/sanyun/temp/sub2api
docker build -t sub2api:<new-tag> -f Dockerfile .
docker pull haproxy:3.0-alpine
docker save sub2api:<new-tag> haproxy:3.0-alpine | gzip -1 > /tmp/app-images.tar.gz
```

如果这次还涉及数据层镜像变更，再额外准备对应镜像。

---

## 6. 单节点 canary 发布流程

默认 canary 节点：

- `154.12.21.52`

### 6.1 同步应用文件

```bash
rsync -az deploy/ha/app/ root@154.12.21.52:/opt/sub2api-ha/app/
scp /tmp/sub2api-ha-bundle/app.env root@154.12.21.52:/opt/sub2api-ha/app/.env
scp /tmp/app-images.tar.gz root@154.12.21.52:/opt/sub2api-ha-app-images.tar.gz
```

### 6.2 导入镜像并重启 canary 节点

```bash
ssh root@154.12.21.52 'gunzip -c /opt/sub2api-ha-app-images.tar.gz | docker load'
ssh root@154.12.21.52 'cd /opt/sub2api-ha/app && docker compose down --remove-orphans || true && docker compose up -d'
```

### 6.3 canary 验证

先看容器状态和健康检查：

```bash
ssh root@154.12.21.52 'docker ps --format "table {{.Names}}\t{{.Status}}" | grep sub2api'
ssh root@154.12.21.52 'curl -fsS http://127.0.0.1:8080/health'
```

再做一次管理员登录验证：

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

如果 canary 验证失败：

- **立刻停止继续发另外两台**
- 先定位问题，再决定修复还是回滚

---

## 7. 全量发布流程

当 canary 节点确认没问题后，再发剩余两台：

- `45.192.105.162`
- `156.225.20.29`

流程和 canary 一样，只是换服务器。

全部发布完成后，再统一检查三台：

```bash
for host in 154.12.21.52 45.192.105.162 156.225.20.29; do
  ssh root@$host 'docker ps --format "table {{.Names}}\t{{.Status}}" | grep sub2api && curl -fsS http://127.0.0.1:8080/health'
done
```

---

## 8. 带 DB 改动时的额外注意事项

### 8.1 兼容型 DB 改动

如果判断属于“有兼容 DB 改动”，顺序必须固定为：

1. 先执行数据库 schema 变更
2. 再做单节点 canary
3. 再全量发布应用节点

### 8.2 为什么顺序不能反过来

因为新代码可能依赖新字段或新表。

如果你先发代码、后跑 migration，就可能出现：

- 应用先启动
- 代码一读新字段/新表
- 数据库还没准备好
- 结果直接报错

### 8.3 不兼容 migration 怎么办

如果 migration 包含下面这类内容：

- drop 字段
- drop 表
- 改字段语义
- 加严格约束导致旧代码写不进去

就不要套普通日常发布流程。

必须单独设计：

- expand / contract 步骤
- 中间兼容期
- 回滚方案

---

## 9. 回滚原则

### 9.1 无 DB 改动

最简单，通常直接回滚应用镜像就可以。

### 9.2 有兼容 DB 改动

大多数情况下，也可以先回滚应用代码，因为 schema 仍然兼容旧版本。

### 9.3 有不兼容 DB 改动

这是最危险的情况。

不要默认以为“回滚应用镜像就完事了”。

这类发布在执行前就必须准备好：

- 数据回滚方案
- 旧代码是否还能跑
- 是否需要保留旧字段 / 旧表一段时间

---

## 10. 当前推荐的日常发布习惯

以后每次发布，按下面顺序走最稳：

1. 挑低峰时段
2. 先看 `backend/migrations/`
3. 判断属于哪一种发布类型
4. 本地测试
5. 本地构建镜像
6. 单节点 canary
7. 验证
8. 全量

下面这些步骤不要跳过：

- migration 检查
- canary
- `/health` 验证
- 登录验证

---

## 11. 一句话版本

如果你只想记最核心的 3 句，就记这个：

1. **无 DB 改动：单节点 canary → 全量**
2. **有兼容 DB 改动：先 schema → 单节点 canary → 全量**
3. **有不兼容 DB 改动：先做 expand/contract，别直接灰度**
