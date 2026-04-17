# 线上菜单巡检后的读写分离候选接口清单

> 基于 **http://154.12.21.52:8080** 的真实页面巡检结果整理。
> 目标不是记录所有请求，而是给后续继续推进读写分离时，提供一份可直接执行的接口候选清单。

---

## 1. 巡检范围与方法

本次巡检基于真实线上页面操作完成：

- 登录
- Dashboard
- API Keys
- Usage
- My Subscriptions
- Redeem
- Profile

采集口径：

- 只看页面真实触发的 `/api/v1/*` 请求
- 再映射到当前代码的 handler / service / repository 链路
- 最后判断它更适合：
  - `reader`
  - `writer`
  - `mixed`
  - `defer`（当前不建议推进）

---

## 2. 本次真实观察到的接口

按菜单实际触发，主要看到这些接口：

- `GET /api/v1/settings/public`
- `GET /api/v1/auth/me`
- `GET /api/v1/subscriptions/active`
- `GET /api/v1/announcements`
- `GET /api/v1/usage/dashboard/trend`
- `GET /api/v1/usage/dashboard/models`
- `GET /api/v1/usage/dashboard/stats`
- `GET /api/v1/usage`
- `GET /api/v1/keys`
- `GET /api/v1/groups/available`
- `GET /api/v1/groups/rates`
- `POST /api/v1/usage/dashboard/api-keys-usage`
- `GET /api/v1/usage/stats`
- `GET /api/v1/subscriptions`
- `GET /api/v1/redeem/history`
- `GET /api/v1/user/totp/status`

---

## 3. 后续可继续推进的接口清单

这一节只列“后续可以按这份清单继续处理”的接口。

## 3.1 已经适合纳入 reader / mixed 基线的接口

这些接口已经满足下面至少一条：

- 当前实现已经是 reader 或 mixed
- 或者从真实页面用途看，主要是只读列表 / 聚合 / 历史记录，允许轻微延迟

| 接口 | 建议状态 | 说明 | 关键文件 |
|---|---|---|---|
| `GET /api/v1/settings/public` | **reader** | 启动级公共配置，读多写少，页面高频触发 | `backend/internal/handler/setting_handler.go`, `backend/internal/service/setting_service.go`, `backend/internal/repository/setting_repo.go` |
| `GET /api/v1/keys` | **reader** | API Key 列表页主查询，适合继续走 reader | `backend/internal/handler/api_key_handler.go`, `backend/internal/service/api_key_service.go`, `backend/internal/repository/api_key_repo.go` |
| `GET /api/v1/usage` | **mixed** | `api_key_id` 归属校验留 writer；列表 / count / hydration 走 reader | `backend/internal/handler/usage_handler.go`, `backend/internal/service/usage_service.go`, `backend/internal/repository/usage_log_repo.go` |
| `GET /api/v1/usage/dashboard/stats` | **reader** | 仪表盘聚合统计，允许轻微延迟 | `backend/internal/handler/usage_handler.go`, `backend/internal/service/usage_service.go`, `backend/internal/repository/usage_log_repo.go` |
| `GET /api/v1/usage/dashboard/trend` | **reader** | 历史趋势图主数据源，典型 reader 场景 | 同上 |
| `GET /api/v1/usage/dashboard/models` | **reader** | 模型统计聚合，典型 reader 场景 | 同上 |
| `POST /api/v1/usage/dashboard/api-keys-usage` | **reader** | 虽然是 POST，但语义是批量读取统计，不是写入 | 同上 |
| `GET /api/v1/subscriptions` | **reader** | 订阅列表页主体查询，允许轻微延迟 | `backend/internal/handler/subscription_handler.go`, `backend/internal/service/subscription_service.go`, `backend/internal/repository/user_subscription_repo.go` |
| `GET /api/v1/redeem/history` | **reader** | 兑换历史记录，天然适合 reader | `backend/internal/handler/redeem_handler.go`, `backend/internal/service/redeem_service.go`, `backend/internal/repository/redeem_code_repo.go` |

## 3.2 本次巡检后新增、建议纳入下一批处理的接口

下面这条在旧文档里没有写清楚，但从本次真实 Usage 页面请求来看，建议纳入下一批：

| 接口 | 建议状态 | 建议处理方式 | 关键文件 |
|---|---|---|---|
| `GET /api/v1/usage/stats` | **mixed** | 当请求带 `api_key_id` 时，API Key 归属校验继续走 writer；真正的聚合统计查询改走 reader | `backend/internal/handler/usage_handler.go`, `backend/internal/service/usage_service.go`, `backend/internal/repository/usage_log_repo.go` |

