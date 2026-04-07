# 需求文档

## 简介

IP 主机池状态机管理系统是一个高可用的自动化运维平台，用于管理一组 IP 主机的健康状态，并通过状态机驱动的 Leader Election 机制，自动将域名（通过 Cloudflare DDNS）指向当前最优可用主机。系统每分钟执行一次全局轮询，依次完成健康检查、目标机选举、前置命令下发和 DDNS 更新，并在异常时触发告警通知。

技术栈：Go + Gin + GORM + SQLite + WebSocket，参考 ippool 项目架构。支持 Docker 部署。

**UI 设计原则：**
- 仅两个页面：监控页面（首页）+ 设置页面
- 监控页面采用毛玻璃（glassmorphism）视觉风格，每个功能区域附带简短说明文字
- 无前台/后台区分，无用户权限系统，无多余导航
- 极简交互：所有操作在当前页面内完成，不跳转

---

## 词汇表

- **System**：IP 主机池状态机管理系统整体
- **Host**：受管理的 IP 主机，是状态机的基本单元
- **Pool**：由多台 Host 组成的主机池，共享同一套 DDNS 和告警配置
- **State_Machine**：负责驱动 Host 状态转换的核心调度器
- **Health_Checker**：负责检测 Host 网络连通性和流量状态的组件
- **Leader_Elector**：负责从 Ready 状态的 Host 中按优先级选出目标机的组件
- **Command_Executor**：负责通过 SSH 或 curl 在目标 Host 上执行前置命令的组件
- **DDNS_Updater**：负责调用 Cloudflare API 将域名指向目标 Host IP 的组件
- **Notifier**：负责通过 Telegram 或 Webhook 发送告警和状态变更通知的组件
- **Poller**：每分钟触发一次全局轮询的定时调度器
- **Ready**：Host 状态之一，表示流量未超限且网络响应正常，可接受流量
- **Full**：Host 状态之一，表示单向流量已达到阈值，需停用
- **Dead**：Host 状态之一，表示主机关机、SSH 不通或网络超时，不可用
- **Leader**：当前被选中承接流量、域名指向其 IP 的 Host
- **Traffic_Threshold**：触发 Full 状态的单向流量上限，按 Host 独立配置，单位为字节
- **Priority**：Host 的选举优先级，数值越小优先级越高
- **Circuit_Breaker**：熔断器，当池内所有 Host 均为 Full 或 Dead 时触发终极告警并停止 DDNS 更新

---

## 需求

### 需求 1：Host 生命周期管理

**用户故事：** 作为管理员，我希望能够增删改查主机池中的 Host，以便灵活维护受管主机列表。

#### 验收标准

1. THE System SHALL 支持通过 REST API 创建 Host，字段包括：名称、IP 地址、SSH 端口、SSH 用户名、SSH 私钥或密码、优先级（Priority）、流量阈值（Traffic_Threshold）、所属 Pool、前置命令（curl 脚本 URL 或 shell 命令）。
2. THE System SHALL 为每个 Host 分配唯一 ID，并在创建时将初始状态设为 Ready。
3. WHEN 管理员提交的 Host IP 地址格式不合法，THEN THE System SHALL 返回 HTTP 400 及描述性错误信息。
4. WHEN 管理员提交的 Priority 值与同 Pool 内已有 Host 重复，THEN THE System SHALL 返回 HTTP 409 及冲突说明。
5. THE System SHALL 支持通过 REST API 更新 Host 的所有可配置字段，更新后立即生效于下一次轮询。
6. THE System SHALL 支持通过 REST API 删除 Host，删除后该 Host 不再参与轮询和选举。
7. THE System SHALL 支持通过 REST API 查询单个 Host 详情及 Pool 内全部 Host 列表，响应中包含当前状态和最后一次状态变更时间。

---

### 需求 2：Host 状态机与状态转换

**用户故事：** 作为系统，我希望每台 Host 的状态能够根据健康检查和流量数据自动流转，以便准确反映主机的实际可用性。

#### 验收标准

1. THE System SHALL 仅允许以下状态转换：
   - Ready → Full（流量超限）
   - Ready → Dead（健康检查失败）
   - Full → Ready（流量恢复至阈值以下，且健康检查通过）
   - Full → Dead（健康检查失败）
   - Dead → Ready（健康检查恢复通过，且流量未超限）
