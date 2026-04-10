# main 合并到 dev-master 的兼容性检查清单

## 适用场景

当需要把 `main` 分支合并到当前长期维护的 `dev-master` 分支时，必须先做兼容性分析，再执行 merge。

当前 `dev-master` 已经包含较大范围的 PostgreSQL 读写分离改造、HA 部署文档、reader/writer 双连接注入和多条 repository 级 reader 路径，因此不能把 `main` 直接盲合进来。

---

## 一、先确认分支对象

1. 当前仓库主线是 `main`，不是 `master`
2. 合并前必须先拿到最新远端：

```bash
git fetch origin main
```

3. 真正要比较的是：

- `origin/main`
- `dev-master`

不要只看本地过期的 `main`

---

## 二、建议执行方式

不要直接在当前 `dev-master` 工作区里 merge。

推荐流程：

1. 先从当前 `dev-master` 新建一个隔离 worktree
2. 在隔离 worktree 里执行：

```bash
git merge --no-commit --no-ff origin/main
```

3. 先检查冲突热点和语义兼容性
4. 跑完整验证
5. 验证通过后，再把结果带回 `dev-master`

这样做的原因是：

- 不会污染当前主工作区
- 即使 merge 结果不对，也可以直接丢弃隔离 worktree
- 更适合逐文件兼容处理

---

## 三、高风险热点文件

### 1. Config / Env 契约

- `backend/internal/config/config.go`
- `backend/internal/config/config_test.go`
- `deploy/.env.example`
- `deploy/config.example.yaml`
- `deploy/docker-compose.yml`

### 风险原因

- `dev-master` 在这里引入了 reader 侧数据库配置
- `main` 也可能带来新的配置项（如 OIDC、调度、网关参数等）
- merge 后即使没有文本冲突，也可能出现：
  - reader 配置字段被覆盖
  - 新配置默认值丢失
  - 测试断言不再覆盖真实配置行为

---

### 2. DI / Bootstrap / Wire

- `backend/internal/repository/ent.go`
- `backend/internal/repository/wire.go`
- `backend/cmd/server/wire_gen.go`

### 风险原因

- `dev-master` 已经在这里建立了 writer / reader 双连接注入
- `main` 如果新增 service / repository / constructor 变化，很容易同时改到这些文件
- 尤其 `wire_gen.go` 是生成文件，最容易在 merge 后变成“编译能过但注入关系不对”

重点要确认：

- `ReaderEntClient`
- `ProvideReaderEnt`
- `ProvideReaderSQLDB`
- 各 repository constructor 的 reader 参数

都还保持正确

---

### 3. Reader 路由主战场 repository

- `backend/internal/repository/usage_log_repo.go`
- `backend/internal/repository/user_repo.go`
- `backend/internal/repository/api_key_repo.go`
- `backend/internal/repository/user_subscription_repo.go`
- `backend/internal/repository/setting_repo.go`
- `backend/internal/repository/redeem_code_repo.go`

### 风险原因

- 这些文件已经被 `dev-master` 改造成“部分读走 reader、写保持 writer”的模式
- `main` 即使不改同一行，也可能改到：
  - constructor
  - preload
  - helper
  - query shape
- merge 后最容易出现“逻辑还能跑，但 reader 路由 silently 失效”

---

### 4. 文档和运维口径

- `deploy/READ_WRITE_SPLIT_PILOT_CN.md`
- `deploy/HA_DEPLOYMENT_CN.md`
- `deploy/RELEASE_RUNBOOK_CN.md`

### 风险原因

- `dev-master` 已经记录了真实 rollout 状态
- `main` 如果同步过别的部署说明，可能导致文档口径回退或互相矛盾

---

## 四、merge 前必须分析的内容

### 1. 先看分支差异

```bash
git log --oneline --decorate --left-right origin/main...dev-master
git diff --stat origin/main...dev-master
git diff --stat dev-master...origin/main
```

### 2. 看哪些文件两边都动了

重点判断：

- 是否同时改了 config
- 是否同时改了 wire / wire_gen
- 是否同时改了 reader-aware repository

### 3. 看 merge 预演结果

推荐先在隔离 worktree 里做 `--no-commit` merge，确认：

- 有没有文本冲突
- 哪些文件虽然自动合并成功，但仍然需要人工语义检查

---

## 五、merge 后必须人工确认的点

### Config 层

确认这些 reader 配置仍然保留：

- `ReaderHost`
- `ReaderPort`
- `ReaderTargetSessionAttrs`
- `ReaderDSN()` / `ReaderDSNWithTimezone()`

同时确认 `main` 带来的新配置（如 OIDC 等）也没有丢。

### Wire / 注入层

确认这些 reader 依赖仍然存在并被正确传递：

- `ReaderEntClient`
- `ProvideReaderEnt`
- `ProvideReaderSQLDB`
- reader-aware repository constructor 参数

### Repository 层

逐个检查已改造的 repository：

- 读方法仍然走 reader helper
- 写方法和事务仍然走 writer
- 不要因为主线 merge 把 read helper 又改回 writer

### 文档层

确认部署/发布文档仍与当前真实状态一致，不要被主线老文档覆盖回去。

---

## 六、merge 后必须执行的验证

至少执行：

```bash
cd backend
go test ./...
```

如果涉及 app 层发布，还要验证：

- `/health`
- 已经切到 reader 的核心接口是否仍返回 `200`
- `docker compose` v2 发布路径是否仍正常

---

## 七、当前推荐口径

一句话总结：

> **main 合并到 dev-master 不是不能做，但必须先 fetch 最新 `origin/main`，再在隔离 worktree 里做 merge 预演，重点检查 config / wire / reader-aware repository 的兼容性，验证通过后再带回 dev-master。**

不要直接在当前工作区里盲合，也不要只看有没有文本冲突；对当前这个分支来说，真正的风险更常见于“自动合并成功，但 reader 逻辑被 silently 破坏”。
