# Sub2API 读写分离试点方案

> 这份文档只讨论 **PostgreSQL 读写分离试点**。
>
> 它不是日常发布手册，也不是整体部署总手册，而是一个专门的专题方案文档，用来说明：
>
> - 为什么要做读写分离
> - 这次试点准备怎么做
> - 哪些地方会改
> - 哪些地方明确不动
> - 如何验证效果
> - 如果试点效果不好，怎么快速回滚

---

## 1. 这次试点的目标

当前 PostgreSQL 已经是 HA 集群，但应用现在仍然是：

- 所有查询默认都走主库
- 读和写没有分开

这样做的好处是简单，但缺点也很明显：

- 仪表盘、趋势图、使用记录、统计聚合这类读查询
- 也会持续占用主库资源
- 并发高的时候，主库压力会越来越大

所以这次试点的目标不是“一步做到全站读写分离”，而是：

> **先把最适合的只读场景分出去，让主库先减压，同时保证改动尽量少、风险尽量低。**

### 当前实际落地结果（重要）

这份文档最初描述的是“计划中的试点范围”，但现在已经完成了一轮真实环境上线验证，所以这里先说明当前实际结果：

- **仪表盘趋势 / 聚合统计**：已经走 reader 路径
- **使用记录统计接口**：已经走 reader 路径
- **使用记录明细列表**：暂时保留在 writer
- **writer / reader 双代理**：已经上线
- **三台应用节点**：已经完成试点发布并验证 `/health`

关于数据库副本状态，还需要额外记住一条：

- `db3`：当前健康，可作为 reader 路径依赖的副本
- `db2`：在试点期间一度暴露出复制异常，进入过 Patroni 管理下的 **reinit / creating replica** 流程，但后来已经恢复成 `streaming`

原因不是设计方向错了，而是实际验证时发现：

- 明细列表这条完整查询在当前副本上会出现长时间卡住
- 但统计 / 趋势类查询在 reader 上工作正常

所以当前真实状态应该理解为：

> **读写分离试点已经落地，但范围比最初设计更保守：先优先承接统计 / 趋势，明细列表暂不下沉到 reader。**

另外还要特别记住这次试点期间曾出现过的数据库副本状态：

- `db3`：始终保持健康，可用于 reader 路径
- `db2`：曾在试点上线期间暴露出复制异常，后续进入 Patroni 管理下的 **reinit / creating replica** 流程，但现在已经恢复成 `streaming`

所以这次试点里得到的经验应该理解为：

> **reader 流量应该建立在“健康副本优先”之上，而不是默认所有副本都会一直可靠。**

---

## 2. 当前部署基础（与本方案直接相关）

当前已经具备的条件：

- PostgreSQL HA：Patroni + etcd
- 数据库节点之间走 WireGuard 内网
- 应用节点本地已经有一个 `pgproxy`（HAProxy）
- 现在的写路径已经稳定：

```text
sub2api -> 本机 pgproxy -> 当前 Patroni 主库
```

这意味着：

- **写路径不需要重做**
- 这次只需要在现有结构上，补一条读路径

---

## 3. 这次试点的整体思路

这次采用的方案是：

## 方案：Repository 层双连接 + 本机 reader proxy

简单来说，就是把数据库连接分成两条：

### 写路径

- 保持现状不变
- 继续走：

```text
sub2api -> writer proxy -> 当前主库
```

### 读路径

- 新增一条 reader 路径
- 未来会走：

```text
sub2api -> reader proxy -> 当前健康副本
```

也就是说，应用以后只需要知道两个逻辑端点：

- `writer`
- `reader`

而不需要知道：

- db1 是不是主
- db2 还是不是副本
- db3 当前 lag 高不高

这些角色判断，都放在代理层完成。

---

## 4. 为什么选这个方案

这次没有选“代码里写死某一台从库”，也没有选“自动分析 SQL 然后分流”，原因很现实：

### 4.1 不想把数据库拓扑逻辑写进业务代码

如果代码里直接写：

- 读 db2
- db2 不通再试 db3
- db3 升主了又要重新判断

那后面数据库角色一变，业务代码就会越来越难维护。

### 4.2 现在的项目结构很适合把改动压在底层

当前项目数据库入口比较集中：

- `backend/internal/repository/ent.go`
- `backend/internal/repository/wire.go`
- `backend/internal/repository/usage_log_repo.go`

这说明可以把改动集中在：

- 配置层
- 连接初始化层
- repository 层

而不是把“读主库还是读从库”这个概念散落到 handler / service 各处。

### 4.3 方便以后逐步扩展

这次虽然只先做：

- 仪表盘
- 使用记录

但后续如果要扩展到：

- API Key 列表
- 某些后台列表页
- 某些报表页
- 某些历史审计页

就不需要重新设计架构，只要继续把更多“明确只读的方法”纳入 reader 即可。

---

## 5. 第一批试点范围

这次试点只做两个方向：

### 5.1 使用记录

包括：

- 趋势统计
- 聚合统计

补充说明：

- 从设计角度，明细列表原本也属于第一批候选
- 但根据真实上线验证，**当前生产环境先不把“使用记录明细列表”切到 reader**
- 明细列表继续保留在 writer，统计 / 趋势先走 reader

### 5.2 仪表盘

只做**非实时的统计与聚合部分**，例如：

- 趋势图
- 模型统计
- 分组统计
- 排行榜
- breakdown
- 批量统计

---

## 6. 第一批明确不碰的范围

为了把风险压到最低，下面这些先全部留在 writer：

### 6.1 事务相关

- 所有事务
- 事务里的读
- 写后立刻读

### 6.2 强一致路径

- 登录 / 鉴权
- API Key 认证查询
- 用户额度
- 订阅状态
- 计费判断
- admin 修改后立刻回读

### 6.3 仪表盘中的高风险链路

第一批不建议直接切：

- `DashboardService.GetDashboardStats`

原因不是它不能读从库，而是这条链路里不只是纯读，还带：

- 缓存
- freshness 逻辑
- 聚合 fallback

它比趋势图、历史查询这类链路复杂得多，不适合作为第一批试点。

---

## 7. reader proxy 在部署层怎么加

当前每台应用机已经有一个本地 PostgreSQL 代理：

- `pgproxy`
- 现在只负责 writer 路径

### 7.1 建议做法

建议在现有 HAProxy 结构上扩成：

- `writer` 入口
- `reader` 入口

### 7.2 writer 入口

writer 入口继续沿用当前逻辑：

- 检查 Patroni `/leader`
- 只把当前主库放进 writer backend

这条路径已经验证过，无需推翻重来。

### 7.3 reader 入口

reader 入口当前已经使用 Patroni 的 replica 健康检查。

当前真实做法是：

- 通过 `/replica?lag=16777216` 判断副本是否可读
- 只有 Patroni 返回 `200` 的节点，才允许进入 reader backend

这样可以避免：

- 虽然还是 replica
- 但复制延迟已经很高
- 仍然被拿来承担读流量

### 7.3.1 当前已经上线的 reader proxy 形态

当前生产上已经真实存在的是：

- writer 入口：`5432`
- reader 入口：`5433`
- reader 通过 Patroni `/replica?lag=16777216` 选择副本

在第一轮试点落地时，reader backend 采用的是比较保守的方式：

- `db2` 作为优先 reader
- `db3` 作为 backup reader
- `db1` 作为最后兜底 backup

