# NetworkPilot — Adaptive PBR Probe System

> Classify egress routing (IEPL / CN2 / CERNET / Standard / Blocked) for any set of domains, visualize the hop path on a world map, and deliver a ready-to-import Surge ruleset or Xray routing config. All in one stack.

English docs below · [中文文档见此](#中文文档)

---

## Overview

NetworkPilot is a full-stack network diagnostics platform that:

1. **Resolves** a batch of domains over DNS-over-HTTPS (no OS resolver interference).
2. **Traces** the forwarding path via raw ICMP/UDP probes (libpcap) or a deterministic synthetic prober for demos.
3. **Enriches** every responding hop with GeoIP city/country/ASN data (MaxMind MMDB).
4. **Classifies** each path into `IEPL_Direct`, `CN2_Premium`, `CERNET_Detour`, `Blocked`, or `Standard_163` using AS-path heuristics plus a border-delta RTT gate.
5. **Exposes** the results via REST, a live React dashboard (Leaflet + ECharts), and auto-generated subscription endpoints for Surge and Xray (`ETag` / `If-None-Match` supported).

It ships as four containers driven by `docker compose`: backend (Go), frontend (Vite + nginx), Postgres, Redis.

## Architecture

```
┌─────────────┐    ┌──────────────────────────────────────────────┐
│  Browser    │───▶│  pbr_frontend (nginx + React SPA)            │
└─────────────┘    │     /api/*  ──▶  pbr_backend:8080            │
                   └──────────────────────────────────────────────┘
                                       │
         ┌─────────────────────────────┴─────────────────────────────┐
         │                                                           │
         ▼                                                           ▼
┌────────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│ DoH Resolver       │    │ Prober           │    │ GeoIP Enricher  │
│ dns.google / CF    │    │ pcap OR synthetic│    │ MaxMind MMDB    │
└────────────────────┘    └──────────────────┘    └─────────────────┘
         │                         │                       │
         └──────────── Orchestrator (worker pool) ─────────┘
                                  │
                                  ▼
                           Classifier ──▶ TraceResult
                                  │
                     ┌────────────┼─────────────┐
                     ▼            ▼             ▼
              pbr_database   pbr_redis    Delivery (Surge / Xray)
              (jobs+traces)  (RIPE cache)
```

## Features

- **Bulk probing** — submit raw domains, paste a Surge ruleset inline, or pass a remote Surge/DOMAIN-SET URL and we'll recursively expand it.
- **Path classification** with a tunable IEPL border-delta threshold (default 3 ms) and ASN allow-lists for CN2 (AS4809), CERNET (AS4538), and China Telecom (AS4134).
- **Concurrent worker pool** — `WORKER_CONCURRENCY` configurable; per-job progress counters update every write.
- **Durable jobs** — Postgres-backed store for jobs + traces; automatic in-memory fallback if `DATABASE_URL` is empty.
- **Cached RIPE lookups** — Redis-backed origin-AS/prefix/community lookup via `stat.ripe.net`; TTL configurable.
- **Delivery endpoints** — `/api/v1/delivery/surge/optimized.list` (`text/plain`) and `/api/v1/delivery/xray/optimized.json` both support `ETag` + `If-None-Match` → `304`.
- **Dashboard** — live job progress, per-status donut chart, Leaflet hop map with polyline, sortable result table, RIPE inspector, one-click subscription URLs.

## Tech Stack

| Layer            | Technology                                                     |
| ---------------- | -------------------------------------------------------------- |
| Backend runtime  | Go 1.24, chi router, pgx/v5, go-redis/v9, gopacket + libpcap   |
| Frontend runtime | React 18, TypeScript 5.5, Vite 5, Tailwind 3, Zustand          |
| Visualization    | Leaflet + Carto dark tiles, ECharts via `echarts-for-react`    |
| Data stores      | Postgres 16, Redis 7                                           |
| Packaging        | Multi-stage Dockerfiles + docker-compose + nginx reverse proxy |

## Quick Start

Prerequisites: Docker 24+ (or OrbStack), ~2 GB free RAM, ports `8080` and `8081` available.

```bash
# 1. clone + configure
cp .env.example .env                 # adjust PROBE_MODE, DOH_ENDPOINT, etc. if needed

# 2. launch everything
docker compose -f deploy/docker-compose.yaml up -d --build

# 3. smoke test
curl -s http://localhost:8080/healthz
curl -s -X POST http://localhost:8080/api/v1/probes \
  -H 'content-type: application/json' \
  -d '{"domains":["google.com","netflix.com","github.com"]}'

# 4. open the dashboard
open http://localhost:8081
```

To stop: `docker compose -f deploy/docker-compose.yaml down` (add `-v` to wipe Postgres/Redis volumes).

## Project Structure

```
NetworkPilot/
├── backend/                      # Go service
│   ├── cmd/server/main.go        # entrypoint (wiring + synthetic prober)
│   └── internal/
│       ├── api/                  # chi router + HTTP handlers
│       ├── classifier/           # IEPL / CN2 / CERNET / Blocked logic
│       ├── config/               # env-var loader
│       ├── delivery/             # Surge + Xray renderers, ETag
│       ├── geoip/                # MaxMind MMDB enricher
│       ├── model/                # shared types (Hop, TraceResult, ProbeJob)
│       ├── orchestrator/         # worker pool / job runner
│       ├── parser/               # Surge ruleset parser with recursive fetch
│       ├── probe/                # pcap prober + mock
│       ├── resolver/             # DoH resolver
│       ├── ripe/                 # stat.ripe.net client + Redis cache
│       └── store/                # Postgres + in-memory stores
├── web/                          # React SPA
│   ├── src/
│   │   ├── App.tsx
│   │   ├── api.ts                # axios client against /api/v1
│   │   ├── store.ts              # Zustand state + status colors
│   │   └── components/           # ProbeForm, HopMap, ResultsTable, ...
│   ├── vite.config.ts
│   └── tailwind.config.js
├── deploy/
│   ├── Dockerfile.backend        # golang:1.24 + libpcap → debian slim
│   ├── Dockerfile.frontend       # node:20 builder → nginx:alpine
│   ├── docker-compose.yaml
│   └── nginx/nginx.conf
├── config/
│   └── engine.yaml               # optional runtime knobs (not required)
└── .env.example
```

## Configuration

The backend reads all configuration from environment variables. Every variable has a sensible default so you can boot with `PROBE_MODE=mock` and nothing else.

| Variable               | Default                             | Meaning                                                 |
| ---------------------- | ----------------------------------- | ------------------------------------------------------- |
| `HTTP_ADDR`            | `:8080`                             | HTTP listener address                                   |
| `PROBE_MODE`           | `pcap`                              | `pcap` (real ICMP traceroute) or `mock` (synthetic)     |
| `PROBE_INTERFACE`      | auto                                | Network interface for libpcap (e.g. `en0`, `eth0`)      |
| `WORKER_CONCURRENCY`   | `32`                                | Goroutines pulled from the domain task channel          |
| `MAX_TTL`              | `40`                                | Maximum TTL for traceroute probes                       |
| `DOH_ENDPOINT`         | `https://cloudflare-dns.com/dns-query` | DNS-over-HTTPS URL (JSON flavor)                     |
| `DOH_BOOTSTRAP_IPS`    | `1.1.1.1,1.0.0.1`                   | Comma-separated IPs used to dial the DoH host           |
| `DATABASE_URL`         | *(empty → memory)*                  | `postgres://user:pass@host:5432/db?sslmode=disable`     |
| `REDIS_URL`            | *(empty → memory)*                  | `redis://host:6379/0`                                   |
| `MMDB_CITY_PATH`       | *(empty)*                           | Path to `GeoLite2-City.mmdb`                            |
| `MMDB_ASN_PATH`        | *(empty)*                           | Path to `GeoLite2-ASN.mmdb`                             |
| `RIPE_CACHE_TTL_HOURS` | `24`                                | TTL for cached RIPE responses                           |

Postgres-only (compose): `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` (defaults `pbr`/`pbr`/`pbr`).

## API Reference

Base URL: `http://localhost:8080/api/v1` (direct) or `http://localhost:8081/api/v1` (through nginx).

### `POST /probes`
Create a new probe job.

```json
{
  "domains":      ["google.com", "netflix.com"],
  "surge_url":    "https://example.com/optional-ruleset.list",
  "surge_inline": "DOMAIN-SUFFIX,github.com,DIRECT\n..."
}
```

Any combination of the three inputs works; domains are normalized, deduped, and the job starts asynchronously. Returns `202 Accepted` with the `ProbeJob` record.

### `GET /probes/{id}` — job status
```json
{
  "id":          "…",
  "status":      "queued | running | completed | failed",
  "total":       15,
  "processed":   15,
  "counts":      { "IEPL_Direct": 3, "CN2_Premium": 5, "CERNET_Detour": 1, "Standard_163": 4, "Blocked": 2 },
  "last_error":  "resolve github.com: …"
}
```

### `GET /probes/{id}/results` — full trace results
Returns `{ "results": TraceResult[] }` where each entry includes the resolved IPv4, classification, path metrics (`total_hops`, `destination_rtt_ms`, `border_delta_ms`, `asn_sequence`), and the enriched hop list.

### `GET /results/latest` — optimized rules for dashboard
### `GET /ripe/{resource}` — proxy to `stat.ripe.net` with Redis cache
### `GET /delivery/surge/optimized.list` — `text/plain` Surge `RULE-SET`
### `GET /delivery/xray/optimized.json` — Xray routing JSON

Both delivery endpoints respond with `ETag`; clients sending a matching `If-None-Match` header get `304 Not Modified`.

## Probe Modes

### Mock (`PROBE_MODE=mock`)
A deterministic synthetic prober at [backend/cmd/server/main.go](backend/cmd/server/main.go) produces realistic CN→US hop patterns bucketed by the target IP. Zero privileges required — perfect for laptops and CI.

### Pcap (`PROBE_MODE=pcap`)
Real ICMP traceroute using libpcap. In Docker requires `NET_RAW` + `NET_ADMIN` capabilities and host networking. In [deploy/docker-compose.yaml](deploy/docker-compose.yaml) uncomment:

```yaml
    # network_mode: host
    # cap_add: [NET_RAW, NET_ADMIN]
```

Drop `GeoLite2-City.mmdb` and `GeoLite2-ASN.mmdb` into `config/geoip/` before starting for ASN/city enrichment.

## Classification Logic

Implemented in [backend/internal/classifier/classifier.go](backend/internal/classifier/classifier.go):

1. **IEPL_Direct** — ASN path stays inside the IEPL allow-list (`4134`, `4809`, `58453`) **and** the RTT jump across the last domestic hop is less than the `ieplDeltaUpperBoundMS` threshold (default `3.0 ms`). This catches short-haul international private lines.
2. **CN2_Premium** — path transits AS `4809` at any point.
3. **CERNET_Detour** — path transits AS `4538`.
4. **Blocked** — resolution failed or fewer than 2 responding hops.
5. **Standard_163** — the fallback when none of the above match (typical ChinaNet routing).

Only `IEPL_Direct` and `CN2_Premium` domains are written to the Surge / Xray optimized list.

## Frontend Dashboard

The SPA at `http://localhost:8081`:

- **Left column** — Probe form with three submission modes, live job progress bar, classification donut, delivery subscription links.
- **Right column** — world map drawing the current domain's hop polyline (Leaflet + Carto dark tiles), sortable result table (click a row to inspect), hop detail table, RIPE lookup panel.
- **Polling** — when a job is running, the UI re-polls `/probes/{id}` + `/probes/{id}/results` every 2 s; once completed it refreshes `/results/latest`.

Routes are proxied through the nginx container: all `/api/*` traffic goes to `pbr_backend:8080` inside the compose network.

## Development

### Backend (without Docker)
```bash
cd backend
go mod download
PROBE_MODE=mock go run ./cmd/server
```

### Frontend (without Docker)
```bash
cd web
npm install
npm run dev          # Vite dev server on :5173, proxies /api to :8080
```

Point `BACKEND_URL=http://localhost:8080` before `npm run dev` if your backend runs elsewhere.

### Submit probes from the CLI
```bash
curl -s -X POST http://localhost:8080/api/v1/probes \
  -H 'content-type: application/json' \
  -d "$(jq -nc --argjson d "$(jq -R . < list.txt | jq -s .)" '{domains:$d}')"
```

## Troubleshooting

| Symptom                                              | Likely cause / fix                                                                                                    |
| ---------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `go: go.mod requires go >= 1.24`                     | Rebuild the backend image — Dockerfile pins `golang:1.24-bookworm`.                                                   |
| Every result is `Blocked`                            | DoH endpoint rejected the JSON format. Use `https://cloudflare-dns.com/dns-query` (default) or Google's `/resolve`.   |
| `pcap: device ... not found` inside the container    | Switch to `PROBE_MODE=mock`, or enable `network_mode: host` + `NET_RAW/NET_ADMIN` caps.                               |
| RIPE lookups return `source: "ripe"` but never cache | `REDIS_URL` missing / unreachable — the code auto-falls back to in-memory, which empties on restart.                  |
| Dashboard shows "0 optimized rules"                  | All trace results hit `Standard_163` / `CERNET_Detour` / `Blocked`. Only IEPL/CN2 entries are promoted to the ruleset.|
| `HEAD /api/v1/delivery/...` returns 405              | Expected — delivery endpoints are registered for `GET` only. Use `GET` (real clients do).                             |

## Roadmap

- Persistent history views with per-domain status timelines.
- BGP community–driven classification refinements using the cached RIPE data.
- Optional IPv6 probing (currently IPv4 only, as the resolver is `LookupIPv4`).
- Authenticated API + multi-tenant subscriptions.

## License

MIT. See the source files for per-package attribution.

---

## 中文文档

> 对任意域名集合探测其出境路由分类（IEPL / CN2 / CERNET / 普通 / 封锁），在世界地图上可视化跳数路径，并自动输出可直接订阅的 Surge 规则或 Xray 路由配置。整套方案一次部署即可使用。

### 项目定位

NetworkPilot 是一个面向“国内访问国际站点”场景的网络诊断平台，目标用户包括网络工程师、机场/代理运维、跨境业务团队。典型用法：

- 批量判断一份域名清单走的是 IEPL 直连、CN2 GIA、CERNET 绕行还是普通 163 出口。
- 将分类结果以 Surge / Xray 订阅地址的形式自动下发，客户端无需人肉维护规则集。
- 在地图上直观查看某个域名从国内机房到海外目标的逐跳地理位置与 AS 路径。
- 在 RIPE 工具中查询任意 IP / 前缀的 Origin-AS 与 BGP Community，带 Redis 缓存。

### 系统架构

```
┌─────────────┐    ┌──────────────────────────────────────────────┐
│   浏览器    │───▶│  pbr_frontend（nginx + React SPA）           │
└─────────────┘    │     /api/*  ──▶  pbr_backend:8080            │
                   └──────────────────────────────────────────────┘
                                       │
         ┌─────────────────────────────┴─────────────────────────────┐
         ▼                                                           ▼
┌────────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│ DoH 解析器         │    │ 探测器           │    │ GeoIP 富化      │
│ 走 HTTPS 防污染    │    │ pcap / 合成模式  │    │ MaxMind mmdb    │
└────────────────────┘    └──────────────────┘    └─────────────────┘
         │                         │                       │
         └────────────── 调度器（Worker 池）──────────────┘
                                  │
                                  ▼
                        分类器 ──▶ TraceResult
                                  │
                     ┌────────────┼─────────────┐
                     ▼            ▼             ▼
               pbr_database  pbr_redis    交付端点（Surge / Xray）
              （任务+轨迹） （RIPE 缓存）
```

### 主要特性

- **批量探测**：支持直接粘贴域名、粘贴 Surge 规则集内容、或传入远程 Surge/DOMAIN-SET URL（递归展开）三种输入方式。
- **路径分类**：IEPL 边界 RTT 差值阈值（默认 3 ms）可调；CN2（AS4809）、CERNET（AS4538）、中国电信（AS4134）白名单可配置。
- **并发 Worker 池**：`WORKER_CONCURRENCY` 可配置，每次任务状态写入都会更新数据库。
- **持久化任务**：Postgres 存储任务与 trace 结果；若 `DATABASE_URL` 为空则自动降级为内存存储。
- **RIPE 查询缓存**：通过 `stat.ripe.net` 查询 origin AS / 前缀 / community，Redis 缓存 TTL 可配置。
- **订阅下发**：`/api/v1/delivery/surge/optimized.list`（`text/plain`）与 `/api/v1/delivery/xray/optimized.json` 均支持 `ETag` 与 `If-None-Match`，命中直接返回 `304 Not Modified`。
- **可视化面板**：任务进度条、分类环形图、Leaflet 跳数地图折线、可排序结果表、跳详情、RIPE 面板、一键复制订阅链接。

### 技术栈

| 层级     | 技术                                                             |
| -------- | ---------------------------------------------------------------- |
| 后端运行 | Go 1.24、chi 路由、pgx/v5、go-redis/v9、gopacket + libpcap       |
| 前端运行 | React 18、TypeScript 5.5、Vite 5、Tailwind 3、Zustand            |
| 可视化   | Leaflet + Carto 深色瓦片、ECharts（`echarts-for-react`）         |
| 数据存储 | Postgres 16、Redis 7                                             |
| 打包交付 | 多阶段 Dockerfile + docker-compose + nginx 反向代理              |

### 快速开始

环境要求：Docker 24+（推荐 OrbStack）、约 2 GB 可用内存、本机 `8080` 与 `8081` 端口未被占用。

```bash
# 1. 克隆 + 准备环境变量
cp .env.example .env
# 根据需要修改 PROBE_MODE、DOH_ENDPOINT 等

# 2. 启动整套服务
docker compose -f deploy/docker-compose.yaml up -d --build

# 3. 冒烟测试
curl -s http://localhost:8080/healthz
curl -s -X POST http://localhost:8080/api/v1/probes \
  -H 'content-type: application/json' \
  -d '{"domains":["google.com","netflix.com","github.com"]}'

# 4. 打开仪表盘
open http://localhost:8081
```

停止：`docker compose -f deploy/docker-compose.yaml down`（加 `-v` 会清空 Postgres/Redis 数据卷）。

### 目录结构

```
NetworkPilot/
├── backend/                      # Go 后端
│   ├── cmd/server/main.go        # 入口，包含合成探测器实现
│   └── internal/
│       ├── api/                  # chi 路由与 HTTP handler
│       ├── classifier/           # IEPL / CN2 / CERNET / Blocked 判定
│       ├── config/               # 环境变量加载
│       ├── delivery/             # Surge / Xray 渲染 + ETag
│       ├── geoip/                # MaxMind mmdb 富化
│       ├── model/                # 跨模块共享类型
│       ├── orchestrator/         # Worker 池调度
│       ├── parser/               # Surge 规则集解析（递归拉取）
│       ├── probe/                # pcap 探测器与 mock
│       ├── resolver/             # DoH 解析
│       ├── ripe/                 # stat.ripe.net 客户端 + Redis 缓存
│       └── store/                # Postgres + 内存存储
├── web/                          # 前端 SPA
├── deploy/                       # Dockerfile + docker-compose + nginx
├── config/engine.yaml            # 可选的运行时参数（非必须）
└── .env.example
```

### 配置项

后端完全由环境变量驱动，每个变量都有默认值，最少仅需设置 `PROBE_MODE=mock` 即可启动。

| 变量                   | 默认值                                 | 作用                                                            |
| ---------------------- | -------------------------------------- | --------------------------------------------------------------- |
| `HTTP_ADDR`            | `:8080`                                | HTTP 监听地址                                                   |
| `PROBE_MODE`           | `pcap`                                 | `pcap`（真实追踪）或 `mock`（合成）                             |
| `PROBE_INTERFACE`      | 自动                                   | libpcap 抓包网卡，如 `en0` / `eth0`                             |
| `WORKER_CONCURRENCY`   | `32`                                   | 并发 Worker 数量                                                |
| `MAX_TTL`              | `40`                                   | Traceroute 最大 TTL                                             |
| `DOH_ENDPOINT`         | `https://cloudflare-dns.com/dns-query` | DoH（JSON 格式）地址                                            |
| `DOH_BOOTSTRAP_IPS`    | `1.1.1.1,1.0.0.1`                      | 用于连接 DoH 主机的 bootstrap IP                                |
| `DATABASE_URL`         | 空 → 内存存储                          | `postgres://user:pass@host:5432/db?sslmode=disable`             |
| `REDIS_URL`            | 空 → 内存缓存                          | `redis://host:6379/0`                                           |
| `MMDB_CITY_PATH`       | 空                                     | `GeoLite2-City.mmdb` 路径                                       |
| `MMDB_ASN_PATH`        | 空                                     | `GeoLite2-ASN.mmdb` 路径                                        |
| `RIPE_CACHE_TTL_HOURS` | `24`                                   | RIPE 响应缓存 TTL                                               |

仅 compose 模式需要：`POSTGRES_USER`、`POSTGRES_PASSWORD`、`POSTGRES_DB`（默认均为 `pbr`）。

### API 速查

Base URL：`http://localhost:8080/api/v1`（直连）或 `http://localhost:8081/api/v1`（经 nginx）。

- `POST /probes`：创建任务。body 支持 `domains`、`surge_url`、`surge_inline` 任意组合；返回 `202 Accepted` + `ProbeJob`。
- `GET /probes/{id}`：任务状态，包含 `status`、`processed/total`、`counts`、`last_error`。
- `GET /probes/{id}/results`：完整 trace 结果（含每跳 GeoIP/ASN、RTT、总 RTT、AS 序列）。
- `GET /results/latest`：仪表盘用的最优规则列表。
- `GET /ripe/{resource}`：RIPE 查询（命中 Redis 缓存时 `cached:true`）。
- `GET /delivery/surge/optimized.list`：Surge RULE-SET，支持 `ETag`/`304`。
- `GET /delivery/xray/optimized.json`：Xray routing JSON，支持 `ETag`/`304`。

### 探测模式

- **Mock 模式**：[backend/cmd/server/main.go](backend/cmd/server/main.go) 内置的确定性合成探测器，按目标 IP 分桶生成“国内→海外”的典型路径，无需任何权限，适合本机演示 / CI。
- **Pcap 模式**：基于 libpcap 的真实 ICMP 追踪。Docker 中需要 `NET_RAW` + `NET_ADMIN` 权限和宿主网络。在 [deploy/docker-compose.yaml](deploy/docker-compose.yaml) 中取消以下两行的注释：
  ```yaml
  # network_mode: host
  # cap_add: [NET_RAW, NET_ADMIN]
  ```
  启用前请将 `GeoLite2-City.mmdb` 与 `GeoLite2-ASN.mmdb` 放到 `config/geoip/`，以便富化 ASN/城市信息。

### 分类逻辑

位于 [backend/internal/classifier/classifier.go](backend/internal/classifier/classifier.go)：

1. **IEPL_Direct**：AS 路径完整落在 IEPL 白名单（`4134`、`4809`、`58453`）内，且跨境最后一跳的 RTT 增量低于 `ieplDeltaUpperBoundMS`（默认 3 ms）。用于识别短距离国际专线。
2. **CN2_Premium**：路径中任意位置经过 AS4809。
3. **CERNET_Detour**：路径中任意位置经过 AS4538。
4. **Blocked**：解析失败或响应跳数少于 2。
5. **Standard_163**：上述条件都不满足时的兜底（通常为普通 ChinaNet 出口）。

**只有 `IEPL_Direct` 与 `CN2_Premium` 的域名会被写入 Surge/Xray 优化订阅**。

### 前端仪表盘

打开 `http://localhost:8081` 后：

- **左侧**：三种模式的提交表单、任务进度条、分类环形图、订阅地址面板。
- **右侧**：Leaflet 世界地图（绘制所选域名的跳数折线）、结果表（点击一行切换地图）、跳详情表、RIPE 查询面板。
- **轮询**：任务运行期间每 2 s 拉取一次 `/probes/{id}` 与 `/probes/{id}/results`；完成后自动刷新 `/results/latest`。

前端通过 nginx 反代访问后端：所有 `/api/*` 请求都转发到 compose 网络内的 `pbr_backend:8080`。

### 本地开发（不走 Docker）

```bash
# 后端
cd backend
go mod download
PROBE_MODE=mock go run ./cmd/server

# 前端
cd web
npm install
npm run dev   # Vite dev server 监听 5173，自动代理 /api → 8080
```

若后端跑在其它地址，启动前端前设置 `BACKEND_URL=http://your-host:8080`。

### 从 CLI 批量提交域名

```bash
curl -s -X POST http://localhost:8080/api/v1/probes \
  -H 'content-type: application/json' \
  -d "$(jq -nc --argjson d "$(jq -R . < list.txt | jq -s .)" '{domains:$d}')"
```

### 常见问题排查

| 现象                                  | 可能原因 / 处理                                                                                     |
| ------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `go: go.mod requires go >= 1.24`      | 重新构建后端镜像，Dockerfile 已固定 `golang:1.24-bookworm`。                                        |
| 全部结果被判为 `Blocked`              | DoH 端点不支持 JSON 格式，使用默认的 `https://cloudflare-dns.com/dns-query` 或 Google `/resolve`。  |
| 容器内 `pcap: device ... not found`   | 改用 `PROBE_MODE=mock`，或启用 `network_mode: host` + `NET_RAW/NET_ADMIN`。                         |
| RIPE 查询总是 `cached:false`          | `REDIS_URL` 未配置或不可达，代码自动降级为内存缓存（重启失效）。                                    |
| 仪表盘显示 “0 optimized rules”        | 所有 trace 都落在 `Standard_163` / `CERNET_Detour` / `Blocked`；只有 IEPL/CN2 的域名才会进入订阅。  |
| `HEAD /api/v1/delivery/...` 返回 405  | 符合预期，交付端点只注册了 `GET`。真实客户端都会使用 `GET`。                                        |

### 路线图

- 持续历史视图：按域名查看状态随时间的变化。
- 基于缓存的 RIPE 数据做 BGP community 级别的更细分类。
- IPv6 探测（当前解析器仅实现 `LookupIPv4`）。
- API 鉴权与多租户订阅。

### 许可证

MIT。各 package 的依赖归属详见源码顶部注释。