2. WHEN Host 的单向出站或入站流量超过 Traffic_Threshold，THE State_Machine SHALL 将该 Host 状态转换为 Full。
3. WHEN Health_Checker 检测到 Host SSH 连接超时或网络 ping 超时超过 10 秒，THE State_Machine SHALL 将该 Host 状态转换为 Dead。
4. WHEN Host 状态发生转换，THE System SHALL 记录转换时间、转换原因和前后状态至审计日志。
5. THE System SHALL 支持管理员通过 REST API 手动强制设置 Host 状态，手动设置的状态在下一次轮询前保持不变。

---

### 需求 3：全局健康检查（Health Check）

**用户故事：** 作为系统，我希望每分钟对所有 Host 执行健康检查，以便及时发现不可用主机。

#### 验收标准

1. WHEN Poller 触发轮询，THE Health_Checker SHALL 对 Pool 内所有 Host 并发执行健康检查。
2. THE Health_Checker SHALL 通过 TCP 连接检测 Host 的 SSH 端口（默认 22）是否可达，超时阈值为 10 秒。
3. THE Health_Checker SHALL 读取 Host 上报的单向流量数据，与 Traffic_Threshold 比较以判断是否触发 Full 状态。
4. WHEN 单次健康检查因网络抖动失败，THE Health_Checker SHALL 在 5 秒后重试一次，仅在两次均失败后才将 Host 标记为 Dead。
5. THE Health_Checker SHALL 在每次检查完成后，将检查结果（延迟、流量、状态）写入数据库供历史查询。

---

### 需求 4：Leader Election（目标机选举）

**用户故事：** 作为系统，我希望在每次轮询中自动选出最优可用主机作为 Leader，以便将流量引导至最佳节点。

#### 验收标准

1. WHEN 健康检查完成后，THE Leader_Elector SHALL 从当前状态为 Ready 的 Host 中，按 Priority 升序选出 Priority 值最小的 Host 作为 Leader。
2. WHEN Pool 内不存在状态为 Ready 的 Host，THE Leader_Elector SHALL 触发熔断逻辑（见需求 7），不执行后续步骤。
3. WHEN 当前 Leader 与上一轮 Leader 相同且状态仍为 Ready，THE Leader_Elector SHALL 跳过前置命令下发和 DDNS 更新，以减少不必要的操作。
4. THE System SHALL 记录每次 Leader 变更事件，包含变更时间、前任 Leader ID 和新任 Leader ID。

---

### 需求 5：前置命令下发

**用户故事：** 作为系统，我希望在 DDNS 更新前对目标 Host 执行前置命令，以便确保主机已完成必要的初始化或切换准备。

#### 验收标准

1. WHEN Leader 被选出且需要执行切换，THE Command_Executor SHALL 对目标 Host 执行配置的前置命令（curl 脚本 URL 或 shell 命令）。
2. THE Command_Executor SHALL 通过 SSH 连接目标 Host 执行命令，SSH 连接超时为 30 秒，命令执行超时为 60 秒。
3. WHEN 前置命令返回 exit code 0，THE System SHALL 继续执行 DDNS 更新流程。
4. WHEN 前置命令返回非 0 exit code 或执行超时，THE State_Machine SHALL 将该 Host 状态标记为 Dead，THE Leader_Elector SHALL 重新选举下一个 Ready Host，并对新目标机重复前置命令下发流程。
5. THE System SHALL 将前置命令的执行日志（stdout、stderr、exit code、耗时）持久化存储，供管理员查询。
6. WHEN 重新选举后仍无 Ready Host 可用，THE System SHALL 触发熔断逻辑（见需求 7）。

---

### 需求 6：DDNS 更新

**用户故事：** 作为系统，我希望在前置命令成功后自动更新 Cloudflare DDNS，以便将域名实时指向当前 Leader 的 IP。

#### 验收标准