这种方式的优点是：

- 行为简单
- 排障直观
- 在试点初期更容易控制风险

### 7.3.2 已批准的下一步升级方案

基于这轮讨论，目前已经确认下一步 reader proxy 的目标形态应改为：

- **reader 池只包含 `db2` 与 `db3`**
- **`db1` 不再进入 reader backend**
- **`db2` 与 `db3` 都作为 active reader，承担读流量**

也就是说，目标形态不是：

- 一个主用副本 + 一个 backup 副本 + 主库兜底

而是：

> **`db2 + db3` 双副本均衡读，`db1` 不进 reader 池。**

这样设计的原因是：

1. 把读流量真正分散到两个副本，而不是长期压在一个副本上
2. 副本异常时，reader 池会自动摘掉坏副本
3. 不把副本故障时的读流量偷偷回灌到主库
4. 主库职责保持清晰：主库只负责写

### 7.3.3 reader 池的剔除与恢复规则

下一步 reader backend 继续沿用当前已经验证过的健康检查节奏：

- `inter 2s`
- `fall 2`
- `rise 1`
- `on-marked-down shutdown-sessions`

预期行为是：

- 任一副本连续两次检查失败，就从 reader 池摘除
- 任一副本恢复一次成功，就重新加入 reader 池
- 被标记 down 的副本，其现有会话也会被关闭，避免连接长期粘在坏副本上

### 7.3.4 为什么 `db1` 不再做 reader 兜底

这里要特别明确：

- **下一步 reader 池故意不再让 `db1` 作为 fallback**

原因不是主库不能读，而是我们不希望：

- 副本一旦出问题
- 读流量就自动回灌到主库
- 让主库在故障时同时承担写压力与额外读压力

这次试点里更合理的取舍是：

> **两个副本都不合格时，reader 明确失败，也不要默认退回主库。**

这会让副本问题暴露得更清楚，也更利于运维判断。

### 7.5 关于 `DATABASE_READER_TARGET_SESSION_ATTRS`

当前代码底层仍使用 `lib/pq` 驱动。

这次真实上线已经验证到：

- 如果给 reader DSN 显式传 `target_session_attrs=read-only`
- 在当前驱动与部署方式下，会导致连接异常

所以当前实际部署策略是：

- **reader 的角色判断交给本机 reader proxy**
- `DATABASE_READER_TARGET_SESSION_ATTRS` **保持为空**

也就是说，现在 reader 不是靠数据库驱动自己判断“这是从库”，而是靠：

> **reader proxy 只把连接转发到健康副本。**

### 7.4 为什么建议 reader proxy 而不是 app 自己选从库

因为这样更稳：

- 数据库角色变化时，app 无感知
- 如果某个副本挂了，reader proxy 可以切到另一个副本
- 不用在业务代码里维护副本列表和健康逻辑

---

## 8. 哪些文件需要修改

### 8.1 部署层

这些文件会涉及 reader proxy：

- `deploy/ha/app/docker-compose.yml`
- `deploy/ha/app/haproxy.cfg`
- `deploy/HA_DEPLOYMENT_CN.md`

作用分别是：

- 应用节点增加 reader 入口
- HAProxy 增加 reader backend
- 文档记录这次试点结构

### 8.2 配置层

这些文件会涉及双连接配置：

- `backend/internal/config/config.go`
- `backend/internal/setup/setup.go`

说明：

- setup 仍然只走 writer
- reader 只是新增能力，不应该参与初始化流程

### 8.3 连接初始化 / 注入层

- `backend/internal/repository/ent.go`
- `backend/internal/repository/wire.go`
- `backend/cmd/server/wire.go`
- `backend/cmd/server/wire_gen.go`

这部分是为了把单连接变成双连接：

- writer client / writer DB
- reader client / reader DB

### 8.4 核心 repository 文件

这次最核心的文件是：

- `backend/internal/repository/usage_log_repo.go`

因为：

- 使用记录的大部分读查询都在这里
- 仪表盘很多统计也最终落到这里
- 它是这次试点最集中的改动点

### 8.5 可能会一起涉及的辅助文件

- `backend/internal/repository/dashboard_aggregation_repo.go`

但这不是第一批最高优先级。第一批重点还是 `usage_log_repo.go`。

---

## 9. 第一批建议进入 reader 的 repository 方法

这部分是本次试点的核心白名单。

### 9.1 使用记录列表 / 历史查询

- `ListWithFilters`（当前真实环境暂未切到 reader）
- `ListByUser`（当前真实环境暂未切到 reader）
- `ListByAPIKey`（当前真实环境暂未切到 reader）
- `ListByAccount`（当前真实环境暂未切到 reader）
- `ListByUserAndTimeRange`（当前真实环境暂未切到 reader）

### 9.2 使用趋势 / 聚合 / 统计

- `GetUsageTrendWithFilters`
- `GetModelStatsWithFilters`
- `GetModelStatsWithFiltersBySource`
- `GetGroupStatsWithFilters`
- `GetStatsWithFilters`
- `GetEndpointStatsWithFilters`
- `GetUpstreamEndpointStatsWithFilters`
- `GetUserUsageTrendByUserID`

### 9.3 仪表盘里的非实时统计

- `GetAPIKeyUsageTrend`
- `GetUserUsageTrend`
- `GetUserSpendingRanking`
- `GetBatchUserUsageStats`
- `GetBatchAPIKeyUsageStats`
- `GetUserBreakdownStats`

这些方法的共同特点是：

- 都是明确的只读查询
- 多数是历史数据、聚合、趋势、分页
- 对“轻微延迟”容忍度相对高
- 大部分集中在 `usage_log_repo.go`

---

## 10. 第一批试点接口与代码路径映射

这一节把“页面 / 接口”具体落到代码路径，后续实施时可以直接按这个清单找位置，不需要再重新翻一遍项目。

### 10.1 管理后台：使用记录列表

接口：

- `GET /api/v1/admin/usage`

代码链路：

- Handler：`backend/internal/handler/admin/usage_handler.go`
  - `UsageHandler.List`
- Service：`backend/internal/service/usage_service.go`
  - `UsageService.ListWithFilters`
- Repository：`backend/internal/repository/usage_log_repo.go`
  - `ListWithFilters`
  - 内部会继续走分页查询与关联数据加载

这条接口从设计角度原本很适合先试点，但当前真实环境里已经验证到：

- 这条完整明细查询在 replica 上会出现长时间卡住

所以当前落地策略已经调整为：

- **这条接口继续保留在 writer**
- 后续等副本上的明细查询行为进一步摸清后，再决定是否单独下沉

---

### 10.2 管理后台：使用记录统计

接口：

- `GET /api/v1/admin/usage/stats`

代码链路：

- Handler：`backend/internal/handler/admin/usage_handler.go`
  - `UsageHandler.Stats`
- Service：`backend/internal/service/usage_service.go`
  - `UsageService.GetStatsWithFilters`
- Repository：`backend/internal/repository/usage_log_repo.go`
  - `GetStatsWithFilters`
  - 以及它内部依赖的统计方法：
    - `GetEndpointStatsWithFilters`
    - `GetUpstreamEndpointStatsWithFilters`

这条链路本质是统计型读查询，也很适合作为第一批 reader 试点。

---

### 10.3 管理后台：仪表盘趋势图

接口：

- `GET /api/v1/admin/dashboard/trend`

