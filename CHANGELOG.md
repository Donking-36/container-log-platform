# 更新日志

本文件记录项目各版本的重要变更。

## [Unreleased]

### 新增

- 增加可配置的`log-producer`，支持stdout、stderr和NDJSON文件输出
- 为生产器事件增加可重复核对的来源ID、序号、级别和UTC发生时间
- 增加生产器配置、输出数量、事件唯一性和文件追加测试
- 增加Filebeat容器自动发现、文件采集、registry和磁盘队列
- 增加Logstash Beats输入、字段转换、HTTP批量输出和持久队列
- 增加API与log-producer非root多阶段镜像
- 增加四服务Compose编排、健康检查、内部网络和持久化挂载
- 增加API冷启动、查询性能和Compose端到端验收工具
- 增加正式验收、性能测试和项目复盘文档

### 验证

- 验证stdout、stderr和文件日志的完整采集、分类与字段映射
- 验证1000条事件幂等重放后不产生重复存储
- 验证API中断期间200条事件通过Logstash持久队列全部恢复
- 验证Filebeat和API重建后继续采集且SQLite数据不丢失
- 验证API冷启动5次均低于3秒
- 验证10,000条数据、10并发、30秒查询P95为9.228ms且零错误
- 验证request ID跨响应头、响应体和结构化日志关联
- 验证API收到SIGTERM后以退出码0完成优雅退出

## [0.2.0] - 2026-07-27

### 新增

- 实现`GET /api/v1/logs`日志组合查询、稳定排序和分页
- 实现`GET /api/v1/logs/:id`日志详情查询
- 实现`GET /api/v1/stats/levels`日志级别数量与占比统计
- 实现`GET /api/v1/stats/services`服务日志数量统计
- 使用SQLite `GROUP BY`完成统计聚合和稳定排序
- 增加公开查询路由的readiness门禁
- 增加查询参数、业务规则、Repository和HTTP集成测试
- 增加统计Repository、Service、Handler和端到端集成测试
- 为结构化HTTP请求日志增加直接连接方`client_ip`

### 安全与兼容性

- 列表响应不返回`raw_event`
- 详情响应将`raw_event`作为JSON值返回，避免二次编码
- 拒绝未知、重复和格式错误的查询参数
- SQLite `BUSY/LOCKED`查询错误返回HTTP 503，其他内部错误返回HTTP 500

## [0.1.0] - 2026-07-24

### 新增

- 建立 Go、Gin、GORM 和 SQLite 项目基础结构
- 实现 Public 与 Internal 双 HTTP Server
- 实现健康检查、动态就绪检查和优雅停机
- 实现 SQLite 数据迁移、索引和日志持久化
- 实现日志事件校验、规范化和稳定 `event_id`
- 实现单条及批量日志幂等接收接口
- 实现并发重复事件处理
- 增加 Request ID、结构化请求日志和统一 JSON Recovery
- 增加请求体大小、批次大小和日志正文大小限制
- 将 SQLite `BUSY/LOCKED` 临时故障映射为 HTTP 503

### 接口

- `GET /healthz`
- `GET /readyz`
- `POST /internal/v1/logs`
- `POST /internal/v1/logs/bulk`

### 已知限制

- 尚未实现日志查询与统计 API
- 尚未接入 Filebeat 和 Logstash
- 尚未提供完整的 Docker Compose 部署链路

[Unreleased]: https://github.com/Donking-36/container-log-platform/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/Donking-36/container-log-platform/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/Donking-36/container-log-platform/releases/tag/v0.1.0
