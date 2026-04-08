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

reader 入口新增一组 backend，候选节点还是：

- `10.77.0.1`
- `10.77.0.2`
- `10.77.0.3`

但检查方式改成：

- `/replica`

如果可以，建议再加 lag 门槛，变成类似：

- `/replica?lag=<阈值>`

这样可以避免：

- 虽然还是 replica
- 但复制延迟已经很高
- 仍然被拿来承担读流量

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

- 只有一个 `frontend postgres_frontend`
- 只有一个 `backend postgres_backend`
- 只用 `/leader` 选当前主库

这次要补的块：

#### A. writer 保持现状

- 现有 writer frontend / backend 保留
- 继续通过 `/leader` 识别当前主库

#### B. 新增 reader frontend

建议新增一个 reader 入口端口，例如：

- `5433`

用途：

- app 的 reader 连接只连这个端口

#### C. 新增 reader backend

reader backend 的候选节点仍然是：

- `10.77.0.1`
- `10.77.0.2`
- `10.77.0.3`

但检查逻辑改成：

- `/replica`

如果 Patroni 支持并且实现方便，进一步建议：

- `/replica?lag=<阈值>`

这样做的目的：

- primary 不进入 reader backend
- lag 太高的 replica 也不进入 reader backend

#### D. reader backend 的选择策略

第一版建议不要做复杂负载均衡，先用：

- 一个优先副本
- 一个备用副本

理由：

- 行为更稳定
- 排障更简单
- 更适合你当前这套小规模自建部署

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

2. **db2 目前不是健康可用的 reader 副本**
   - 它在试点期间暴露了复制异常
   - 当前处于 Patroni 管理下的 `reinit / creating replica` 重建状态
   - 在它恢复成稳定 `streaming` 之前，不应把 reader 依赖在它身上

### 20.6 当前一句话状态

如果只记一句话，可以记这个：

> **读写分离试点已经成功上线，但当前是“统计 / 趋势先读副本，明细列表继续走主库，db2 仍在重建中”的保守稳定状态。**
