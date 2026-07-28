# Final Acceptance Test Report

> 验收日期：2026-07-28  
> 被测提交：`f980ecd408af15bb2733809110dae3e092aca14c`  
> 验收环境：WSL2 Ubuntu 24.04.4 LTS + Docker Engine  
> 最终结论：通过

## 1. 验收范围

本次验收覆盖容器化日志收集平台完整MVP链路：

```text
log-producer
→ Filebeat
→ Logstash
→ Gin Internal API
→ GORM
→ SQLite
→ Gin Public API
```

验收内容包括：

- Go依赖、格式、静态检查、单元和集成测试。
- 竞态检测和核心包覆盖率。
- API冷启动耗时。
- 10,000条日志下的组合查询性能。
- stdout、stderr和NDJSON文件日志端到端采集。
- 相同来源事件的幂等去重。
- API中断及Logstash持久队列恢复。
- Filebeat和API容器重建恢复。
- SQLite数据持久化。
- request ID在响应头、响应体和API日志中的一致性。
- API收到`SIGTERM`后的优雅退出。

## 2. 验收环境

| 项目 | 实际值 |
|---|---|
| Windows | `10.0.26100.1742` |
| WSL | `2.7.10.0` |
| Linux发行版 | Ubuntu 24.04.4 LTS |
| WSL内核 | `6.18.33.2-microsoft-standard-WSL2` |
| CPU | 12th Gen Intel Core i7-12700，WSL可见20个逻辑CPU |
| WSL内存 | 7,932,584 kB，约7.6 GiB |
| Go | `go1.26.5 linux/amd64` |
| Docker Engine | `29.6.2` |
| Docker Compose | `v5.3.1` |
| Filebeat | `9.4.2` |
| Logstash | `9.4.2` |

启动和查询性能的完整环境输出分别保存在：

- [startup environment](../artifacts/acceptance/20260728T074500Z-305453-startup-environment.txt)
- [query performance environment](../artifacts/acceptance/20260728T074537Z-306694-environment.txt)

## 3. 质量门禁

执行命令：

```bash
go mod verify
gofmt -l .
go vet ./...
CGO_ENABLED=1 go test -race \
  -coverprofile=artifacts/acceptance/f980ecd-coverage.out \
  ./...
go build ./cmd/api ./cmd/log-producer
```

结果：

| 检查 | 结果 |
|---|---|
| Go Module依赖校验 | 通过 |
| Go格式检查 | 通过，无待格式化文件 |
| `go vet ./...` | 通过 |
| 全量测试 | 通过 |
| Race Detector | 通过，未发现数据竞争 |
| API与log-producer构建 | 通过 |

核心业务包覆盖率：

| 包 | 覆盖率 |
|---|---:|
| `internal/config` | 88.0% |
| `internal/handler` | 93.4% |
| `internal/ingestion` | 88.4% |
| `internal/middleware` | 87.3% |
| `internal/repository` | 88.3% |
| `internal/server` | 93.3% |
| `internal/service` | 97.1% |

核心业务包均达到80%的目标。全仓加权覆盖率为61.0%；该口径还包含
`cmd/api`入口、只有简单表名方法的`internal/model`，以及通过`go run`执行的
E2E和性能工具，因此不用于代替核心包覆盖率验收。

原始覆盖率数据见
[f980ecd-coverage.out](../artifacts/acceptance/f980ecd-coverage.out)。

## 4. 启动时间验收

验收标准：

- 使用已构建镜像，不计镜像拉取和构建时间。
- 每次使用全新的SQLite目录。
- 连续运行5次。
- API从容器启动到`/readyz`成功不超过3000ms。

| 次数 | 耗时 | 结果 |
|---:|---:|---|
| 1 | 770ms | 通过 |
| 2 | 470ms | 通过 |
| 3 | 513ms | 通过 |
| 4 | 452ms | 通过 |
| 5 | 470ms | 通过 |

平均耗时535ms，中位数470ms，最慢770ms。最慢结果仅占3000ms门槛的
25.7%。

原始结果见
[startup.csv](../artifacts/acceptance/20260728T074500Z-305453-startup.csv)。

## 5. 查询性能验收

验收参数：

```text
数据量：10,000条
并发量：10
持续时间：30秒
查询条件：container + level + start + end
阈值：p95 <= 500ms，错误率 = 0%
```

关键结果：

| 指标 | 结果 |
|---|---:|
| 总请求数 | 65,772 |
| 成功请求数 | 65,772 |
| 错误数 | 0 |
| 吞吐量 | 2,192.10 requests/s |
| P50 | 4.048ms |
| P95 | 9.228ms |
| P99 | 12.643ms |
| 最大耗时 | 31.856ms |

查询性能通过。详细方法、结果和限制见
[Performance Test Report](performance-report.md)，原始JSON见
[query-performance.json](../artifacts/acceptance/20260728T074537Z-306694-query-performance.json)。

