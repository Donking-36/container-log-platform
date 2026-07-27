# Container Log Platform

基于 Go、Gin、GORM、SQLite、Filebeat、Logstash 和 Docker Compose 的容器化日志收集平台。

## 当前状态

`v0.1.0` 日志接收核心和 `v0.2.0` 查询与统计能力已经发布。下一步将进入 `v0.3.0` 自动采集与 Compose 开发。

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
- Go 单元测试与基础 CI

查询与统计 API 已完成；Filebeat、Logstash 和 Compose 将在 `v0.3.0` 实现。

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
- 后续容器化阶段需要 Docker Engine 和 Docker Compose

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
curl --noproxy '*' -i http://127.0.0.1:8081/healthz

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

## 目录结构

```text
cmd/api/          API程序入口
internal/config/  应用配置
internal/handler/ HTTP解析、状态码和响应DTO
internal/ingestion/ 日志事件规范化与幂等ID
internal/middleware/ request ID等HTTP中间件
internal/repository/ SQLite持久化
internal/server/  Gin Router与HTTP Server生命周期
internal/service/ 日志接收业务规则
data/             SQLite持久化目录
docs/             需求、用例、架构与ADR
```

目录只在产生真实代码时创建，不预先提交大量空包。

## 项目文档

- [实施方案](docs/implementation-plan.md)
- [需求规格](docs/requirements.md)
- [系统架构](docs/architecture.md)
- [REST API 使用说明](docs/api.md)
- [版本路线图](docs/release-roadmap.md)
- [架构决策记录](docs/adr/)
- [核心用例](docs/use-cases/)

## Git Flow

- `main`：正式发布版本
- `develop`：集成分支
- `feature/*`：功能开发分支
- `release/*`：发布准备分支
- `hotfix/*`：正式版本紧急修复
