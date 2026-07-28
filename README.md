# Container Log Platform

基于 Go、Gin、GORM、SQLite、Filebeat、Logstash 和 Docker Compose 的容器化日志收集平台。

## 当前状态

`v0.1.0`日志接收核心、`v0.2.0`查询与统计能力以及`v0.3.0`自动采集与
Compose编排已经发布。完整MVP工程验收已经完成，当前准备`v1.0.0`正式交付。

当前已完成：

- 需求、用例、架构和关键 ADR
- Go Module 与 API 程序入口
- 环境变量配置、默认值和启动校验
- Public/Internal 双 HTTP Server
- 健康检查与就绪检查基础路由
- HTTP Server 优雅停机
- SQLite数据层、迁移、索引和幂等批量写入
- 日志事件校验、规范化和稳定`event_id`
- 单条与批量内部日志接收API
- request ID、结构化请求日志、统一JSON Recovery
- 内部接收接口的readiness门禁和503响应
- 日志列表组合过滤、稳定排序和分页
- 单条日志详情查询
- 公开查询接口的readiness门禁和统一错误响应
- 按日志级别聚合数量与占比
- 按服务聚合日志数量
- 统计接口的组合过滤、稳定排序和空结果处理
- 可配置、可编号并输出三类日志的 `log-producer`
- Filebeat容器自动发现、文件采集、registry和磁盘队列
- Logstash字段转换、非法事件隔离、HTTP批量输出和持久队列
- API与日志生产器的非root多阶段镜像
- 四服务Compose编排、健康检查、网络隔离和持久化挂载
- Go 单元测试与基础 CI
- 可重复执行的冷启动、查询性能和Compose端到端验收工具
- API中断、Logstash持久队列、Filebeat/API重建和SQLite持久化验证
- request ID关联、Race Detector和SIGTERM优雅退出验证

完整MVP工程验收已于2026-07-28在提交`f980ecd`上通过，结果见
[最终验收报告](docs/test-report.md)和[性能报告](docs/performance-report.md)。
当前尚未创建`v1.0.0`标签；验收结果将作为`v1.0.0`正式发布的依据。

| 验收项 | 实际结果 | 门槛 |
|---|---:|---:|
| API冷启动 | 5/5通过，最大770ms | <=3000ms |
| 组合查询 | P95 9.228ms，2192.10 RPS | P95 <=500ms |
| 查询错误 | 0 / 65,772 | 0 |
| E2E与恢复 | 1000 → 去重1000 → PQ恢复1200 → 重建后1300 | 缺失0、重复存储0 |

## 数据链路

```text
log-producer
→ Filebeat
→ Logstash
→ Gin Internal API
→ GORM
→ SQLite
→ Gin Public API
```

## 环境要求

- Go 1.26.5
- Git
- Docker Engine
- Docker Compose

## 本地启动

下载依赖：

```bash
go mod download
```

使用默认配置启动：

```bash
go run ./cmd/api
```

默认监听：

```text
Public Server   :8080
Internal Server :8081
```

也可以临时覆盖配置：

```bash
PUBLIC_ADDR=:18080 \
INTERNAL_ADDR=:18081 \
go run ./cmd/api
```

如需使用 `.env` 文件：

```bash
cp .env.example .env

set -a
source .env
set +a

go run ./cmd/api
```

程序本身不会自动解析 `.env`，上面的 `source` 命令负责把配置导入当前 Shell 环境。

## Docker Compose一键启动

首次启动前准备配置：

```bash
cp .env.example .env
```

默认容器用户的UID/GID均为`1000`。如果`id -u`或`id -g`不是`1000`，
请同步修改`.env`中的`APP_UID`和`APP_GID`。

校验并启动完整日志链路：

```bash
docker compose config --quiet
docker compose up -d --build --wait --wait-timeout 180
docker compose ps
```

验证公开API和自动采集结果：

```bash
curl --noproxy '*' -fsS http://127.0.0.1:8080/healthz
curl --noproxy '*' -fsS http://127.0.0.1:8080/readyz

curl --noproxy '*' -fsS \
  'http://127.0.0.1:8080/api/v1/logs?service=log-producer&page_size=20'
```

以上命令使用默认宿主端口8080；修改`API_PORT`后，请同步替换URL中的端口。

宿主机只发布公开API端口。停止平台时使用：

```bash
docker compose down
```

该命令保留SQLite、Filebeat registry、Logstash持久队列和生产器文件日志。
不要把`docker compose down -v`作为普通停止命令。

完整说明见[Docker Compose部署说明](docs/deployment.md)。