## 6. Compose端到端验收

正式运行ID：

```text
20260728T074707Z-307572
```

测试使用独立Compose项目、独立命名卷和临时SQLite目录。验收结束后，临时
容器、网络、命名卷和SQLite目录均已清理。

### 6.1 一键启动与初始采集

四个服务均通过健康检查。生成并验证1000条验收事件：

| 来源 | 预期 | 实际 |
|---|---:|---:|
| stdout | 400 | 400 |
| stderr | 100 | 100 |
| file | 500 | 500 |
| 合计 | 1000 | 1000 |

唯一`event_id`数量为1000，字段、来源、级别、分页和排序验证均通过。

证据：

- [Compose服务状态](../artifacts/acceptance/20260728T074707Z-307572-compose-ps.txt)
- [初始E2E结果](../artifacts/acceptance/20260728T074707Z-307572-at001-e2e.json)

### 6.2 幂等重放

重新发送相同的1000个`source_event_id`：

```text
duplicated_events=1000
```

数据库中的验收事件仍为1000条，唯一事件仍为1000条，没有产生重复存储。

证据：

- [幂等计数](../artifacts/acceptance/20260728T074707Z-307572-at001-idempotency.txt)
- [重放后验证](../artifacts/acceptance/20260728T074707Z-307572-at001-idempotency.json)

### 6.3 API中断与Logstash持久队列

停止API后生成200条事件：

```text
events_in_before=2050
events_in_after=2250
queue_events_before_recreate=150
```

`events.in`增加200，说明Logstash接收了全部故障期事件。队列瞬时指标为150，
其余50条位于正在处理但尚未确认的批次中；因此不能只使用单个
`queue.events`数值判断总接收量。

重新创建Logstash后：

```text
events_in_after_recreate=0
queue_after_recreate=150
events_out_after_recovery=200
queue_after_recovery=0
```

新Logstash进程没有重新接收来源事件，却成功输出200条；公开API最终查询到
1200条唯一验收事件，证明故障期的200条全部恢复。

证据：

- [队列指标](../artifacts/acceptance/20260728T074707Z-307572-at002-queue.txt)
- [下游恢复结果](../artifacts/acceptance/20260728T074707Z-307572-at002-downstream-recovery.json)

### 6.4 Filebeat和API重建

重建Filebeat后再生成100条事件，验收总数变为1300，唯一事件数仍为1300。

随后重新创建API容器，查询结果仍为1300，证明SQLite绑定挂载在容器重建后
继续保留数据。

证据：

- [Filebeat恢复](../artifacts/acceptance/20260728T074707Z-307572-at003-filebeat-recovery.json)
- [SQLite持久化](../artifacts/acceptance/20260728T074707Z-307572-at005-sqlite-persistence.json)

### 6.5 Request ID

使用固定请求ID发送非法分页参数，三个位置均出现：

```text
acceptance-20260728t074707z-307572-request
```

对应结果：

- HTTP状态码：400
- 响应错误码：`INVALID_ARGUMENT`
- 响应头、JSON响应体、API结构化日志中的request ID完全一致

证据：

- [响应头](../artifacts/acceptance/20260728T074707Z-307572-at010-headers.txt)
- [响应体](../artifacts/acceptance/20260728T074707Z-307572-at010-body.json)
- [API日志](../artifacts/acceptance/20260728T074707Z-307572-at010-api.log)

### 6.6 优雅退出

在持续查询期间向API发送`SIGTERM`：

```text
api_exit_code=0
```

重新启动API后，1300条验收事件仍然完整且唯一，说明HTTP Server和SQLite
按照预期完成了优雅关闭。

证据：

- [退出码](../artifacts/acceptance/20260728T074707Z-307572-at009-graceful-shutdown.txt)
- [重启后结果](../artifacts/acceptance/20260728T074707Z-307572-at009-after-graceful-shutdown.json)

## 7. 验收口径和限制

- 1000、1200和1300是带本次验收前缀的事件精确数量，不是SQLite整表总数。
  Compose启动时的普通log-producer可能产生少量非验收前缀日志。
- 无丢失结论限定在本次200条故障期事件、当前队列容量和等待时间内，不表示
  可以承受无限期下游故障。
- 性能结果来自本机WSL2回环网络和单机SQLite，不能直接推算为生产集群容量。
- Filebeat需要只读挂载Docker socket和容器日志目录，该权限边界已经记录，
  但MVP没有引入独立Docker socket代理。

## 8. 最终结论

三个核心用例、全部量化指标和核心故障恢复场景均通过。当前代码满足完整MVP
Definition of Done中的实现与验收要求，可以合并回`develop`并进入
`release/v1.0.0`发布流程。

Git Flow发布、`main`合并和`v1.0.0`标签尚未执行，因此项目当前状态是
“验收通过，待发布”，而不是“已经发布”。