代码链路：

- Handler：`backend/internal/handler/admin/dashboard_handler.go`
  - `DashboardHandler.GetUsageTrend`
- Service：`backend/internal/service/dashboard_service.go`
  - `DashboardService.GetUsageTrendWithFilters`
- Repository：`backend/internal/repository/usage_log_repo.go`
  - `GetUsageTrendWithFilters`

这是第一批推荐进入 reader 的典型代表。

---

### 10.4 管理后台：仪表盘模型统计

接口：

- `GET /api/v1/admin/dashboard/models`

代码链路：

- Handler：`backend/internal/handler/admin/dashboard_handler.go`
  - `DashboardHandler.GetModelStats`
- Service：`backend/internal/service/dashboard_service.go`
  - `DashboardService.GetModelStatsWithFilters`
  - `DashboardService.GetModelStatsWithFiltersBySource`
- Repository：`backend/internal/repository/usage_log_repo.go`
  - `GetModelStatsWithFilters`
  - `GetModelStatsWithFiltersBySource`

---

### 10.5 管理后台：仪表盘分组统计

接口：

- `GET /api/v1/admin/dashboard/groups`

代码链路：

- Handler：`backend/internal/handler/admin/dashboard_handler.go`
  - `DashboardHandler.GetGroupStats`
- Service：`backend/internal/service/dashboard_service.go`
  - `DashboardService.GetGroupStatsWithFilters`
- Repository：`backend/internal/repository/usage_log_repo.go`
  - `GetGroupStatsWithFilters`

---

### 10.6 管理后台：仪表盘用户 / API Key 批量统计

接口：

- `POST /api/v1/admin/dashboard/users-usage`
- `POST /api/v1/admin/dashboard/api-keys-usage`

代码链路：

- Handler：`backend/internal/handler/admin/dashboard_handler.go`
  - `DashboardHandler.GetBatchUserUsageStats`
  - `DashboardHandler.GetBatchAPIKeyUsageStats`
- Service：`backend/internal/service/dashboard_service.go`
  - `DashboardService.GetBatchUserUsageStats`
  - `DashboardService.GetBatchAPIKeyUsageStats`
- Repository：`backend/internal/repository/usage_log_repo.go`
  - `GetBatchUserUsageStats`
  - `GetBatchAPIKeyUsageStats`

---

### 10.7 管理后台：仪表盘用户排行 / breakdown

接口：

- `GET /api/v1/admin/dashboard/users-ranking`
- `GET /api/v1/admin/dashboard/user-breakdown`

代码链路：

- Handler：`backend/internal/handler/admin/dashboard_handler.go`
  - `DashboardHandler.GetUserSpendingRanking`
  - `DashboardHandler.GetUserBreakdownStats`
- Service：`backend/internal/service/dashboard_service.go`
  - `DashboardService.GetUserSpendingRanking`
  - `DashboardService.GetUserBreakdownStats`
- Repository：`backend/internal/repository/usage_log_repo.go`
  - `GetUserSpendingRanking`
  - `GetUserBreakdownStats`

---

### 10.8 用户侧：个人仪表盘统计

接口：

- `GET /api/v1/usage/dashboard/stats`

代码链路：

- Handler：`backend/internal/handler/usage_handler.go`
  - `UsageHandler.DashboardStats`
- Service：`backend/internal/service/usage_service.go`
  - `UsageService.GetUserDashboardStats`
- Repository：`backend/internal/repository/usage_log_repo.go`
  - `GetUserDashboardStats`

这个接口虽然属于“仪表盘”，但它是用户个人统计视图，比后台总览更适合先做试点。

---

### 10.9 用户侧：个人仪表盘趋势

接口：

- `GET /api/v1/usage/dashboard/trend`

代码链路：

- Handler：`backend/internal/handler/usage_handler.go`
  - `UsageHandler.DashboardTrend`
- Service：`backend/internal/service/usage_service.go`
  - `UsageService.GetUserUsageTrendByUserID`
- Repository：`backend/internal/repository/usage_log_repo.go`
  - `GetUserUsageTrendByUserID`

---

### 10.10 用户侧：个人模型统计

接口：

- `GET /api/v1/usage/dashboard/models`

代码链路：

- Handler：`backend/internal/handler/usage_handler.go`
  - `UsageHandler.DashboardModels`
- Service：`backend/internal/service/usage_service.go`
  - `UsageService.GetUserModelStats`
- Repository：`backend/internal/repository/usage_log_repo.go`
  - `GetUserModelStats`

---

### 10.11 第一批不纳入 reader 的接口（明确列出）

这部分也要写清楚，避免实施时误伤：

- `GET /api/v1/admin/dashboard/stats`
  - Handler：`DashboardHandler.GetStats`
  - Service：`DashboardService.GetDashboardStats`
  - 原因：涉及缓存、freshness、水位与聚合 fallback，第一批先不动

- `GET /api/v1/admin/usage`
  - Handler：`UsageHandler.List`
  - Service：`UsageService.ListWithFilters`
  - 原因：在真实 canary 验证中，这条完整明细列表查询在 replica 上出现长时间卡住，因此当前版本先固定回 writer

- `POST /api/v1/admin/dashboard/aggregation/backfill`
  - Handler：`DashboardHandler.BackfillAggregation`
  - 原因：本质是写入型 / 回填型流程，不属于读副本试点范围

---

### 10.12 用户当前高频接口清单：哪些已经走从库，哪些还没有

这一节专门回答当前这批用户侧高频接口的真实状态。

判断口径统一按下面这条来理解：

- **reader**：代码里已经明确落到 reader-backed repository helper
- **mixed**：同一条接口内部，部分查询走 reader，部分查询仍走 writer
- **writer**：当前代码路径里没有显式 reader helper，仍按默认主库 / 非 reader 路径处理

这里要特别注意一个事实：

> **当前代码里，只有 `backend/internal/repository/usage_log_repo.go` 明确实现了 reader-backed helper（`readSQL()` / `readDB()` / `readClient()`）。**

所以你会看到：

- usage / dashboard 这一支已经有部分接口真正走从库
- 其他像 `auth`、`subscriptions`、`keys`、`groups`、`settings`、`redeem`、`totp` 这些路径，当前都还没有显式接入 reader 白名单

#### 10.12.1 逐条接口状态

