# dev-master 读写分离实施基线（Merge Safe）

> 本文档是 **当前已生效实现** 的基线说明，不是讨论稿。
> 后续 `main` 再合并到 `dev-master` 时，**一律以本文档判断哪些逻辑必须保留，哪些逻辑必须继续跟随 `main`**。

---

## 1. 文档目的

`dev-master` 不是一个“自由分叉分支”。

当前它的唯一长期差异目标是：

> **在尽量保持 `main` 业务逻辑不变的前提下，只保留 PostgreSQL / Redis 高可用与读写分离试点相关改动。**

因此后续每次把 `main` 合并到 `dev-master` 时，都必须遵守下面这条总原则：

- **业务逻辑默认跟随 `main`**
- **只有本文档明确列出的 reader / writer / mixed 路径，以及对应基础设施文件，才允许保留 `dev-master` 差异**

如果 merge 后发现某条接口行为与本文档不一致，应优先认为 merge 结果有问题，而不是随意相信自动合并结果。

---

## 2. 总体原则

## 2.1 source of truth

- `main` 是业务逻辑 source of truth
- `dev-master` 只额外承载：
  - writer / reader 双连接能力
  - HA proxy / Sentinel / reader endpoint
  - repository 层 reader 白名单

## 2.2 主从判断的边界

主从判断应尽量留在 **repository 内部**，不要扩散到 handler / service。

允许的形式：

- repository 内部显式 `readSQL()` / `readClient()`
- repository 内部显式 writer client / writer db

不推荐的形式：

- 在 handler 里写“这个请求走 reader”
- 在 service 里写“这个场景走 writer / reader 分支”
- 做“自动识别 SELECT 就走从库”这类黑盒路由

## 2.3 三种状态定义

### reader

整条接口的核心读取路径已经明确收口到 reader。

### writer

接口当前仍应固定走 writer，原因通常是：

- 强一致
- 写后读
- 身份 / 权益 / 安全状态
- 事务 / 初始化 / 配置写入

### mixed

同一条接口内部允许拆分：

- 强实时 / 权限校验 / 写后读仍走 writer
- 主体只读查询、count、hydration 走 reader

---

## 3. 当前已确认的接口分类（以当前代码为准）

下面这张表是**后续 merge 时最重要的判定表**。

## 3.1 用户侧接口决策表