## 当前接口

| Server | 接口 | 当前行为 |
|---|---|---|
| Public `:8080` | `GET /healthz` | 返回 HTTP 200，表示进程存活 |
| Public `:8080` | `GET /readyz` | SQLite可访问且服务正在接收流量时返回HTTP 200，否则返回HTTP 503 |
| Public `:8080` | `GET /api/v1/logs` | 按容器、服务、级别和时间范围组合查询，支持分页 |
| Public `:8080` | `GET /api/v1/logs/:id` | 按SQLite内部ID查询完整日志详情 |
| Public `:8080` | `GET /api/v1/stats/levels` | 按日志级别统计数量和占比 |
| Public `:8080` | `GET /api/v1/stats/services` | 按服务统计日志数量 |
| Internal `:8081` | `POST /internal/v1/logs` | 校验、规范化并幂等接收单条日志 |
| Internal `:8081` | `POST /internal/v1/logs/bulk` | 在一个批次中处理插入、重复和永久拒绝事件 |

本地验证：

```bash
curl --noproxy '*' -i http://127.0.0.1:8080/healthz
curl --noproxy '*' -i http://127.0.0.1:8080/readyz

curl --noproxy '*' -i \
  -H 'Content-Type: application/json' \
  -d '{
    "source_event_id": "local-example-1",
    "agent_id": "local-agent",
    "container_name": "local-producer",
    "container_id": "local-container",
    "service": "log-producer",
    "level": "INFO",
    "message": "hello from local test",
    "source": "stdout",
    "logged_at": "2026-07-24T10:00:00Z"
  }' \
  http://127.0.0.1:8081/internal/v1/logs

curl --noproxy '*' \
  'http://127.0.0.1:8080/api/v1/logs?container=local-producer&level=info&page=1&page_size=20'

curl --noproxy '*' \
  http://127.0.0.1:8080/api/v1/logs/1

curl --noproxy '*' \
  'http://127.0.0.1:8080/api/v1/stats/levels?container=local-producer'

curl --noproxy '*' \
  'http://127.0.0.1:8080/api/v1/stats/services?container=local-producer'
```

完整参数和响应字段见[REST API 使用说明](docs/api.md)。

## 质量检查

```bash
gofmt -w .
go vet ./...
go test ./...
```

CI 还会执行竞态检测和 API 构建。

正式验收：

```bash
./scripts/acceptance/startup.sh
./scripts/acceptance/query-performance.sh
./scripts/acceptance/e2e.sh
```

验收脚本使用独立端口、临时SQLite目录和独立Compose资源。原始结果保存在
`artifacts/acceptance/`。

## 目录结构

```text
cmd/api/          API程序入口
cmd/log-producer/ 标准日志生产器
deploy/filebeat/  Filebeat采集配置
deploy/logstash/  Logstash配置、pipeline和转换脚本
internal/config/  应用配置
internal/handler/ HTTP解析、状态码和响应DTO
internal/ingestion/ 日志事件规范化与幂等ID
internal/middleware/ request ID等HTTP中间件
internal/repository/ SQLite持久化
internal/server/  Gin Router与HTTP Server生命周期
internal/service/ 日志接收业务规则
scripts/acceptance/ 启动、性能与Compose端到端验收脚本
test/e2e/        E2E公开API验证程序
test/performance/ 查询压力与结果计算程序
artifacts/acceptance/ 正式验收原始证据
data/             SQLite持久化目录
docs/             需求、用例、架构与ADR
Dockerfile        API和日志生产器的多阶段构建
compose.yaml      四服务编排、网络与持久化资源
```

目录只在产生真实代码时创建，不预先提交大量空包。

## 项目文档

- [实施方案](docs/implementation-plan.md)
- [需求规格](docs/requirements.md)
- [系统架构](docs/architecture.md)
- [REST API 使用说明](docs/api.md)
- [日志生产器说明](docs/log-producer.md)
- [Filebeat采集配置](docs/filebeat.md)
- [Logstash传输配置](docs/logstash.md)
- [Docker Compose部署说明](docs/deployment.md)
- [最终验收报告](docs/test-report.md)
- [性能测试报告](docs/performance-report.md)
- [项目复盘](docs/retrospective.md)
- [版本路线图](docs/release-roadmap.md)
- [架构决策记录](docs/adr/)
- [核心用例](docs/use-cases/)

## Git Flow

- `main`：正式发布版本
- `develop`：集成分支
- `feature/*`：功能开发分支
- `release/*`：发布准备分支
- `hotfix/*`：正式版本紧急修复