1. WHEN 前置命令执行成功，THE DDNS_Updater SHALL 调用 Cloudflare API 将指定域名的 A 记录更新为 Leader 的 IPv4 地址。
2. THE DDNS_Updater SHALL 使用配置的 Cloudflare API Token、Zone ID 和 Record Name 执行更新，API 调用超时为 15 秒。
3. WHEN Cloudflare API 返回成功响应，THE Notifier SHALL 发送 DDNS 切换成功通知，内容包含：新 Leader 名称、IP 地址、域名和切换时间。
4. WHEN Cloudflare API 调用失败或超时，THE DDNS_Updater SHALL 重试最多 3 次，每次间隔 5 秒；若 3 次均失败，THE Notifier SHALL 发送 DDNS 更新失败告警。
5. THE System SHALL 记录每次 DDNS 更新操作的结果（成功/失败、目标 IP、时间戳）至审计日志。

---

### 需求 7：熔断与终极告警

**用户故事：** 作为管理员，我希望当所有主机均不可用时系统能立即告警并停止无效操作，以便快速感知并介入处理。

#### 验收标准

1. WHEN Pool 内所有 Host 的状态均为 Full 或 Dead，THE Circuit_Breaker SHALL 停止本轮 DDNS 更新尝试。
2. WHEN Circuit_Breaker 触发，THE Notifier SHALL 立即发送终极告警，内容包含：Pool 名称、各 Host 当前状态列表和触发时间。
3. WHILE Circuit_Breaker 处于触发状态，THE Poller SHALL 继续每分钟执行健康检查，以便在 Host 恢复后自动解除熔断。
4. WHEN 至少一台 Host 状态恢复为 Ready，THE Circuit_Breaker SHALL 自动解除，THE Notifier SHALL 发送熔断解除通知。
5. THE System SHALL 确保终极告警在 Circuit_Breaker 触发后 60 秒内送达通知渠道。

---

### 需求 8：通知系统

**用户故事：** 作为管理员，我希望通过 Telegram 或 Webhook 接收系统关键事件通知，以便及时掌握主机池状态变化。

#### 验收标准

1. THE System SHALL 支持配置 Telegram Bot Token 和 Chat ID 作为通知渠道。
2. THE System SHALL 支持配置自定义 Webhook URL 作为通知渠道，请求体为 JSON 格式。
3. WHERE 多个通知渠道均已配置，THE Notifier SHALL 向所有已启用的渠道并发发送通知。
4. WHEN 通知发送失败，THE Notifier SHALL 记录失败原因至系统日志，不影响主流程继续执行。
5. THE System SHALL 支持以下通知事件类型：Leader 变更、Host 状态转换、DDNS 更新成功、DDNS 更新失败、熔断触发、熔断解除。
6. THE System SHALL 支持管理员通过 REST API 发送测试通知，以验证通知渠道配置是否正确。

---

### 需求 9：系统配置管理

**用户故事：** 作为管理员，我希望通过设置页面管理系统全局配置，以便灵活调整运行参数，无需重启服务。

#### 验收标准

1. THE System SHALL 提供单一设置页面，包含以下所有配置项，分组展示并附带说明文字：
   - **主机管理**：添加/编辑/删除 Host（名称、IP、SSH 端口、用户名、私钥/密码、优先级、流量阈值、前置命令）
   - **DDNS 配置**：Cloudflare API Token、Zone ID、Record Name（域名）
   - **通知配置**：Telegram Bot Token + Chat ID、Webhook URL
   - **轮询参数**：轮询间隔（默认 60 秒，最小 10 秒）
   - **账号安全**：修改管理员密码
2. WHEN 管理员将轮询间隔设置为小于 10 秒的值，THEN THE System SHALL 在页面内显示错误提示，拒绝保存。
3. THE System SHALL 将所有配置持久化存储至 SQLite 数据库，重启后自动加载。
4. WHEN 配置发生变更并保存，THE System SHALL 在下一次轮询周期生效，无需重启服务。
5. THE System SHALL 在设置页面提供"发送测试通知"按钮，点击后立即向已配置渠道发送测试消息并在页面内显示结果。

---

### 需求 14：管理员认证

**用户故事：** 作为管理员，我希望系统有登录保护，以便防止未授权访问。

#### 验收标准

1. THE System SHALL 提供登录页面（`/login`），包含用户名和密码输入框，采用毛玻璃风格。
2. WHEN 管理员首次启动系统且数据库中无账号，THE System SHALL 自动创建默认管理员账号并将用户名和密码打印到控制台。
3. WHEN 登录成功，THE System SHALL 设置 `session_token` cookie（有效期 30 天），并跳转到监控页。
4. WHEN 登录失败（用户名或密码错误），THE System SHALL 在登录页显示错误提示，不泄露具体原因。
5. ALL 页面路由（`/`、`/settings`）和 API 路由（`/api/*`、`/ws`）SHALL 要求有效 session，未登录请求重定向到 `/login`。
6. THE System SHALL 提供退出登录功能（`/logout`），清除 session 后跳转到登录页。
7. THE System SHALL 支持在设置页面修改管理员密码，修改后使所有现有 session 失效。

