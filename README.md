# IP Pool Monitor

高可用 IP 主机池状态机管理系统。自动监控主机健康状态，通过 Leader Election 机制将 Cloudflare DDNS 指向当前最优可用主机。

## 功能

- **状态机管理** — 每台主机自动维护 Ready / Full / Dead 三种状态
- **健康检查** — 每分钟并发 TCP 检测所有主机 SSH 端口，失败自动重试
- **流量监控** — 通过 SSH 读取 `/proc/net/dev`，流量超限自动标记 Full
- **Leader Election** — 按优先级自动选出最优 Ready 主机
- **前置命令** — 切换前对目标主机执行 curl 脚本或 shell 命令（exit code 0 才继续）
- **Cloudflare DDNS** — 命令成功后自动更新 A 记录，失败重试 3 次
- **熔断保护** — 所有主机不可用时停止 DDNS 操作并发送终极告警
- **通知** — 支持 Telegram Bot 和自定义 Webhook
- **毛玻璃 UI** — 监控页 + 设置页，WebSocket 实时更新，无需刷新
- **管理员认证** — Session cookie 登录保护

---

## 快速部署（Docker Compose）

### 1. 克隆仓库

```bash
git clone https://github.com/L1ttlebear/ippool.git
cd ippool
```

### 2. 启动服务

```bash
docker-compose up -d
```

首次启动会自动构建镜像（约 2-3 分钟），完成后访问：

```
http://your-server-ip:8080
```

### 3. 获取初始密码

```bash
docker-compose logs ippool | grep "Default admin"
```

输出示例：
```
Default admin account created  username=admin  password=xK9mP2qR4nLw
```

### 4. 登录并配置

1. 打开浏览器访问 `http://your-server-ip:8080`
2. 用上面的账号密码登录
3. 点击右上角「设置」
4. 依次配置：主机列表、Cloudflare DDNS、通知渠道

---

## 配置说明

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `0.0.0.0:8080` | 监听地址 |
| `DB_PATH` | `/data/ippool.db` | SQLite 数据库路径 |

### 修改端口

编辑 `docker-compose.yml`：

```yaml
ports:
  - "9090:8080"   # 改为 9090 对外暴露
environment:
  - PORT=0.0.0.0:8080
```

### 数据持久化

数据库文件存储在 Docker volume `ippool_data` 中，容器重启不丢失数据。

查看数据存储位置：

```bash
docker volume inspect ippool_ippool_data
```

---

## 常用命令

```bash
# 启动
docker-compose up -d

# 查看日志
docker-compose logs -f ippool

# 停止
docker-compose down

# 重启
docker-compose restart ippool

# 更新（重新构建）
docker-compose down
docker-compose up -d --build

# 备份数据库
docker cp $(docker-compose ps -q ippool):/data/ippool.db ./backup.db
```

---

## 主机状态说明

| 状态 | 含义 |
|------|------|
| 🟢 Ready | 流量未超限，网络响应正常，可接受流量 |
| 🟠 Full | 单向流量已达阈值，暂停使用 |
| 🔴 Dead | 主机关机或 SSH 不通，不可用 |

### 状态转换规则

```
Ready → Full   流量超过 traffic_threshold
Ready → Dead   健康检查失败（两次）
Full  → Ready  流量恢复 + 健康检查通过
Full  → Dead   健康检查失败
Dead  → Ready  健康检查恢复 + 流量未超限
```

---

## 轮询流程

每分钟自动执行：

1. **全局健康检查** — 并发 TCP 检测所有主机 SSH 端口
2. **状态更新** — 根据检查结果更新各主机状态
3. **熔断检查** — 若全部 Full/Dead，触发告警并停止后续操作
4. **Leader Election** — 选出 Priority 最小的 Ready 主机
5. **前置命令** — 对目标主机执行配置的命令（失败则重新选举）
6. **DDNS 更新** — 命令成功后更新 Cloudflare A 记录
7. **通知** — 发送切换成功通知

---

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/hosts` | 获取所有主机 |
| POST | `/api/hosts` | 添加主机 |
| PUT | `/api/hosts/:id` | 更新主机 |
| DELETE | `/api/hosts/:id` | 删除主机 |
| PUT | `/api/hosts/:id/state` | 手动设置状态 |
| GET | `/api/config` | 获取配置 |
| PUT | `/api/config` | 更新配置 |
| GET | `/api/logs/recent` | 最近 20 条事件 |
| POST | `/api/notify/test` | 发送测试通知 |
| GET | `/ws` | WebSocket 实时推送 |

---

## 本地开发

```bash
# 需要 Go 1.24+ 和 CGO 支持（SQLite）
go mod download
go run . --listen 0.0.0.0:8080 --db ./data/ippool.db
```