| 接口 | 当前状态 | 处理规则 | 关键代码链路 | Merge 时必须确认 |
|---|---|---|---|---|
| `GET /api/v1/settings/public` | **reader** | 公共设置读取走 reader | `SettingHandler.GetPublicSettings` → `SettingService.GetPublicSettings` → `settingRepository.GetMultiple` | `GetMultiple` 不要被改回 writer 查询 |
| `GET /api/v1/auth/me` | **writer** | 当前用户身份 / 会话状态保持 writer | `AuthHandler.GetCurrentUser` → `UserService.GetByID` → `userRepository.GetByID` | `userRepository.GetByID` 不要因 merge 被误切 reader |
| `GET /api/v1/keys` | **reader** | API Key 列表走 reader | `APIKeyHandler.List` → `APIKeyService.List` → `apiKeyRepository.ListByUserID` | `ListByUserID` 继续 reader，写方法继续 writer |
| `GET /api/v1/usage` | **mixed** | `api_key_id` 归属校验走 writer；列表查询 / count / hydration 走 reader | `UsageHandler.List` → `UsageService.ListWithFilters` → `usageLogRepository.ListWithFilters` | 归属校验不要改；`listUsageLogsWithPagination` / `queryUsageLogs` / hydration 必须保持 reader |
| `GET /api/v1/usage/dashboard/stats` | **reader** | dashboard stats 走 reader | `UsageHandler.DashboardStats` → `UsageService.GetUserDashboardStats` → `usageLogRepository.GetUserDashboardStats` | `GetUserDashboardStats` 保持 `readSQL()` |
| `GET /api/v1/usage/dashboard/trend` | **reader** | dashboard trend 走 reader | `UsageHandler.DashboardTrend` → `UsageService.GetUserUsageTrendByUserID` → `usageLogRepository.GetUserUsageTrendByUserID` | `GetUserUsageTrendByUserID` 保持 `readSQL()` |
| `GET /api/v1/usage/dashboard/models` | **reader** | dashboard model stats 走 reader | `UsageHandler.DashboardModels` → `UsageService.GetUserModelStats` → `usageLogRepository.GetUserModelStats` | `GetUserModelStats` 保持 `readSQL()` |
| `POST /api/v1/usage/dashboard/api-keys-usage` | **reader** | 批量 usage stats 走 reader | `UsageHandler.DashboardAPIKeysUsage` → `UsageService.GetBatchAPIKeyUsageStats` → `usageLogRepository.GetBatchAPIKeyUsageStats` | `GetBatchAPIKeyUsageStats` 保持 `readSQL()` |
| `GET /api/v1/usage/stats` | **mixed** | `api_key_id` 归属校验走 writer；聚合统计查询走 reader | `UsageHandler.Stats` → `UsageService.GetStatsByUser` / `GetStatsByAPIKey` → `usageLogRepository.GetUserStatsAggregated` / `GetAPIKeyStatsAggregated` | 前置归属校验不要改；聚合统计应继续按 mixed 规则处理 |
| `GET /api/v1/subscriptions` | **reader** | 用户订阅列表走 reader | `SubscriptionHandler.List` → `SubscriptionService.ListUserSubscriptions` → `userSubscriptionRepository.ListByUserID` | `ListByUserID` 不要被回退成 writer-only |
| `GET /api/v1/subscriptions/summary` | **reader** | 订阅汇总走 reader | `SubscriptionHandler.GetSummary` → `SubscriptionService.ListActiveUserSubscriptions` → `userSubscriptionRepository.ListActiveByUserID` | `ListActiveByUserID` 保持 reader |
| `GET /api/v1/subscriptions/progress` | **reader** | 订阅进度读取走 reader | `SubscriptionHandler.GetProgress` → `SubscriptionService.GetSubscriptionProgress` → `userSubscriptionRepository.GetByID` | `GetByID` 保持 reader |
| `GET /api/v1/subscriptions/active` | **writer** | 当前权益即时判断继续 writer | `SubscriptionHandler.GetActive` → `SubscriptionService.ListActiveUserSubscriptions` | 不要因为“都是 GET”就误切 reader |
| `GET /api/v1/groups/available` | **writer** | 组合用户 / 订阅 / 分组，当前保守留 writer | `APIKeyHandler.GetAvailableGroups` → 多 repo 聚合 | merge 时继续跟随 `main`，不要私自扩 reader |
| `GET /api/v1/groups/rates` | **writer** | 当前费率展示继续 writer | `APIKeyHandler.GetUserGroupRates` → `userGroupRateRepository.GetByUserID` | 继续保持 writer |
| `GET /api/v1/redeem/history` | **reader** | 用户兑换历史走 reader | `RedeemHandler.GetHistory` → `RedeemService.GetUserHistory` → `redeemCodeRepository.ListByUser` | `ListByUser` 保持 reader |
| `GET /api/v1/user/totp/status` | **writer** | 安全状态读取继续 writer | `TotpHandler.GetStatus` → `TotpService.GetStatus` → `userRepository.GetByID` | 不要因 merge 误切 reader |

---

## 4. `/api/v1/usage` 的特殊规则（必须单独记住）

这是后续最容易在 merge 时被覆盖错的一类接口。

## 4.1 正确规则

`GET /api/v1/usage` 与 `GET /api/v1/usage/stats` 当前都不是纯 reader，也不是纯 writer，而是：

> **mixed**

### writer 部分

如果请求带 `api_key_id`：

- 先在 handler 层做 API Key 归属校验
- 这一步继续走 writer

对应位置：

- `backend/internal/handler/usage_handler.go`
- `UsageHandler.List`
- `UsageHandler.Stats`
- `h.apiKeyService.GetByID(...)`

### reader 部分

`GET /api/v1/usage` 的主体读取部分走 reader：

- `COUNT(*)`
- usage_logs 明细查询
- hydration 关联加载：
  - users
  - api_keys
  - accounts
  - groups
  - subscriptions

对应位置：

- `backend/internal/repository/usage_log_repo.go`

必须保持的实现点：

