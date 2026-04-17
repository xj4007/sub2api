# main 合并到 dev-master 的处理清单（main-first）

> 这份文档的核心目标不是“死保 dev-master 现状”，而是：
>
> **在后续继续合并 `main` 时，始终让业务逻辑以 `main` 为准；如果读写分离逻辑被 merge 覆盖，允许覆盖，但必须先重新按基线文档分析这些接口是否还适合继续做读写分离。**

---

## 1. 核心原则

这份清单先写死最重要的一句话：

> **逻辑以 `main` 分支为准，读写分离是辅助逻辑，不是主业务 source of truth。**

这意味着：

- merge `main` 后，如果某些读写分离实现被覆盖，**这是允许发生的**
- 但不能因为“以前做过 split”，就机械地把旧实现整段抄回来
- 正确做法是：
  1. 先接受 `main` 的业务逻辑
  2. 再根据 `deploy/READ_WRITE_SPLIT_PILOT_CN.md` 和最新线上巡检结果
  3. 重新分析该接口在 merge 后是否仍适合：
     - `reader`
     - `writer`
     - `mixed`
     - 或者暂时取消 split

---

## 2. 正确理解 dev-master

`dev-master` 不是一个长期独立业务分支。

它只承担两类附加职责：

1. **高可用 / 部署辅助能力**
   - writer / reader proxy
   - Redis Sentinel
   - HA 部署素材

2. **少量 repository 级读写分离辅助逻辑**
   - reader / writer 双连接
   - repository 内部 reader 白名单
   - 少数 mixed 路径

换句话说：

> `dev-master` 的存在意义不是偏离 `main`，而是在尽量不改变 `main` 业务语义的前提下，额外挂载读写分离辅助能力。

---

## 3. merge 后哪些情况是允许的

下面这些情况，merge `main` 后发生都是允许的：

### 3.1 某条接口原先是 reader，现在被 main 的新逻辑改回 writer 路径

允许。

这时不要第一反应是“把 reader 改回去”，而应先判断：

- 新业务逻辑是否引入了更强一致性要求
- service / repository 链路是否发生变化
- 这条接口是否仍适合继续 split

### 3.2 某个 repository 的查询结构、preload、排序、字段被 main 改了

允许。

这类变化通常应优先保留 `main`。

只有在重新分析后，确认：

- 新逻辑仍然是只读
- 允许轻微延迟
- 可以继续在 repository 内部低耦合收口

才应重新补回 reader / mixed 逻辑。

### 3.3 `wire_gen.go` / constructor / provider 因 main 更新而变化

允许。

注入链变化后，应该优先先让 `main` 的依赖关系成立，再判断 reader 注入是否仍有必要补回。

---

## 4. merge 后哪些做法是错误的

### 错误 1：为了保住读写分离，整文件保留 dev-master 版本

不允许。

原因：

- 很容易把 `main` 的新业务逻辑、修复、字段、接口调整一起抹掉
- 这类错误比“暂时少一个 reader 路径”更严重

### 错误 2：看到接口以前是 reader，就直接重新打回旧 patch

不允许。

必须先重新分析：

- merge 后接口语义有没有变
- 当前页面是否还真实高频使用
- 这条接口是否仍然适合 reader / mixed

### 错误 3：把“读写分离仍存在”放在“业务逻辑跟 main 对齐”之前

不允许。

优先级必须是：

1. `main` 业务逻辑正确
2. 再考虑是否补回读写分离辅助逻辑

---

## 5. merge 前推荐流程

不要在当前工作区里直接盲合。

推荐：

1. `git fetch origin main`
2. 在隔离 worktree 或临时分支里执行：

```bash
git merge --no-commit --no-ff origin/main
```

3. 先看两类差异：
   - `main` 带来的业务逻辑变化
   - `dev-master` 原有的读写分离辅助逻辑
4. 不要先修 reader；先确认业务逻辑是否以 `main` 为准

---

## 6. merge 后处理顺序（必须按这个顺序）

## 第一步：先确认业务逻辑是否已经正确跟 main 对齐

优先看：

- handler / service / repository 的业务语义
- 新字段 / 新 preload / 新排序 / 新校验
- 新页面相关接口

如果这里还没对齐，就不要急着恢复 reader / mixed。

## 第二步：再拿读写分离基线文档重新分析

重新打开：

- `deploy/READ_WRITE_SPLIT_PILOT_CN.md`
- `deploy/LIVE_MENU_RW_SPLIT_AUDIT_CN.md`

