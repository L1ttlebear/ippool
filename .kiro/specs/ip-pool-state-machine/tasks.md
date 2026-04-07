# 实现计划：IP 主机池状态机管理系统

## 概述

基于现有 ippool 项目魔改，复用 `ws`、`config`、`auditlog`、`dbcore`、`accounts/sessions`、`utils/log`，新增 `engine/`、`notifier/`、`web/` 核心模块，实现状态机驱动的 Leader Election + Cloudflare DDNS 自动切换系统。

## 任务

- [x] 1. 清理旧模块，搭建新项目骨架
  - 删除不需要的包：`database/clients`、`database/records`、`database/tasks`、`database/traffic_rules`、`api/admin`、`api/client`、`api/terminal`、`api/jsonRpc`、`utils/cloudflared`、`utils/geoip`、`utils/oauth`、`utils/renewal`、`utils/rpc`、`utils/notifier`（旧）、`utils/messageSender`（旧）
  - 更新 `go.mod` 模块名为 `ip-pool-state-machine`，清理无用依赖
  - 创建新目录结构：`engine/`、`notifier/`、`web/templates/`、`web/static/`
  - 保留并精简 `database/dbcore/`、`database/auditlog/`、`database/accounts/`、`config/`、`ws/`、`utils/log/`
  - _需求：13.1（项目结构）_

- [ ] 2. 定义数据模型与数据库初始化
  - [x] 2.1 创建 `database/models/host.go`，定义 `Host` 结构体（含 `HostState` 类型、所有字段、GORM tag）
  - [x] 2.2 创建 `database/models/check_record.go`，定义 `CheckRecord` 结构体
  - [x] 2.3 精简 `database/dbcore/dbcore.go`，AutoMigrate `Host`、`CheckRecord`、`Log`（复用现有）、`Config`（复用现有）
  - [x] 2.4 精简 `database/accounts/accounts.go` 和 `sessions.go`，保留 `CheckPassword`、`CreateSession`、`GetSession`、`DeleteSession`、首次启动自动创建默认账号逻辑
  - _需求：1.1、1.2、14.2_

- [ ] 3. 实现状态机核心
  - [x] 3.1 创建 `engine/statemachine.go`，实现 `StateMachine` 接口：`Transition`（含合法转换矩阵校验）、`GetState`、`ForceSet`；状态变更时写入 `auditlog` 并广播 WebSocket 事件
  - [ ]* 3.2 为状态机编写属性测试
    - **属性 4：状态转换合法性** — 生成随机（当前状态，目标状态）对，验证合法性判断与转换矩阵严格一致
    - **验证：需求 2.1**
  - [ ]* 3.3 为状态机编写单元测试
    - 覆盖每条合法转换路径和每条非法转换路径
    - _需求：2.1、2.4_

- [ ] 4. 实现健康检查器
  - [x] 4.1 创建 `engine/healthcheck.go`，实现 `HealthChecker` 接口：TCP 拨号检测 SSH 端口（10s 超时），失败后 5s 重试一次；检查结果写入 `check_records` 表
  - [ ] 4.2 在 `CheckAll` 中使用 semaphore（`chan struct{}`，容量默认 10）限制并发协程数，防止主机较多时瞬时耗尽本地文件句柄
  - [ ]* 4.3 为健康检查编写单元测试
    - 测试 TCP 超时、重试逻辑、结果写库、并发限制不超过 semaphore 容量
    - _需求：3.2、3.4、3.5_

- [x] 5. 实现流量判断与 Full 状态触发
  - [x] 5.1 在 `engine/healthcheck.go` 中补充流量读取逻辑（通过 SSH 读取 `/proc/net/dev`），将 `TrafficIn`/`TrafficOut` 填入 `CheckResult`
  - [ ] 5.2 在 `engine/statemachine.go` 中实现流量超限判断：`traffic > threshold` 时触发 `Ready→Full` 转换（threshold=0 表示不限制）
  - [ ]* 5.3 为流量比较编写属性测试
    - **属性 5：流量超限触发 Full** — 生成随机 threshold 和超过 threshold 的流量值，验证状态变为 Full
    - **属性 7：流量比较正确性** — 生成随机流量值和阈值，验证比较结果与 `traffic > threshold` 严格一致
    - **验证：需求 2.2、3.3**

- [x] 6. 实现 Leader Election
  - [ ] 6.1 创建 `engine/election.go`，实现 `LeaderElector` 接口：从 Ready Host 中按 Priority 升序选出最小值；空集合返回 nil；记录 Leader 变更事件至 auditlog
  - [ ]* 6.2 为 Leader 选举编写属性测试
    - **属性 8：Leader 选举最小 Priority** — 生成随机 Ready Host 集合（随机 Priority），验证选出的是 Priority 最小的
    - **属性 9：Leader 不变时跳过操作** — 连续两轮选举结果相同时，验证跳过标志正确返回
    - **验证：需求 4.1、4.3**
  - [ ]* 6.3 为 Leader 选举编写单元测试
    - 测试空集合、单元素、多元素、Priority 相同（同 Pool 内不可能，跨 Pool 验证隔离）
    - _需求：4.1、4.2、4.4_