| 接口 | 当前状态 | 代码链路 | 说明 |
|---|---|---|---|
| `GET /api/v1/usage/dashboard/trend` | **reader** | `UsageHandler.DashboardTrend` → `UsageService.GetUserUsageTrendByUserID` → `usageLogRepository.GetUserUsageTrendByUserID` | `usage_log_repo.go` 里这条方法显式使用 `readSQL()`，当前已经走从库。 |
| `GET /api/v1/usage/dashboard/models` | **reader** | `UsageHandler.DashboardModels` → `UsageService.GetUserModelStats` → `usageLogRepository.GetUserModelStats` | 这条模型统计查询显式使用 reader-backed 查询路径。 |
| `GET /api/v1/usage` | **writer** | `UsageHandler.List` → `UsageService.ListWithFilters` → `usageLogRepository.ListWithFilters` | 明细分页列表当前仍走 `r.sql` / 默认主路径，真实环境里也已确认先固定保留在 writer。 |
| `GET /api/v1/usage/dashboard/stats` | **reader** | `UsageHandler.DashboardStats` → `UsageService.GetUserDashboardStats` → `usageLogRepository.GetUserDashboardStats` | 这条接口现在已经完整走 `readSQL()`，today stats 也已收口到 reader 主线。 |
| `GET /api/v1/auth/me` | **writer** | `AuthHandler.GetCurrentUser` → `UserService.GetByID` → `userRepository.GetByID` | 当前 `userRepository` 这条路径没有显式 reader helper，仍按默认主路径处理。 |
| `GET /api/v1/subscriptions/active` | **writer** | `SubscriptionHandler.GetActive` → `SubscriptionService.ListActiveUserSubscriptions` → `userSubscriptionRepository.ListActiveByUserID` | 当前订阅读取链路没有显式 reader 接入。 |
| `GET /api/v1/keys` | **reader** | `APIKeyHandler.List` → `APIKeyService.List` → `apiKeyRepository.ListByUserID` | API Key 列表当前已经通过 `ListByUserID` 的 reader 主线接入从库。 |
| `GET /api/v1/groups/available` | **writer** | `APIKeyHandler.GetAvailableGroups` → `APIKeyService.GetAvailableGroups` → `userRepository.GetByID` / `groupRepository.ListActive` / `userSubscriptionRepository.ListActiveByUserID` | 这条链路会组合用户、分组、订阅信息，当前都还没有接入显式 reader 路径。 |
| `GET /api/v1/groups/rates` | **writer** | `APIKeyHandler.GetUserGroupRates` → `APIKeyService.GetUserGroupRates` → `userGroupRateRepository.GetByUserID` | 当前仍走默认主路径。 |
| `GET /api/v1/settings/public` | **reader** | `SettingHandler.GetPublicSettings` → `SettingService.GetPublicSettings` → `settingRepository.GetMultiple` | 这条公共设置读取当前已经通过 `settingRepository.GetMultiple` 的 reader 路径接入从库。 |
| `POST /api/v1/usage/dashboard/api-keys-usage` | **reader** | `UsageHandler.DashboardAPIKeysUsage` → `UsageService.GetBatchAPIKeyUsageStats` → `usageLogRepository.GetBatchAPIKeyUsageStats` | 这条批量统计查询显式使用 `readSQL()`，当前已经走从库。 |
| `GET /api/v1/redeem/history` | **reader** | `RedeemHandler.GetHistory` → `RedeemService.GetUserHistory` → `redeemCodeRepository.ListByUser` | 这条历史记录查询当前已经通过 `ListByUser` 的 reader 路径接入从库。 |
| `GET /api/v1/user/totp/status` | **writer** | `TotpHandler.GetStatus` → `TotpService.GetStatus` → `userRepository.GetByID` | 当前 TOTP 状态读取仍落在默认主路径。 |

#### 10.12.2 当前一句话结论

如果只看你这批接口，当前已经真正切到从库的只有：

- `GET /api/v1/usage/dashboard/trend`
- `GET /api/v1/usage/dashboard/models`
- `GET /api/v1/usage/dashboard/stats`
- `GET /api/v1/settings/public`
- `GET /api/v1/redeem/history`
- `GET /api/v1/keys`
- `POST /api/v1/usage/dashboard/api-keys-usage`

其余你列出来但未进入上面清单的接口，当前都还没有显式改成从库读取，应该继续按 **writer / 非 reader 白名单路径** 来理解。

#### 10.12.3 为什么会是这个结果

原因很简单：

- 这轮真正实现读写分离的重点，集中在 `usage_log_repo.go`
- 那里聚合了大部分 usage / dashboard 的历史统计、趋势、聚合查询
- 其他 repository 目前还没有做同样的 reader helper 改造

所以当前系统的真实状态不是“所有读接口都已经自动走从库”，而是：

> **只有被明确纳入 reader 白名单的 usage / dashboard 查询，才会真正走从库；其余接口仍按默认主路径执行。**

#### 10.12.4 下一批可考虑下沉到 reader 的优先级表

为了避免后续扩展时重新讨论一轮，这里把你列出来的 13 个接口按下一批 reader 下沉优先级整理成 4 档：

- **P0**：已经完成，不需要再讨论是否接入 reader
- **P1**：下一批最值得做，收益直接、风险相对低
- **P2**：可以评估，但不建议优先推进
- **P3**：当前继续固定留在 writer，更稳

| 优先级 | 接口 | 当前状态 | 建议 | 原因 |
|---|---|---|---|---|
| **P0** | `GET /api/v1/usage/dashboard/trend` | reader | 保持现状 | 已在 `usage_log_repo.go` 的 reader 主线上，历史趋势查询天然适合从库。 |
| **P0** | `GET /api/v1/usage/dashboard/models` | reader | 保持现状 | 已显式使用 reader helper，属于典型聚合统计查询。 |
| **P0** | `POST /api/v1/usage/dashboard/api-keys-usage` | reader | 保持现状 | 已显式走 reader，且属于批量聚合统计。 |
| **P0** | `GET /api/v1/usage/dashboard/stats` | reader | 保持现状 | 这条接口已经完成收口，today stats 也已切到 reader 主线。 |
| **P0** | `GET /api/v1/settings/public` | reader | 保持现状 | 已完成 repository-local reader 接入，且不需要 handler/service 识别主从。 |
| **P0** | `GET /api/v1/redeem/history` | reader | 保持现状 | 已完成 repository-local reader 接入，查询语义仍保持在 redeem repository 内部。 |
| **P0** | `GET /api/v1/keys` | reader | 保持现状 | 已完成 repository-local reader 接入，列表路径通过 `ListByUserID` 走 reader。 |
| **P3** | `GET /api/v1/usage` | writer | 继续留 writer | 真实 canary 已经验证过，完整明细分页查询在 replica 上会长时间卡住，当前明确不应再次优先尝试。 |
| **P3** | `GET /api/v1/auth/me` | writer | 继续留 writer | 属于当前用户身份与会话即时状态读取，用户刚登录或状态变化后体感强，不适合优先下沉。 |
| **P3** | `GET /api/v1/subscriptions/active` | writer | 继续留 writer | 订阅状态属于权益即时判断路径，延迟容忍度低。 |
| **P3** | `GET /api/v1/groups/available` | writer | 继续留 writer | 这条接口会组合用户、分组、订阅信息，多仓库组合读取，改动面更大。 |
| **P3** | `GET /api/v1/groups/rates` | writer | 继续留 writer | 直接影响当前用户分组费率视图，和即时策略展示耦合较高。 |
| **P3** | `GET /api/v1/user/totp/status` | writer | 继续留 writer | 这是 2FA / 安全状态查询，用户刚开关 TOTP 后往往会立刻刷新，不适合优先下沉。 |

#### 10.12.5 推荐的下一步顺序

如果按“尽量少改代码 + 尽量少和别人冲突”的原则继续推进，推荐顺序是：

1. 先观察当前已经完成的 7 条 reader 路径是否稳定：
   - `GET /api/v1/usage/dashboard/trend`
   - `GET /api/v1/usage/dashboard/models`
   - `GET /api/v1/usage/dashboard/stats`
   - `POST /api/v1/usage/dashboard/api-keys-usage`
   - `GET /api/v1/settings/public`
   - `GET /api/v1/redeem/history`
   - `GET /api/v1/keys`
2. 再视观察结果，决定是否继续推进更高一致性要求的接口
3. 其余即时状态 / 权益 / 安全相关接口继续保留在 writer

