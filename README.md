# Container Log Platform

基于 Go、Gin、GORM、SQLite、Filebeat、Logstash 和 Docker Compose 的容器化日志收集平台。

## 当前状态

正在开发 v0.1.0。

当前已完成：

- 需求、用例、架构和关键 ADR
- Go Module 与 API 程序入口
- 环境变量配置、默认值和启动校验
- Public/Internal 双 HTTP Server
- 健康检查与就绪检查基础路由
- HTTP Server 优雅停机
- Go 单元测试与基础 CI

SQLite、日志接收、查询统计、Filebeat、Logstash 和 Compose 将在后续功能分支中实现。

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
| Public `:8080` | `GET /readyz` | 当前返回 HTTP 503，等待 SQLite 接入 |
| Internal `:8081` | 日志接收接口 | 尚未实现 |

本地验证：

```bash
curl --noproxy '*' -i http://127.0.0.1:8080/healthz
curl --noproxy '*' -i http://127.0.0.1:8080/readyz
curl --noproxy '*' -i http://127.0.0.1:8081/healthz
```

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
internal/server/  Gin Router与HTTP Server生命周期
data/             SQLite持久化目录
docs/             需求、用例、架构与ADR
```

目录只在产生真实代码时创建，不预先提交大量空包。

## 项目文档

- [实施方案](docs/implementation-plan.md)
- [需求规格](docs/requirements.md)
- [系统架构](docs/architecture.md)
- [架构决策记录](docs/adr/)
- [核心用例](docs/use-cases/)

## Git Flow

- `main`：正式发布版本
- `develop`：集成分支
- `feature/*`：功能开发分支
- `release/*`：发布准备分支
- `hotfix/*`：正式版本紧急修复