- [ ] 7. 实现 SSH 命令执行器
  - [ ] 7.1 创建 `engine/executor.go`，实现 `CommandExecutor` 结构体，内含 semaphore（`chan struct{}`，容量由配置项 `max_ssh_concurrency` 控制，默认 5）限制最大并发 SSH 连接数
  - [ ] 7.2 SSH 连接使用 `context.WithTimeout`（30s）；命令执行使用**独立的** `context.WithTimeout`（60s），防止 curl 挂起等场景阻塞整个 Poller；超时视同 exit code 非 0
  - [ ] 7.3 在 executor 中支持 curl URL 类型的前置命令（检测 `http://` / `https://` 前缀，改用 HTTP GET 执行，同样受 60s context 超时保护）
  - [ ] 7.4 执行日志（stdout、stderr、exit code、耗时）写入 auditlog
  - [ ]* 7.5 为命令执行器编写单元测试（mock SSH）
    - 测试 exit code 0 继续流程、非 0 触发 Dead 标记、命令超时触发 Dead 标记、semaphore 并发限制
    - _需求：5.1、5.2、5.3、5.4、5.5_

- [ ] 8. 实现 Cloudflare DDNS 更新器
  - [ ] 8.1 创建 `engine/ddns.go`，实现 `DDNSUpdater` 接口：调用 CF API 更新 A 记录（15s 超时），失败重试 3 次（间隔 5s）；结果写入 auditlog
  - [ ]* 8.2 为 DDNS 更新器编写单元测试（mock HTTP）
    - 测试成功路径、重试逻辑（3 次失败后告警）、审计日志写入
    - _需求：6.1、6.2、6.4、6.5_

- [ ] 9. 实现熔断器
  - [ ] 9.1 创建 `engine/circuit.go`，实现 `CircuitBreaker`：内存状态（`isOpen bool`）；`Check(hosts)` 判断是否全部 Full/Dead；状态变化时触发通知并写 auditlog
  - [ ]* 9.2 为熔断器编写属性测试
    - **属性 12：熔断条件正确性** — 生成随机 Host 状态组合，验证熔断条件判断正确（有任意 Ready 则不熔断）
    - **属性 13：熔断自动解除** — 验证至少一台 Ready 时熔断自动解除
    - **验证：需求 7.1、7.4**

- [ ] 10. 实现轮询调度器（Poller）
  - [ ] 10.1 创建 `engine/poller.go`，实现定时调度器：按配置间隔（默认 60s，最小 10s）触发完整轮询流程（HealthCheck → StateMachine → Election → Executor → DDNS → Circuit）；支持动态更新间隔（配置变更后下一轮生效）
  - [ ] 10.2 在 poller 中串联完整流程：并发健康检查 → 批量状态更新 → 选举 → 前置命令 → DDNS → 熔断检查 → WebSocket 推送 poll_summary
  - _需求：3.1、4.1、4.3、5.4、7.3_

- [x] 11. 检查点 — 核心引擎自测
  - 确保所有 engine/ 单元测试和属性测试通过，ask the user if questions arise.

- [x] 12. 实现通知系统
  - [ ] 12.1 创建 `notifier/notifier.go`，实现 `Notifier` 接口：并发向所有已启用渠道发送；失败记录日志不影响主流程
  - [ ] 12.2 创建 `notifier/telegram.go`，实现 Telegram Bot API 发送（使用配置的 Token + Chat ID）
  - [ ] 12.3 创建 `notifier/webhook.go`，实现 Webhook 发送（POST JSON，包含 event type 和 payload）
  - [ ]* 12.4 为通知系统编写单元测试（mock HTTP）
    - 测试并发发送、失败隔离、消息格式
    - _需求：8.1、8.2、8.3、8.4、8.5_

- [ ] 13. 实现 WebSocket Hub
  - [x] 13.1 创建 `ws/hub.go`，实现 `Hub`：`Register`/`Unregister`/`Broadcast`（复用现有 `ws.SafeConn`）；连接建立时推送 snapshot
  - _需求：11.1、11.2、11.3、11.4、11.5_

- [x] 14. 实现 REST API 层
  - [x] 14.1 创建 `api/middleware.go`，实现 session cookie 鉴权中间件（复用 `accounts.GetSession`）；未登录重定向 `/login`
  - [x] 14.2 创建 `api/auth.go`：`GET /login`（渲染登录页）、`POST /login`（验证+设置 cookie）、`GET /logout`（清除 session）
  - [x] 14.3 创建 `api/hosts.go`：Host CRUD（GET/POST/PUT/DELETE `/api/hosts`）+ 手动设置状态（`PUT /api/hosts/:id/state`）；含 IP 格式校验（返回 400）和 Priority 唯一性校验（返回 409）
  - [x] 14.4 创建 `api/config.go`：`GET /api/config`、`PUT /api/config`（批量更新，轮询间隔 <10s 返回 400）
  - [ ] 14.5 创建 `api/logs.go`：`GET /api/logs`（分页）、`GET /api/logs/recent`（最近 20 条）
  - [ ] 14.6 创建 `api/notify.go`：`POST /api/notify/test`（发送测试通知）
  - [ ] 14.7 创建 `api/ws.go`：WebSocket 升级，注册到 Hub，连接建立后推送 snapshot
  - _需求：1.1–1.7、2.5、9.1–9.5、14.1–14.7_