也就是说，下一批最合理的推进方式不是“把剩下所有 GET 都一口气下沉到从库”，而是：

> **先稳定观察当前已经完成的 usage/dashboard reader 白名单，再逐步评估新的 repository 路径。**

---

## 11. 哪些方法明确禁止走 reader

这部分要作为硬规则写死。

### 10.1 所有写方法

任何写操作都不能走 reader。

### 10.2 所有事务

凡是事务相关：

- 开事务
- 事务里的读
- 写后读

全部固定走 writer。

### 10.3 第一批不建议进入 reader 的读方法

第一批先不要动：

- `DashboardService.GetDashboardStats`

理由：

- 不是简单纯读链路
- 还带缓存和 freshness 相关逻辑
- 第一批先不碰，能显著降低试点风险

---

## 12. 如何尽量降低代码耦合

这一点是这次方案里最重要的设计原则之一。

### 11.1 让 repository 决定走 writer 还是 reader

不要把“走主库还是从库”这个概念扩散到：

- handler
- service
- 业务逻辑分支

最理想的做法是：

> **由 repository 方法内部决定是走 writer 还是 reader。**

这样做的好处：

- 改动集中在少数文件
- 后续别人在 service / handler 层改功能时，不容易和这次改动冲突
- 逻辑边界清晰，排查问题也容易

### 11.2 不做“全局自动识别 SELECT 就走从库”

不推荐做下面这种“看起来很聪明”的方案：

- 自动识别 SQL 是不是 SELECT
- 然后统一送到 reader

原因：

- 规则不透明
- 容易误伤写后读
- 很难排查
- 以后别人读代码看不懂为什么这条查询跑到从库去了

### 11.3 用白名单方式逐步扩展

最适合这个项目的方式是：

- 先建立 writer / reader 双连接能力
- 再按 repository 方法做 reader 白名单

以后扩展到其他菜单时，也是：

1. 先判断这个页面是否能容忍轻微延迟
2. 找到它落到哪个 repository 方法
3. 再把该方法纳入 reader 白名单

这样可控、清晰，而且容易回滚。

---

## 13. 上线实施顺序建议

### 第 1 步：先把 reader proxy 搭起来，但先不切业务方法

先完成：

- 部署层 reader endpoint
- 只验证 reader proxy 是否真的只选副本
- 先不让业务查询走 reader

这样先把基础设施验证通过。

### 第 2 步：只先切“使用记录”

原因：

- 更纯
- 改动集中
- 风险更低
- 更容易观察效果

### 第 3 步：再切“仪表盘里的非实时统计”

等使用记录稳定后，再把：

- trend
- ranking
- breakdown
- 聚合图表

逐步切到 reader。

### 第 4 步：观察效果，再决定是否扩展

看：

- 主库压力是否下降
- 副本 lag 是否稳定
- 用户是否感知到明显陈旧

如果效果好，再考虑扩展到别的菜单。

---

## 14. 文件改动级别的实施清单

这一节不再只讲“原则”，而是直接按文件拆分。后面真正实施时，可以直接按这里拆任务。

### 14.1 `deploy/ha/app/docker-compose.yml`

这个文件负责应用节点上的 PostgreSQL 代理容器编排。

当前作用：

- 只有一个本地 `pgproxy`
- 只承接 writer 路径

这次要做的改动：

- 给现有 PostgreSQL 代理增加 reader 入口，或者新增一个单独的 reader proxy 服务
- 保持 `sub2api` 继续依赖本机数据库代理，而不是直接访问数据库节点
- 明确 app 在容器内同时具备：
  - writer 连接目标
  - reader 连接目标

实施目标：

- app 以后不直接感知 db1/db2/db3
- app 只知道本机的 writer / reader 逻辑端点

### 14.2 `deploy/ha/app/haproxy.cfg`

这个文件是读写分离试点里**部署层最关键的文件**。

当前状态：

- 已经同时存在 writer / reader 两个入口
- writer 继续通过 `/leader` 选当前主库
- reader 已经通过 `/replica?lag=16777216` 选副本

如果继续推进第二阶段拓扑升级，这个文件的关注点不再是“从 0 增加 reader”，而是：

- 把已存在的 reader backend 从“保守优先级切换”调整为“双副本均衡读”
- 把 `db1` 从 reader backend 中移除
- 显式固定 reader backend 的均衡策略与剔除语义

这次要调整的重点块：

#### A. writer 保持现状

- 现有 writer frontend / backend 保留
- 继续通过 `/leader` 识别当前主库

#### B. 新增 reader frontend

这一块在第一轮试点中已经完成，当前 reader 入口端口是：

- `5433`

用途：

- app 的 reader 连接只连这个端口

#### C. 新增 reader backend

第一轮试点落地时，reader backend 候选节点曾包含：

- `10.77.0.1`
- `10.77.0.2`
- `10.77.0.3`

但当前已批准的下一步目标应更新为：

- reader backend 只保留 `db2` 与 `db3`
- `db1` 不再进入 reader backend

reader backend 的检查逻辑继续保持：

- `/replica?lag=16777216`

这样做的目的：

- primary 不进入 reader backend
- lag 太高的 replica 也不进入 reader backend

#### D. reader backend 的选择策略

第一轮试点上线时，reader backend 采用过更保守的方式：

- 一个优先副本
- 一个备用副本

但基于这轮真实验证和后续讨论，当前已经批准的下一步策略应更新为：

- `db2` 与 `db3` 同时进入 reader backend
- 显式使用连接级均衡策略（建议 `roundrobin`）
- `db1` 不进入 reader backend

这样做的原因是：

- 当前允许 reader 侧存在 1~5 秒延迟
- 更重要的目标是把读压力分散到两个副本
- 同时继续保留 Patroni lag-aware 健康检查来自动剔除坏副本

需要特别注意：

> **这里的均衡是连接级均衡，不是每条 SQL 的轮询。**

也就是说，最终流量分布还会受应用连接池行为影响，但它仍然会比“长期只压一个副本”更合理。

#### E. 可直接实施的 HAProxy 变更草案

如果进入第二阶段实施，`deploy/ha/app/haproxy.cfg` 的 reader backend 建议直接按下面的目标形态调整。

当前真实配置（简化后）是：

```haproxy
backend postgres_reader_backend
    option httpchk GET /replica?lag=16777216
    http-check expect status 200
    default-server inter 2s fall 2 rise 1 on-marked-down shutdown-sessions
    server db2 10.77.0.2:5432 check port 8008
    server db3 10.77.0.3:5432 check port 8008 backup
    server db1 10.77.0.1:5432 check port 8008 backup
```

目标配置（草案）应改成：

```haproxy
backend postgres_reader_backend
    balance roundrobin
    option httpchk GET /replica?lag=16777216
    http-check expect status 200
    default-server inter 2s fall 2 rise 1 on-marked-down shutdown-sessions
    server db2 10.77.0.2:5432 check port 8008
    server db3 10.77.0.3:5432 check port 8008
```

这份草案里有 3 个关键变化：

1. **显式加入 `balance roundrobin`**
   - 不依赖默认策略
   - 让文档、配置和排障口径完全一致

2. **去掉 `db3` 的 `backup` 语义**
   - 让 `db2` 与 `db3` 都作为 active reader
   - 平时共同承担读连接