- `listUsageLogsWithPagination()` → `scanSingleRow(..., r.readSQL(), ...)`
- `queryUsageLogs()` → `r.readSQL().QueryContext(...)`
- `loadUsers()` → `r.readClient().User.Query()`
- `loadAPIKeys()` → `r.readClient().APIKey.Query()`
- `loadAccounts()` → `r.readClient().Account.Query()`
- `loadGroups()` → `r.readClient().Group.Query()`
- `loadSubscriptions()` → `r.readClient().UserSubscription.Query()`

`GET /api/v1/usage/stats` 的聚合统计部分也应走 reader：

- `GetUserStatsAggregated()` → 应使用 `readSQL()`
- `GetAPIKeyStatsAggregated()` → 应使用 `readSQL()`

## 4.2 merge 时禁止出现的回退

后续 merge `main` 时，下面这些回退都属于错误：

- 把 `listUsageLogsWithPagination()` 的 count 改回 `r.sql`
- 把 `queryUsageLogs()` 改回 `r.sql`
- 把 hydration 的关联加载从 `r.readClient()` 改回 `r.client`
- 为了“统一风格”把 handler 里的 `api_key_id` 归属校验也改去 reader
- 把 `GetUserStatsAggregated()` / `GetAPIKeyStatsAggregated()` 长期固定回 writer，但又忘了重新评估这是否仍符合当前页面真实访问场景

---

## 5. 文件归属：哪些文件允许偏离 main

下次 merge 时，不是所有冲突都能“保留 dev-master 一侧”。

只有下面这些文件，允许保留读写分离差异；其余业务文件默认优先跟 `main`。

## 5.1 基础设施 / 配置层（允许偏离）

- `backend/internal/config/config.go`
- `backend/internal/config/config_test.go`
- `backend/internal/setup/setup.go`
- `backend/internal/repository/ent.go`
- `backend/internal/repository/wire.go`
- `backend/cmd/server/wire.go`
- `backend/cmd/server/wire_gen.go`
- `backend/internal/repository/redis.go`
- `deploy/.env.example`

这些文件允许保留的差异类型：

- reader host / port / target_session_attrs
- `ReaderDSN()` / `ReaderDSNWithTimezone()`
- Redis Sentinel 配置
- writer / reader 双连接注入
- `ReaderEntClient` / `ReaderSQLDB` 注入链

## 5.2 repository 白名单层（允许偏离）

- `backend/internal/repository/usage_log_repo.go`
- `backend/internal/repository/user_repo.go`
- `backend/internal/repository/api_key_repo.go`
- `backend/internal/repository/user_subscription_repo.go`
- `backend/internal/repository/setting_repo.go`
- `backend/internal/repository/redeem_code_repo.go`

但要注意：

> 这些文件允许偏离，不等于整文件都保留 `dev-master`。只允许保留 **reader / writer / mixed 决策本身**，业务语义仍要优先跟 `main`。

例如：

- 查询应该走 reader，可以保留
- 但排序、字段、业务校验、返回结构，如果 `main` 有新逻辑，仍应优先跟 `main`

## 5.3 部署 / 运维层（允许偏离）

- `deploy/HA_DEPLOYMENT_CN.md`
- `deploy/RELEASE_RUNBOOK_CN.md`
- `deploy/READ_WRITE_SPLIT_PILOT_CN.md`
- `deploy/MAIN_TO_DEV_MASTER_MERGE_CHECKLIST_CN.md`
- `deploy/ha/app/docker-compose.yml`
- `deploy/ha/app/haproxy.cfg`
- `deploy/ha/scripts/render-app-env.py`
- `deploy/ha/scripts/render-db-env.py`

这些文件承载的是：

- HA / pgproxy / reader proxy
- Sentinel / WireGuard / rollout
- canary / rollback / merge 口径

这些内容不应被 `main` 上旧部署文档回退覆盖。

---

## 6. 文件内的硬规则

## 6.1 `backend/internal/config/config.go`

必须保留：

- `ReaderHost`
- `ReaderPort`
- `ReaderTargetSessionAttrs`
- `TargetSessionAttrs`
- `ReaderDSN()`
- `ReaderDSNWithTimezone()`
- Redis Sentinel 相关配置项

但同时也必须吸收 `main` 新增的其它配置字段。

## 6.2 `backend/internal/repository/ent.go`

必须保留：

- writer / reader 双连接初始化
- `EntBundle`
- `InitEntBundle()`

必须继续保证：