---

### 需求 10：监控页面（Web UI）

**用户故事：** 作为管理员，我希望通过一个简洁的监控页面实时掌握所有主机状态，以便快速判断系统健康情况。

#### 验收标准

1. THE System SHALL 提供单一监控首页，采用毛玻璃（glassmorphism）视觉风格，深色渐变背景，卡片使用半透明模糊效果。
2. 监控页面 SHALL 包含以下区域，每个区域附带一行简短说明文字（灰色小字）：
   - **系统状态栏**（顶部）：显示当前 Leader 主机名、域名、最后切换时间；说明："当前正在承接流量的主机"
   - **主机卡片列表**：每台主机一张卡片，显示名称、IP、状态徽章（Ready=绿/Full=橙/Dead=红）、流量用量进度条、优先级；说明："主机池中所有受管主机的实时状态"
   - **熔断告警横幅**：仅在熔断触发时显示，红色醒目提示；说明："所有主机均不可用，请立即介入"
   - **最近事件流**（底部）：最新 20 条状态变更/切换记录，时间倒序；说明："系统最近的自动操作记录"
3. THE System SHALL 通过 WebSocket 实时更新监控页面数据，无需手动刷新。
4. WHEN Host 状态发生变化，对应卡片 SHALL 以动画过渡方式更新状态徽章颜色。
5. 监控页面 SHALL 在移动端浏览器上正常显示（响应式布局）。
6. 监控页面顶部右上角 SHALL 提供"设置"入口链接，是唯一的页面跳转。

---

### 需求 11：实时状态推送（WebSocket）

**用户故事：** 作为管理员，我希望通过 WebSocket 实时接收主机池状态更新，以便监控页面无刷新显示最新状态。

#### 验收标准

1. THE System SHALL 提供 WebSocket 端点，客户端连接后可实时接收 Host 状态变更事件。
2. WHEN Host 状态发生转换，THE System SHALL 在 2 秒内通过 WebSocket 向所有已连接客户端推送状态变更消息。
3. WHEN 轮询完成，THE System SHALL 通过 WebSocket 推送本轮轮询摘要，包含各 Host 最新状态和当前 Leader。
4. WHEN WebSocket 客户端连接建立，THE System SHALL 立即推送当前所有 Host 的完整状态快照。
5. IF WebSocket 连接断开，THEN THE System SHALL 释放相关资源，不影响系统其他功能正常运行。

---

### 需求 12：审计日志

**用户故事：** 作为管理员，我希望在监控页面查看最近的操作记录，以便快速了解系统自动执行了哪些操作。

#### 验收标准

1. THE System SHALL 记录以下事件并在监控页面事件流中展示：Host 状态转换、Leader 变更、DDNS 更新结果、熔断触发/解除。
2. 每条事件记录 SHALL 包含：时间戳、事件类型、相关主机名、简短描述。
3. THE System SHALL 自动清理 30 天前的日志，以控制数据库体积。
4. THE System SHALL 支持通过 REST API 查询日志，供外部工具集成使用。

---

### 需求 13：Docker 部署支持

**用户故事：** 作为运维人员，我希望通过 Docker 一键部署本系统，以便快速上线且不依赖宿主机环境。

#### 验收标准

1. THE System SHALL 提供 `Dockerfile`，基于多阶段构建，最终镜像基于 `alpine`，体积尽量小。
2. THE System SHALL 提供 `docker-compose.yml`，包含服务定义、数据卷挂载（持久化 SQLite 数据库）和端口映射。
3. THE System SHALL 通过环境变量支持以下启动配置覆盖：监听端口（默认 `8080`）、数据库文件路径（默认 `/data/ippool.db`）。
4. THE System SHALL 将数据库文件存储在容器内 `/data` 目录，该目录应挂载为外部卷以持久化数据。
5. WHEN 容器重启，THE System SHALL 自动从数据库恢复所有配置和主机状态，无需人工干预。