- [ ] 15. 为 Host API 编写属性测试
  - [ ]* 15.1 为 IP 格式校验编写属性测试
    - **属性 2：非法 IP 格式拒绝** — 生成随机非法 IP 字符串，验证均返回 HTTP 400
    - **验证：需求 1.3**
  - [ ]* 15.2 为 Priority 唯一性编写属性测试
    - **属性 3：同 Pool Priority 唯一性** — 生成随机 Pool 名和 Priority 值，插入两次，验证第二次返回 HTTP 409
    - **验证：需求 1.4**
  - [ ]* 15.3 为 Host 创建初始状态编写属性测试
    - **属性 1：Host 创建初始状态不变量** — 任意合法 Host 创建后状态必须为 Ready，ID 唯一
    - **验证：需求 1.2**

- [x] 16. 实现前端模板与静态资源
  - [x] 16.1 创建 `web/static/style.css`：毛玻璃样式（`.glass-card`、深色渐变背景、状态徽章颜色、进度条、响应式布局）
  - [x] 16.2 创建 `web/templates/base.html`：基础布局（导航栏含设置入口和退出登录）
  - [x] 16.3 创建 `web/templates/login.html`：登录页（毛玻璃卡片，错误提示区域）
  - [x] 16.4 创建 `web/templates/index.html`：监控页（系统状态栏、主机卡片列表、熔断告警横幅、最近事件流；Go template 渲染 `IndexPageData`）
  - [ ] 16.5 创建 `web/templates/settings.html`：设置页（主机管理表格+模态框、DDNS 配置、通知配置、轮询参数、账号安全；Go template 渲染初始值）
  - [ ] 16.6 创建 `web/static/app.js`：WebSocket 连接与重连逻辑、接收 `snapshot`/`state_change`/`poll_summary` 消息并更新 DOM（状态徽章动画过渡）
  - [x] 16.7 创建 `web/web.go`：`embed.FS` 打包模板和静态文件，提供 `RenderTemplate` 函数和静态文件 handler
  - _需求：10.1–10.6、11.1–11.5、14.1_

- [x] 17. 实现页面路由与服务器启动
  - [x] 17.1 创建 `api/pages.go`：`GET /`（渲染 index.html，传入 `IndexPageData`）、`GET /settings`（渲染 settings.html，传入配置数据）
  - [ ] 17.2 精简 `cmd/server.go`：初始化 DB → 启动 Poller → 注册所有路由（页面路由 + API 路由 + WebSocket）→ 启动 Gin server
  - [ ] 17.3 精简 `cmd/root.go`：保留 cobra 根命令和 `server` 子命令，支持 `--port` 和 `--db` 参数（可被环境变量 `PORT`、`DB_PATH` 覆盖）
  - [x] 17.4 更新 `main.go`：入口调用 `cmd.Execute()`
  - _需求：9.3、9.4、13.3、14.1–14.6_

- [ ] 18. 实现 Docker 配置
  - [ ] 18.1 更新 `Dockerfile`：多阶段构建（`golang:1.24-alpine` 构建，`alpine:3.19` 运行），`CGO_ENABLED=1`，`VOLUME ["/data"]`，`EXPOSE 8080`
  - [x] 18.2 创建 `docker-compose.yml`：服务定义、`/data` 卷挂载、端口映射、`restart: unless-stopped`
  - _需求：13.1–13.5_

- [ ] 19. 实现日志自动清理与审计日志 API
  - [x] 19.1 在 `database/auditlog/log.go` 中添加 `CleanOldLogs(days int)` 函数，在 Poller 启动时注册定时清理任务（每天执行一次，清理 30 天前记录）
  - _需求：12.3、12.4_

- [ ] 20. 最终检查点 — 全量测试
  - 确保所有单元测试、属性测试通过；验证 Docker 构建成功；ask the user if questions arise.

## 备注

- 标有 `*` 的子任务为可选测试任务，可跳过以加快 MVP 交付
- 属性测试使用 [gopter](https://github.com/leanovate/gopter) 库，每个属性最少运行 100 次迭代
- 每个属性测试用注释标注对应属性编号，例如：`// Feature: ip-pool-state-machine, Property 8: Leader 选举最小 Priority`
- 所有任务均引用具体需求条款以保证可追溯性
- 检查点任务确保增量验证，避免集成阶段爆发大量问题