- migration / bootstrap / setup 只走 writer

## 6.3 `backend/internal/repository/wire.go`

必须保留：

- `ReaderEntClient`
- `ReaderSQLDB`
- `ProvideEntBundle`
- `ProvideReaderEnt`
- `ProvideReaderSQLDB`

## 6.4 `backend/internal/setup/setup.go`

必须保留：

- setup 路径只消费 writer DSN
- 但配置结构要兼容 reader / Sentinel 字段

## 6.5 `backend/internal/repository/usage_log_repo.go`

这是读写分离主战场，merge 时必须逐段检查。

当前必须保留的能力：

- `readSQL()`
- `readDB()`
- `readClient()`
- dashboard 统计 reader 化
- `/api/v1/usage` mixed 路径
- `/api/v1/usage/stats` mixed 路径

### 注意

`usage_log_repo.go` 里并不是所有读方法都必须走 reader。

如果后续 `main` 改了某些统计或 admin 查询，不要机械地把整文件所有 SQL 都切到 reader；仍应只保留本文档明确确认过的 reader / mixed 白名单。

---

## 7. merge main 到 dev-master 的操作规则

## 7.1 不允许直接盲合

推荐流程：

1. `git fetch origin main`
2. 在隔离 worktree 或临时分支做预演 merge
3. 先看高风险文件
4. 再做代码验证

## 7.2 merge 后必须逐项确认

### A. 配置层

确认以下 reader / Sentinel 字段仍在：

- `ReaderHost`
- `ReaderPort`
- `ReaderTargetSessionAttrs`
- `TargetSessionAttrs`
- `SentinelEnabled`
- `SentinelMasterName`
- `SentinelAddrs`

### B. 注入层

确认以下类型 / provider 仍在：

- `EntBundle`
- `ReaderEntClient`
- `ReaderSQLDB`
- `ProvideEntBundle`
- `ProvideReaderEnt`
- `ProvideReaderSQLDB`

### C. repository 路由层

至少重新核对以下方法：

- `settingRepository.GetMultiple`
- `apiKeyRepository.ListByUserID`
- `redeemCodeRepository.ListByUser`
- `userSubscriptionRepository.ListByUserID`
- `userSubscriptionRepository.ListActiveByUserID`
- `userSubscriptionRepository.GetByID`
- `usageLogRepository.GetUserDashboardStats`
- `usageLogRepository.GetUserUsageTrendByUserID`
- `usageLogRepository.GetUserModelStats`
- `usageLogRepository.GetBatchAPIKeyUsageStats`
- `usageLogRepository.ListWithFilters`
- `usage_log_repo.go` 里的 `/api/v1/usage` mixed 路径
- `usageLogRepository.GetUserStatsAggregated`
- `usageLogRepository.GetAPIKeyStatsAggregated`
- `usage_log_repo.go` 里的 `/api/v1/usage/stats` mixed 路径

### D. 文档层

确认本文档没有被旧状态覆盖回去。

---

## 8. 验证口径（merge 后 / 发布前）

至少需要确认：

### 代码层

- 镜像可成功构建
- `wire_gen.go` / 注入链不报错

### 运行层

- `/health` 正常
- 版本正确
- 登录正常

### 关键接口层

至少抽查：

- `GET /api/v1/settings/public`
- `GET /api/v1/keys`
- `GET /api/v1/usage`
- `GET /api/v1/usage/stats`
- `GET /api/v1/usage/dashboard/stats`
- `GET /api/v1/usage/dashboard/trend`
- `GET /api/v1/usage/dashboard/models`
- `POST /api/v1/usage/dashboard/api-keys-usage`

其中 `/api/v1/usage` 与 `/api/v1/usage/stats` 都要特别验证：

- 不带 `api_key_id` 正常
- 带 `api_key_id` 时归属校验仍正常

---

## 9. 当前一句话状态

当前 `dev-master` 的正确口径是：

> **业务逻辑以 `main` 为准；`dev-master` 只额外保留 HA / Sentinel / writer-reader 双连接能力，以及少量 repository 级 reader / mixed 白名单。其中 `/api/v1/usage` 与 `/api/v1/usage/stats` 都必须保持“归属校验走 writer，主体读取/聚合统计走 reader”的 mixed 模式。**

这句话就是后续 merge 时的最高优先级判断标准。