### 为什么 `GET /api/v1/usage/stats` 适合进入下一批

真实页面上，Usage 菜单会同时触发：

- `GET /api/v1/usage`
- `GET /api/v1/usage/stats`

其中：

- `/usage` 已经采用 mixed 思路
- `/usage/stats` 的真实语义也是“统计聚合读取”

因此最合理的处理方式不是把它长期留在 writer，而是与 `/usage` 保持一致：

> **前置强一致校验走 writer，聚合查询走 reader。**

### `/api/v1/usage/stats` 后续处理规则

- handler 层如果带 `api_key_id`：
  - 继续保留 `apiKeyService.GetByID(...)` 的 writer 校验
- repository 层统计查询：
  - `GetUserStatsAggregated(...)` 改走 `readSQL()`
  - `GetAPIKeyStatsAggregated(...)` 改走 `readSQL()`

---

## 4. 当前不建议纳入下一批的接口

这些接口虽然在真实页面中高频出现，但当前不建议优先推进。

| 接口 | 当前建议 | 原因 | 涉及文件 |
|---|---|---|---|
| `GET /api/v1/auth/me` | **writer** | 身份 / 会话 / 当前用户状态，强一致优先 | `backend/internal/handler/auth_handler.go`, `backend/internal/service/user_service.go`, `backend/internal/repository/user_repo.go` |
| `GET /api/v1/subscriptions/active` | **writer** | 头部权益状态，用户刚购买/刚过期后体感强 | `backend/internal/handler/subscription_handler.go`, `backend/internal/service/subscription_service.go`, `backend/internal/repository/user_subscription_repo.go` |
| `GET /api/v1/user/totp/status` | **writer** | 安全状态接口，不适合优先下沉 | `backend/internal/handler/totp_handler.go`, `backend/internal/service/totp_service.go`, `backend/internal/repository/user_repo.go` |
| `GET /api/v1/announcements` | **defer** | 多 repo 聚合用户可见性与已读状态，不适合先做 | announcement 相关 handler/service/repository |
| `GET /api/v1/groups/available` | **defer** | 会组合用户 / 分组 / 订阅 / 权益，耦合偏高 | `backend/internal/handler/api_key_handler.go`, `backend/internal/service/api_key_service.go` |
| `GET /api/v1/groups/rates` | **writer / defer** | 当前费率视图直接影响 key 配置与即时展示，先不扩 | `backend/internal/handler/api_key_handler.go`, `backend/internal/service/api_key_service.go` |

---

## 5. 建议作为后续推进基线的接口列表

如果后续只想维护一份“继续处理哪些接口”的短名单，建议直接用下面这份：

### 5.1 reader

- `GET /api/v1/settings/public`
- `GET /api/v1/keys`
- `GET /api/v1/usage/dashboard/stats`
- `GET /api/v1/usage/dashboard/trend`
- `GET /api/v1/usage/dashboard/models`
- `POST /api/v1/usage/dashboard/api-keys-usage`
- `GET /api/v1/subscriptions`
- `GET /api/v1/redeem/history`

### 5.2 mixed

- `GET /api/v1/usage`
- `GET /api/v1/usage/stats`

### 5.3 继续留 writer / defer

- `GET /api/v1/auth/me`
- `GET /api/v1/subscriptions/active`
- `GET /api/v1/user/totp/status`
- `GET /api/v1/announcements`
- `GET /api/v1/groups/available`
- `GET /api/v1/groups/rates`

---

## 6. 下一步推荐顺序

### 第一步

先把：

- `GET /api/v1/usage/stats`

补成和 `/api/v1/usage` 一样的 mixed 规则。

### 第二步

保持已经完成的 reader / mixed 接口不被后续 merge main 回退：

- `settings/public`
- `keys`
- `usage`
- `usage/stats`
- `usage/dashboard/*`
- `subscriptions`
- `redeem/history`

### 第三步

`announcements` / `groups/available` / `groups/rates` 暂时只记录，不立即推进。

---

## 7. 一句话结论

本次真实菜单巡检后的最稳结论是：

> **后续继续推进读写分离时，应优先围绕 settings / keys / usage / usage-stats / usage-dashboard / subscriptions / redeem-history 这批真实高频只读接口展开；其中 `/api/v1/usage` 和 `/api/v1/usage/stats` 都应采用 mixed 方案，而 `auth/me`、`subscriptions/active`、`totp/status`、`announcements`、`groups/available`、`groups/rates` 当前不建议优先推进。**
