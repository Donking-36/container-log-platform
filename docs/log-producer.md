# 日志生产器

`log-producer`是端到端采集链路的标准日志来源。它只负责生成可预测、可编号的日志，不访问日志接收API，也不直接写入SQLite。

## 输出规则

生产器以“周期”为单位工作：

- 每个周期向stdout写入1条`INFO`日志。
- 每5个周期向stderr写入1条`ERROR`日志。
- 每个周期向NDJSON文件追加1条`WARN`日志。

因此，运行100个周期会生成：

| 来源 | 数量 |
|---|---:|
| stdout | 100 |
| stderr | 20 |
| NDJSON文件 | 100 |

stdout、stderr和文件中的每一行都是一个完整JSON对象。生产器不会在正常运行时向这些输出写入banner等额外文本。

## 事件格式

```json
{
  "source_event_id": "acceptance-run-stdout-000001",
  "sequence": 1,
  "level": "INFO",
  "message": "stdout event 000001",
  "logged_at": "2026-07-27T08:00:00.123456789Z"
}
```

规则：

- `source_event_id`同时包含运行ID、来源类型和来源内序号。
- stdout、stderr和文件分别拥有连续序号。
- `logged_at`使用UTC RFC3339Nano格式。
- 同一个显式运行ID会生成相同的来源ID，可用于重放和幂等测试。
- 未指定运行ID时，每次启动都会生成新的UUID，避免正常重启产生ID冲突。

Filebeat接入时会把解析后的原始字段放入`producer.*`命名空间。stdout和stderr来源由Docker日志的`stream`元数据判断，文件来源由Filebeat文件输入固定标记，不能根据日志级别猜测来源。

## 配置

| 环境变量 | 默认值 | 规则 |
|---|---|---|
| `PRODUCER_RUN_ID` | 自动生成`run-<uuid>` | 可选；不能包含空白字符 |
| `PRODUCER_LOG_PATH` | `./data/producer/events.ndjson` | NDJSON追加写入路径 |
| `PRODUCER_INTERVAL` | `1s` | 每个周期的等待时间，必须大于0 |
| `PRODUCER_CYCLES` | `0` | `0`表示持续运行；正整数表示运行指定周期后退出 |

文件以追加模式打开，已有内容不会在生产器重启时被截断。

持续模式会不断增加NDJSON文件大小。后续Compose配置必须明确采用有限周期或文件轮转与保留策略；验收任务应优先设置正整数周期，避免无上限占用磁盘。

## 本地运行

下面的命令运行10个周期：

```bash
mkdir -p /tmp/log-producer-demo

PRODUCER_RUN_ID=local-demo \
PRODUCER_LOG_PATH=/tmp/log-producer-demo/file.ndjson \
PRODUCER_INTERVAL=10ms \
PRODUCER_CYCLES=10 \
go run ./cmd/log-producer \
  >/tmp/log-producer-demo/stdout.ndjson \
  2>/tmp/log-producer-demo/stderr.ndjson
```

检查数量：

```bash
wc -l \
  /tmp/log-producer-demo/stdout.ndjson \
  /tmp/log-producer-demo/stderr.ndjson \
  /tmp/log-producer-demo/file.ndjson
```

预期分别为10、2、10。

查看事件：

```bash
head -n 1 /tmp/log-producer-demo/*.ndjson
```

## 与后续采集链路的边界

`log-producer`不负责：

- 调用Gin内部日志接收API。
- 添加容器ID和容器名称。
- 保存Filebeat读取偏移量。
- 重试Filebeat或Logstash传输。

这些职责分别由Filebeat、Logstash和API承担。