3. **把 `db1` 从 reader backend 完全移除**
   - 不再作为 reader fallback
   - 两个副本都不合格时，reader 明确失败

#### F. 这次修改只动哪里

第二阶段拓扑升级应尽量只动这一处：

- `deploy/ha/app/haproxy.cfg`

也就是说，这一轮不应该同时去改：

- writer backend
- `render-app-env.py`
- reader DSN 结构
- repository 白名单逻辑

原因是这次目标非常明确：

> **先验证“reader backend 从优先级切换升级为双副本均衡读”本身稳定，再考虑扩大 reader 接口范围。**

#### G. 建议实施顺序

如果进入实操，建议按这个顺序推进：

1. **先修改 `deploy/ha/app/haproxy.cfg`**
   - 只改 reader backend
   - 不改 writer backend

2. **先在单个 app 节点 canary**
   - 只更新一台应用机的 `pgproxy`
   - 观察 reader 连接是否同时落到 `db2` / `db3`

3. **验证 canary 节点的 reader 行为**
   - 访问当前已经走 reader 的接口
   - 确认没有异常 5xx / timeout / reset 激增
   - 确认 reader backend 没有频繁 flap

4. **确认稳定后再全量到 3 台 app 节点**

#### H. canary 阶段重点验证什么

第二阶段 canary 不需要重新验证全站，只需要盯住和 reader backend 直接相关的现象：

1. `db2` 与 `db3` 是否都收到来自 canary 节点的读连接
2. reader 接口是否仍返回 `200`
3. `db2` 或 `db3` 人为摘除后，reader 是否还能继续工作
4. 恢复副本后，是否能重新加入 reader 池

建议优先验证这些已经在 reader 路径上的接口：

- `GET /api/v1/usage/dashboard/trend`
- `GET /api/v1/usage/dashboard/models`
- `GET /api/v1/usage/dashboard/stats`
- `GET /api/v1/usage/dashboard/api-keys-usage`

#### I. 明确的回滚触发条件

如果出现下面任一情况，这一轮应直接回滚到“优先副本 + backup”的旧 reader 形态：

- `db2` / `db3` 频繁在 up/down 间抖动
- reader 接口明显出现新的超时或错误峰值
- 两个副本中的任一个在承压后 lag 明显放大且持续不恢复
- canary 期间观察到主库异常承担了本不该有的 reader 流量

回滚方式应保持简单：

- 恢复旧版 `postgres_reader_backend`
- 重新加载 app 节点上的 `pgproxy`
- 不需要改业务代码
- 不需要改数据库结构

#### J. 为什么这版草案已经可以直接实施

因为它具备下面这些特征：

- 目标配置已经明确到具体 backend 行
- 变更范围只收敛在 `haproxy.cfg`
- canary 与全量顺序已经明确
- 成功信号与失败信号都已经列出
- 回滚方式不依赖代码回退

所以后续真正进入实施时，不需要再重新讨论“reader 池到底放谁、主库要不要兜底、均衡策略用什么”。

#### K. 逐步执行命令级 runbook（canary / 验证 / 回滚）

这一节把第二阶段拓扑升级直接展开成可执行步骤。

默认前提：

- 本轮只改 `deploy/ha/app/haproxy.cfg`
- 不涉及数据库 migration
- 按 `deploy/RELEASE_RUNBOOK_CN.md` 的“无 DB 改动：单节点 canary → 全量”执行
- 默认 canary 节点仍使用 `154.12.21.52`

##### K.1 本地准备

1. 先确认本地 `deploy/ha/app/haproxy.cfg` 已改成目标 reader backend

目标应类似：

```haproxy
backend postgres_reader_backend
    balance roundrobin
    option httpchk GET /replica?lag=16777216
    http-check expect status 200
    default-server inter 2s fall 2 rise 1 on-marked-down shutdown-sessions
    server db2 10.77.0.2:5432 check port 8008
    server db3 10.77.0.3:5432 check port 8008
```

2. 可在本地先做一次配置自检

```bash
docker run --rm -v "$PWD/deploy/ha/app/haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg:ro" haproxy:3.0-alpine haproxy -c -f /usr/local/etc/haproxy/haproxy.cfg
```

预期：

- 输出 `Configuration file is valid`

##### K.2 下发到 canary 节点

先只同步到 canary 节点 `154.12.21.52`：

```bash
rsync -az deploy/ha/app/ root@154.12.21.52:/opt/sub2api-ha/app/
```

登录 canary 节点：

```bash
ssh root@154.12.21.52
```

在服务器上检查目标文件是否到位：

```bash
cd /opt/sub2api-ha/app
ls -la
sed -n '1,120p' haproxy.cfg
```

##### K.3 只重建 pgproxy，不动应用镜像

由于这轮只改 HAProxy 配置，不需要重新发布 `sub2api` 镜像。

在 canary 节点执行：

```bash
cd /opt/sub2api-ha/app
docker-compose up -d pgproxy
docker ps
docker logs --tail 100 sub2api-pgproxy
```

这里重点确认：

- `sub2api-pgproxy` 已正常重建或重启
- 没有明显的 HAProxy 配置报错

##### K.4 验证 canary 节点本机 reader 端口

在 canary 节点执行：

```bash
curl http://127.0.0.1:8080/health
docker logs --tail 100 sub2api-pgproxy
```

如果需要进一步确认后端状态变化，可临时连续观察：

```bash
docker logs -f sub2api-pgproxy
```

##### K.5 验证两个副本都具备 reader 资格

分别在 `db2` / `db3` 上执行：

```bash
curl http://127.0.0.1:8008/replica?lag=16777216
docker exec sub2api-patroni psql -U sub2api -d postgres -c "select pg_is_in_recovery();"
```

预期：

- `/replica?lag=16777216` 返回 `200`
- `pg_is_in_recovery()` 返回 `t`

##### K.6 验证 canary 节点 reader 接口

在 canary 节点执行，优先验证已经走 reader 的接口：

```bash
curl "http://127.0.0.1:8080/api/v1/usage/dashboard/trend?start_date=2026-04-02&end_date=2026-04-08&granularity=day&timezone=Asia%2FShanghai"
curl "http://127.0.0.1:8080/api/v1/usage/dashboard/models?start_date=2026-04-02&end_date=2026-04-08&timezone=Asia%2FShanghai"
curl "http://127.0.0.1:8080/api/v1/usage/dashboard/stats?timezone=Asia%2FShanghai"
curl -X POST "http://127.0.0.1:8080/api/v1/usage/dashboard/api-keys-usage" -H "Content-Type: application/json" -d '{}'
```

预期：

- 接口返回 `200`
- 没有明显新增 timeout / 5xx

如果这些接口需要登录态，优先沿用你现有 canary 验证方式，在节点上先登录拿 token，再带 token 调用。

##### K.7 验证读连接是否能分散到 `db2` / `db3`

在 `db2` 与 `db3` 上分别执行：

```bash
docker exec sub2api-patroni psql -U sub2api -d postgres -c "select client_addr, state, usename, application_name from pg_stat_activity where client_addr = '10.77.0.4';"
```

说明：

- `10.77.0.4` 是 canary 应用节点 `154.12.21.52` 的 WG IP
- 如果后续换别的 app 节点做 canary，这里把 IP 换成对应 WG IP

预期：

- `db2` 与 `db3` 都能观察到来自 canary 节点的连接
- 不要求绝对 50/50，但不能长期只落到一个副本

