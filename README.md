# IPPool

IP 主机池管理系统，基于 [Komari Monitor](https://github.com/komari-monitor/komari) 二次开发。

## 功能

- **主机池管理** — 管理多台主机（A/B/C...），查看实时流量、CPU、内存等指标
- **流量监控** — 实时监控每台主机的上行/下行/总流量
- **流量触发规则** — 当主机 A 的流量超过设定阈值时，自动对主机 B 执行指定命令
  - 支持执行自定义 `curl` 命令
  - 支持自动运行 `cf-ddns` 脚本（Cloudflare DDNS 更新）
  - 可配置冷却时间，防止重复触发
- **内置 SSH 终端** — 通过 Web 界面直接 SSH 到任意主机
- **任务系统** — 向一台或多台主机下发 shell 命令并查看执行结果

## 流量规则示例

假设主机池里有 A、B、C 三台机：

- 监控 A 机的单向流量（可选 `up`/`down`/`sum`/`max`/`min`）
- 当 A 机流量超过 500GB（`536870912000` 字节）时：
  - 对 B 机执行 `curl https://example.com/switch` 命令
  - 同时对 B 机运行 `bash /root/cf-ddns.sh`

## API

### 流量规则

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/admin/traffic-rules/` | 列出所有规则 |
| POST | `/api/admin/traffic-rules/` | 创建规则 |
| POST | `/api/admin/traffic-rules/:id` | 更新规则 |
| POST | `/api/admin/traffic-rules/:id/delete` | 删除规则 |

### 创建规则示例

```json
{
  "name": "A机流量切换",
  "enabled": true,
  "source_client": "<A机UUID>",
  "traffic_threshold": 536870912000,
  "traffic_type": "max",
  "target_clients": ["<B机UUID>"],
  "curl_command": "curl -s https://example.com/switch",
  "run_cf_ddns": true,
  "cf_ddns_command": "bash /root/cf-ddns.sh",
  "cooldown_seconds": 3600
}
```

### 流量类型说明

| 值 | 说明 |
|----|------|
| `max` | 上行和下行中较大的那个（默认） |
| `up` | 仅上行流量 |
| `down` | 仅下行流量 |
| `sum` | 上行 + 下行总和 |
| `min` | 上行和下行中较小的那个 |

## 部署

```bash
./ippool server --listen 0.0.0.0:25774
```

环境变量：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `IPPOOL_LISTEN` | `0.0.0.0:25774` | 监听地址 |
| `IPPOOL_DB_FILE` | `./data/ippool.db` | SQLite 数据库路径 |
| `ADMIN_USERNAME` | `admin` | 初始管理员用户名 |
| `ADMIN_PASSWORD` | 随机生成 | 初始管理员密码 |
