# Acceptance Evidence

本目录保存提交`f980ecd408af15bb2733809110dae3e092aca14c`在
2026-07-28执行正式验收时产生的原始结果。

## Run IDs

| 验收项 | Run ID | 主要结果 |
|---|---|---|
| API冷启动 | `20260728T074500Z-305453` | 5次全部通过，最大770ms |
| 查询性能 | `20260728T074537Z-306694` | 65,772次请求，P95 9.228ms，0错误 |
| Compose E2E | `20260728T074707Z-307572` | 采集、去重、恢复、持久化和停机全部通过 |

## File Groups

- `*-startup.csv`：每次冷启动的开始、就绪和耗时。
- `*-startup-environment.txt`：启动测试镜像及Docker环境。
- `*-query-performance.json`：数据准备、请求量、延迟和阈值判断。
- `*-environment.txt`：查询性能测试环境。
- `*-query-performance.stderr.log`：性能工具运行阶段信息。
- `*-compose-ps.txt`：E2E启动后的四服务状态。
- `*-at001-*`：初始1000条采集和幂等重放。
- `*-at002-*`：API中断、Logstash持久队列和200条恢复。
- `*-at003-*`：Filebeat重建后继续采集。
- `*-at005-*`：API重建后的SQLite持久化。
- `*-at009-*`：SIGTERM优雅退出和重启后验证。
- `*-at010-*`：request ID响应头、响应体和API日志关联。
- `f980ecd-coverage.out`：正式Race Detector运行生成的Go覆盖率profile。

部分`*.stderr.log`文件为空，表示对应Go验证程序没有输出错误，不是证据缺失。

## Reproduction

```bash
./scripts/acceptance/startup.sh
./scripts/acceptance/query-performance.sh
./scripts/acceptance/e2e.sh
```

重新运行会生成新的Run ID和结果文件。完整验收口径、环境和限制见：

- [Final Acceptance Test Report](../../docs/test-report.md)
- [Performance Test Report](../../docs/performance-report.md)
- [Project Retrospective](../../docs/retrospective.md)