##### K.8 人工摘除一个副本，验证自动切换

建议先摘除 `db2` 做验证。

在 `db2` 上执行：

```bash
curl -XPATCH http://127.0.0.1:8008/config -d '{"tags":{"noloadbalance":true}}'
curl http://127.0.0.1:8008/replica?lag=16777216
```

预期：

- 设置成功后，`db2` 不再通过 reader eligibility 检查

然后回到 canary 节点，再次访问 reader 接口：

```bash
curl "http://127.0.0.1:8080/api/v1/usage/dashboard/trend?start_date=2026-04-02&end_date=2026-04-08&granularity=day&timezone=Asia%2FShanghai"
```

再到 `db3` 上看连接是否继续承接：

```bash
docker exec sub2api-patroni psql -U sub2api -d postgres -c "select client_addr, state, usename, application_name from pg_stat_activity where client_addr = '10.77.0.4';"
```

验证完后，把 `db2` 恢复：

```bash
curl -XPATCH http://127.0.0.1:8008/config -d '{"tags":{"noloadbalance":false}}'
curl http://127.0.0.1:8008/replica?lag=16777216
```

恢复后再观察它是否重新加入 reader 池。

##### K.9 canary 通过后的全量步骤

如果 canary 节点观察稳定，再同步到另外两台应用节点：

```bash
rsync -az deploy/ha/app/ root@45.192.105.162:/opt/sub2api-ha/app/
rsync -az deploy/ha/app/ root@156.225.20.29:/opt/sub2api-ha/app/
```

分别在两台机器执行：

```bash
ssh root@45.192.105.162
cd /opt/sub2api-ha/app
docker-compose up -d pgproxy
docker logs --tail 100 sub2api-pgproxy
curl http://127.0.0.1:8080/health
```

```bash
ssh root@156.225.20.29
cd /opt/sub2api-ha/app
docker-compose up -d pgproxy
docker logs --tail 100 sub2api-pgproxy
curl http://127.0.0.1:8080/health
```

##### K.10 回滚步骤（命令级）

如果 canary 或全量后触发回滚条件，直接恢复旧版 reader backend：

旧版目标：

```haproxy
backend postgres_reader_backend
    option httpchk GET /replica?lag=16777216
    http-check expect status 200
    default-server inter 2s fall 2 rise 1 on-marked-down shutdown-sessions
    server db2 10.77.0.2:5432 check port 8008
    server db3 10.77.0.3:5432 check port 8008 backup
    server db1 10.77.0.1:5432 check port 8008 backup
```

本地恢复后，重新同步到目标 app 节点：

```bash
rsync -az deploy/ha/app/ root@154.12.21.52:/opt/sub2api-ha/app/
```

如已全量，则三台都同步：

```bash
rsync -az deploy/ha/app/ root@154.12.21.52:/opt/sub2api-ha/app/
rsync -az deploy/ha/app/ root@45.192.105.162:/opt/sub2api-ha/app/
rsync -az deploy/ha/app/ root@156.225.20.29:/opt/sub2api-ha/app/
```

每台回滚节点执行：

```bash
cd /opt/sub2api-ha/app
docker-compose up -d pgproxy
docker logs --tail 100 sub2api-pgproxy
curl http://127.0.0.1:8080/health
```

##### K.11 回滚后最少验证项

回滚后至少确认：

1. `sub2api-pgproxy` 正常启动
2. `/health` 正常
3. reader 接口恢复正常
4. `db2` 再次成为优先 reader，`db3` 只作为 backup

如果只回滚了 canary 节点，那么验证完成后，该节点即可继续留在旧 reader 拓扑，等待下一轮再试。

### 14.3 `backend/internal/config/config.go`

这个文件负责数据库配置结构和 DSN 生成。

当前状态：

- 只有一套 database 配置
- 最终只会生成一个 DSN

这次要做的改动：

- 保留当前 writer 配置不动
- 新增一套 reader 配置入口
- 让配置层显式区分：
  - writer 连接
  - reader 连接

这里的目标不是做复杂配置，而是提供足够清晰的双连接入口。

### 14.4 `backend/internal/setup/setup.go`

这个文件属于初始化 / AUTO_SETUP 路径。

当前策略：

- setup 只应该走 writer

这次要做的改动：

- 确保 setup 不会误用 reader
- 如果配置结构增加了 reader，也要明确 setup 只消费 writer 这一套

原因：

- 初始化、建表、迁移、校验都必须是强一致路径

### 14.5 `backend/internal/repository/ent.go`

这个文件是连接初始化的核心入口。

当前状态：

- 只初始化一个 `*ent.Client`
- 只初始化一个 `*sql.DB`

这次要做的改动：

- 扩成双连接初始化：
  - writer client / writer DB
  - reader client / reader DB
- migration / bootstrap / secret 初始化继续只使用 writer

这个文件是整个试点最关键的底层改造点之一。

### 14.6 `backend/internal/repository/wire.go`

这个文件负责给依赖注入提供 repository 层依赖。

这次要做的改动：

- 提供 writer / reader 两套 DB 依赖
- 让 usage / dashboard 相关 repository 能拿到双连接

目标：

- 双连接能力只在底层展开
- 不把“主从库选择逻辑”扩散到上层业务代码

### 14.7 `backend/cmd/server/wire.go` 与 `backend/cmd/server/wire_gen.go`

这两个文件负责最终组装应用启动依赖。

这次要做的改动：

- 把原来的单 DB 注入，替换成双 DB 注入
- 让使用记录 / 仪表盘路径能够拿到新的 reader 能力

这部分属于跟随 wiring 变化的必要改动，不是业务逻辑改动。

### 14.8 `backend/internal/repository/usage_log_repo.go`

这是本次试点里**最核心的业务代码文件**。

原因：

- 使用记录查询在这里
- 仪表盘大部分统计查询最终也在这里
- 第一批 reader 白名单的大部分方法都集中在这里

这次要做的改动方向：

- 让这个 repo 同时持有：
  - writer client / writer DB
  - reader client / reader DB
- 让 repo 内部不同方法明确选择走哪一边

换句话说：

> 这次试点最重要的代码改动，大概率就集中在 `usage_log_repo.go`。

### 14.9 `backend/internal/repository/dashboard_aggregation_repo.go`

这个文件不是第一批最高优先级，但需要标记出来。

原因：

- 仪表盘的某些聚合相关能力和 freshness 逻辑会碰到它
- 如果后面想把某些 dashboard 聚合读也继续优化，这里可能要一起考虑

但第一批建议：

- 不把它作为主要改动点
- 先把 `usage_log_repo.go` 跑通

---

## 15. repository 内部建议怎么拆 writer / reader

这一节是实施层最关键的“拆法建议”。

### 15.1 总原则

不要让 service / handler 决定：

- 这次该走主库
- 这次该走从库

而是让 repository 自己决定。

目标是：

> **repository 方法内部显式选择 writer 或 reader。**

### 15.2 `usage_log_repo.go` 的推荐拆法

建议在 repo 内部按职责拆成三类方法：

#### A. 强制 writer 的方法

包括：

- 所有写方法
- 所有事务内方法
- 所有写后立刻读的辅助方法

这类方法统一只用 writer。

#### B. 明确 reader 白名单方法

包括这次第一批试点方法：