按这两个问题重新判断：

1. 这条接口 merge 后是否仍然适合读写分离？
2. 如果适合，它现在应是：
   - `reader`
   - `writer`
   - `mixed`

## 第三步：只补回仍然成立的 split 逻辑

只恢复那些**重新分析后仍成立**的 reader / mixed 路径。

如果某条接口 merge 后已经不适合继续 split，就允许它继续留在 writer。

---

## 7. merge 后重点重评估的接口

这些接口不是“必须永远保持原状”，而是 merge 后必须重新判断。

## 7.1 优先重评估的接口

### reader 候选 / 已有 reader

- `GET /api/v1/settings/public`
- `GET /api/v1/keys`
- `GET /api/v1/usage/dashboard/stats`
- `GET /api/v1/usage/dashboard/trend`
- `GET /api/v1/usage/dashboard/models`
- `POST /api/v1/usage/dashboard/api-keys-usage`
- `GET /api/v1/subscriptions`
- `GET /api/v1/redeem/history`

### mixed 候选 / 已有 mixed

- `GET /api/v1/usage`
- `GET /api/v1/usage/stats`

### 当前继续偏向 writer / defer

- `GET /api/v1/auth/me`
- `GET /api/v1/subscriptions/active`
- `GET /api/v1/user/totp/status`
- `GET /api/v1/announcements`
- `GET /api/v1/groups/available`
- `GET /api/v1/groups/rates`

---

## 8. merge 后重点关注的文件

## 8.1 基础设施层

- `backend/internal/config/config.go`
- `backend/internal/config/config_test.go`
- `backend/internal/setup/setup.go`
- `backend/internal/repository/ent.go`
- `backend/internal/repository/wire.go`
- `backend/cmd/server/wire.go`
- `backend/cmd/server/wire_gen.go`
- `backend/internal/repository/redis.go`
- `deploy/.env.example`

这些文件的检查重点不是“是否完全保持 dev-master 内容”，而是：

- `main` 的新配置 / 新注入 / 新依赖有没有保留
- reader / Sentinel 能力是否仍能在不破坏 `main` 逻辑的前提下存在

## 8.2 repository 层

- `backend/internal/repository/usage_log_repo.go`
- `backend/internal/repository/user_repo.go`
- `backend/internal/repository/api_key_repo.go`
- `backend/internal/repository/user_subscription_repo.go`
- `backend/internal/repository/setting_repo.go`
- `backend/internal/repository/redeem_code_repo.go`

这里的处理原则是：

> **保留 main 的业务语义，只在重新分析仍成立时，补回 repository-local 的 reader / mixed 路由。**

---

## 9. 需要特别记住的两条 mixed 规则

## 9.1 `/api/v1/usage`

merge 后如果仍适合继续 split，正确规则是：

- `api_key_id` 归属校验 → writer
- 列表 / count / hydration → reader

## 9.2 `/api/v1/usage/stats`

merge 后如果仍适合继续 split，正确规则是：

- `api_key_id` 归属校验 → writer
- 聚合统计查询 → reader

如果 merge 后这两条接口的语义已发生明显变化，则允许暂时不恢复 mixed，先留 writer，再重新评估。

---

## 10. merge 后验证口径

至少要做三层验证：

### 10.1 构建验证

- 镜像能构建成功
- 注入链不报错

### 10.2 运行验证

- `/health` 正常
- 登录正常
- 版本正确

### 10.3 接口验证

至少抽查：

- `GET /api/v1/settings/public`
- `GET /api/v1/keys`
- `GET /api/v1/usage`
- `GET /api/v1/usage/stats`
- `GET /api/v1/usage/dashboard/stats`
- `GET /api/v1/usage/dashboard/trend`
- `GET /api/v1/usage/dashboard/models`
- `POST /api/v1/usage/dashboard/api-keys-usage`

重点不只是看 `200`，还要看：

- 当前页面真实是否仍会触发它
- 它在 merge 后是否还属于适合 split 的类型

---

## 11. 一句话执行口径

如果后续再合并 `main`，请始终按下面这句话执行：

> **先让代码逻辑正确回到 `main`，再根据 `READ_WRITE_SPLIT_PILOT_CN.md` 与 `LIVE_MENU_RW_SPLIT_AUDIT_CN.md` 重新分析接口是否仍适合做读写分离；如果适合，再补回 reader / writer / mixed 辅助逻辑，不适合就允许继续留在 writer。**