- `ListWithFilters`
- `ListByUser`
- `ListByAPIKey`
- `ListByAccount`
- `ListByUserAndTimeRange`
- `GetUsageTrendWithFilters`
- `GetModelStatsWithFilters`
- `GetModelStatsWithFiltersBySource`
- `GetGroupStatsWithFilters`
- `GetStatsWithFilters`
- `GetEndpointStatsWithFilters`
- `GetUpstreamEndpointStatsWithFilters`
- `GetUserUsageTrendByUserID`
- `GetAPIKeyUsageTrend`
- `GetUserUsageTrend`
- `GetUserSpendingRanking`
- `GetBatchUserUsageStats`
- `GetBatchAPIKeyUsageStats`
- `GetUserBreakdownStats`
- `GetUserDashboardStats`
- `GetUserModelStats`

这类方法统一使用 reader。

#### C. 暂不下沉的读方法

例如：

- `GetDashboardStats`

这类方法虽然表面是读，但因为链路复杂，第一批先继续留在 writer。

### 15.3 关联数据加载怎么处理

像 `ListWithFilters` 这种方法，不只是查 usage_logs，还会继续查：

- users
- api keys
- accounts
- groups
- subscriptions

这里建议的原则是：

> **既然入口方法已经决定走 reader，那么这条方法内部的关联加载也一起走 reader。**

不要出现：

- 外层列表查 reader
- 内层 hydration 又跳回 writer

否则会让行为非常混乱，也增加主库压力。

### 15.4 为什么不建议“自动识别 SELECT 就走 reader”

原因有 4 个：

1. 规则不透明
2. 误伤概率高
3. 排查困难
4. 更容易和别人改代码时冲突

白名单式方法级路由，虽然笨一点，但最稳。

---

## 16. 第一批实施任务拆分建议

如果后面真的开始做，建议按下面顺序拆任务：

### 任务 1：补部署层 reader proxy

涉及：

- `deploy/ha/app/docker-compose.yml`
- `deploy/ha/app/haproxy.cfg`

目标：

- writer / reader 双入口都准备好

### 任务 2：补双连接初始化能力

涉及：

- `backend/internal/config/config.go`
- `backend/internal/repository/ent.go`
- `backend/internal/repository/wire.go`
- `backend/cmd/server/wire.go`
- `backend/cmd/server/wire_gen.go`

目标：

- 应用具备 writer / reader 双连接能力

### 任务 3：先切使用记录 reader 白名单

涉及：

- `backend/internal/repository/usage_log_repo.go`

目标：

- 只先让使用记录列表 / 趋势 / 聚合走 reader

### 任务 4：再切仪表盘非实时统计

涉及：

- `backend/internal/repository/usage_log_repo.go`
- 可能少量关联 dashboard service / repo

目标：

- 让趋势 / 模型统计 / 分组统计 / 排行等走 reader

### 任务 5：观察效果，决定是否扩展

重点看：

- 主库是否减压
- 副本 lag 是否稳定
- 是否出现明显陈旧投诉

---

## 17. 上线验证怎么做

### 13.1 先验证 reader proxy 自身

至少确认：

1. reader backend 只接纳 replica
2. primary 不会被 reader backend 接纳
3. 一个副本挂掉时，reader 能切到另一个副本
4. lag 超过阈值时，副本会被摘掉（如果你启用了 lag 条件）

### 13.2 再验证试点接口

重点看：

- 使用记录列表是否正常
- 使用记录趋势是否正常
- 仪表盘趋势 / 聚合是否正常
- 是否出现明显“刚产生的数据看不到”的问题

### 13.3 数据库侧观察项

建议重点观察：

- 主库连接数 / CPU / 查询压力
- 副本 lag
- standby 上是否有长查询冲突
- reader proxy backend 是否频繁 flap

---

## 18. 回滚怎么做

这次试点的一个重要优点是：

> **回滚很简单。**

### 14.1 最快回滚方式

把这批试点方法重新切回 writer。

这样做的好处是：

- 不需要回滚数据库结构
- 不需要回滚 HA
- 不需要回滚 WireGuard
- 不需要大规模回退部署架构

### 14.2 更保守的回滚方式

如果你怀疑 reader proxy 自己都有问题，也可以：

- 保留 reader proxy 配置
- 但应用层停止使用 reader client
- 全部读重新走 writer

---

## 19. 这次方案的一句话总结

这次试点的最终方案可以概括成一句话：

> **在现有 writer proxy 的基础上新增 reader proxy，并在 repository 层引入 writer / reader 双连接，第一批只让“使用记录 + 仪表盘非实时统计”这类明确只读、可接受轻微延迟的方法走 reader，其余强一致路径继续留在 writer。**

这条方案最符合你现在的要求：

- 尽量少改代码
- 尽量低耦合
- 尽量少和别人冲突
- 后续还能继续扩展到更多菜单

---

## 20. 实施完成记录（简版）

这一节不是未来方案，而是这次已经真实完成的落地结果，方便后面快速回看。

### 20.1 已完成的改造

本次已经实际完成：

- writer / reader 双连接能力
- 本机 writer proxy + reader proxy
- reader 入口端口 `5433`
- reader proxy 通过 Patroni `/replica` 选择副本
- 应用节点完成 canary + 全量发布

### 20.2 已经真正生效的 reader 范围

当前已经确认走 reader 的范围：

- 仪表盘趋势 / 聚合统计
- 使用记录统计接口

### 20.3 当前仍保留在 writer 的范围

当前明确保留在 writer 的范围：

- 使用记录明细列表
- 强一致路径
- 事务路径
- 登录 / 鉴权 / 额度 / 订阅 / 计费相关读取
- `DashboardService.GetDashboardStats`

### 20.4 本次上线验证结果

这次真实环境里已经验证通过：

- `go test ./...` 通过
- 三台应用节点 `/health` 正常
- canary 登录返回 `200`
- `GET /api/v1/admin/usage` 返回 `200`
- `GET /api/v1/admin/usage/stats` 返回 `200`
- `GET /api/v1/admin/dashboard/trend?granularity=day` 返回 `200`
- writer proxy 端口 `5432` 指向主库
- reader proxy 端口 `5433` 指向副本

### 20.5 当前的现实限制

虽然试点已经上线成功，但当前还有两个现实限制需要记住：

1. **使用记录明细列表还不适合直接放 reader**
   - 在真实环境里验证时，完整明细查询在副本上会长时间卡住
   - 所以当前先保守地把它留在 writer

2. **reader proxy 的第二阶段拓扑升级还未实施**
   - 当前 reader 路径虽然已经上线，但还保留着第一轮试点时的保守形态
   - 下一步已批准的目标形态是：`db2 + db3` 双副本均衡读，`db1` 不进 reader 池
   - 这一步属于 reader proxy 拓扑优化，不是重新设计代码架构

补充说明：

- `db2` 之前确实暴露过复制异常
- 但经过后续排障与重建后，当前已经恢复成稳定 `streaming`
- 所以现在 `db2` 与 `db3` 都可以作为后续 balanced reader 方案的候选副本

### 20.6 当前一句话状态

如果只记一句话，可以记这个：

> **读写分离试点已经成功上线，当前是“统计 / 趋势先读副本、明细列表继续走主库”的保守稳定状态；下一步已批准把 reader proxy 升级为 `db2 + db3` 双副本均衡读，且 `db1` 不进入 reader 池。**
