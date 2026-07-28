# Logstash传输配置

Logstash负责接收Filebeat事件、转换字段，并批量调用Gin内部接收API。它不采集原始日志、不直接写入SQLite，也不提供日志查询接口。

## 固定版本

首个兼容性基线固定为：

```text
docker.elastic.co/logstash/logstash:9.4.2
```

禁止使用`latest`。Logstash与Filebeat使用相同的`9.4.2`版本线，减少Beats协议和ECS字段兼容性问题。

## 处理链路

```text
Filebeat
→ Beats input :5044
→ 非法JSON判断
→ Ruby字段转换
→ HTTP json_batch
→ POST /internal/v1/logs/bulk
→ Gin API
→ SQLite
```

Logstash只负责传输层的格式转换和故障缓冲。字段是否满足业务规则、重复事件是否写入等判断，仍由Gin API负责。

## Beats输入

Beats input监听：

```text
0.0.0.0:5044
```

配置使用：

```text
ecs_compatibility: v8
enrich: none
```

`ecs_compatibility: v8`与Filebeat事件的ECS版本保持一致。`enrich: none`避免加入本项目不需要的连接元数据。

Compose阶段不得把5044端口发布到宿主机，只允许同一内部网络中的Filebeat使用服务名`logstash:5044`访问。

## 事件转换

Ruby脚本`transform_event.rb`把Filebeat字段转换成内部接收API要求的结构：

| Filebeat字段 | API字段 | 说明 |
|---|---|---|
| `producer.source_event_id` | `source_event_id` | 生产器提供的来源事件ID |
| `agent.id` | `agent_id` | Filebeat实例ID |
| `container.name` | `container_name` | 容器名称 |
| `container.id` | `container_id` | 容器ID |
| `service.name` | `service` | 服务名称 |
| `producer.level` | `level` | 日志级别 |
| `producer.message`，缺失时使用根字段`message` | `message` | 日志正文 |
| `platform.input_kind`或`stream` | `source` | `file`、`stdout`或`stderr` |
| `log.file.path` | `log_path` | 原始日志文件路径 |
| `log.offset` | `log_offset` | 文件偏移量 |
| `producer.logged_at` | `logged_at` | 生产器记录时间 |
| 转换前的事件快照 | `raw_event` | 保留排障所需的原始字段 |

来源字段的规则是：

- `platform.input_kind`为`file`时，固定得到`source: "file"`。
- 其他输入使用Docker日志的`stream`，得到`stdout`或`stderr`。
- 不根据`level`猜测日志来自stdout还是stderr。

脚本先保存`raw_event`，再清空原来的根字段并只写入API字段。这样可以避免把`@timestamp`、`producer`、`agent`等未知根字段发送给启用了`DisallowUnknownFields()`的Gin接口。

来源没有提供的可选字段值为`nil`，脚本不会发送这些字段。字段缺失是否合法仍由API统一判断。

脚本还会检查API字符串字段和`log_offset`的JSON类型。类型不兼容的事件会被标记为转换错误并单独拒绝，避免Gin解码整个批量数组时因一条坏事件返回HTTP 400，连带丢弃同批合法事件。

`raw_event`有助于排障，但会增加HTTP请求体和SQLite占用，也可能包含容器标签等敏感元数据。后续接入真实环境时需要评估脱敏和保留策略。

## 非法事件

### Filebeat无法解析NDJSON

Filebeat解析失败时会写入：

```text
producer.error.type: json
```

Logstash为事件增加`_invalid_producer_json`标签，不调用内部API，只向自身日志输出安全摘要：

```text
rejected event reason=invalid_producer_json log_path=... log_offset=...
```

摘要不重复输出原始日志正文，避免错误数据或敏感内容再次扩散。该事件被拒绝后，后面的合法日志仍可继续处理。

### Ruby转换异常

Ruby filter发生异常时会增加`_ingestion_transform_error`标签。该事件同样不调用内部API，只输出路径、偏移量和拒绝原因。

字段类型不兼容也会进入该分支，例如`source_event_id`为数字或`message`为对象。

缺少必填字段、来源字段不合法、时间格式错误或正文超限等业务错误不属于以上两类。它们会发送到Gin API，由API逐条校验，并计入批量响应的`rejected`总数。空或未知日志级别不会被拒绝，而是由API规范化为`UNKNOWN`。

## 批量HTTP输出

默认目标地址为：

```text
http://api:8081/internal/v1/logs/bulk
```

可以通过环境变量覆盖：

```text
INGESTION_API_URL
```

HTTP output使用以下行为：

| 配置 | 值 | 作用 |
|---|---:|---|
| 方法 | `POST` | 调用内部批量接收接口 |
| 格式 | `json_batch` | 把一个Logstash批次编码成JSON数组 |
| Content-Type | `application/json` | 与Gin接口约定一致 |
| `pipeline.batch.size` | `50` | 单次处理批次最多约50条 |
| `pool_max` | `1` | HTTP连接池总并发为1 |
| `pool_max_per_route` | `1` | 单目标并发为1 |
| `connect_timeout` | `5`秒 | 建立连接超时 |
| `socket_timeout` | `10`秒 | 等待网络读写超时 |
| `request_timeout` | `15`秒 | 单次请求总超时 |

单worker和单路由连接可以降低SQLite并发写入压力，也让首个版本的行为更容易观察。它不是高吞吐场景的最终参数。

API默认允许最多1000条事件和16MiB请求体，因此50条的数量上限与默认配置兼容。但`raw_event`当前没有独立大小上限，异常大的原事件仍可能使请求体超过16MiB并收到不可重试的413。Compose不得把API上限下调到与该批次设置冲突；后续还需要根据真实数据决定是否增加单事件裁剪或隔离策略。

即使当前批次只有一条日志，请求体仍然是数组：

```json
[
  {
    "source_event_id": "example-000001",
    "level": "INFO",
    "message": "hello"
  }
]
```

## 重试与幂等

HTTP客户端层先执行一次自动重试：

```text
automatic_retries: 1
```

插件层对以下HTTP状态持续重试：

```text
429, 500, 502, 503, 504
```

网络连接失败、DNS解析失败和超时等可恢复异常也会重试。因为请求方法是POST，所以配置启用了：

```text
retry_non_idempotent: true
```

`400`和`413`不在重试状态列表中。格式错误或请求体过大无法通过重复发送自动修复。

对于已经通过解析和转换、并在HTTP output中遇到可重试故障的事件，该链路采用“至少一次”投递：请求可能已被API处理，但响应在返回途中丢失，Logstash随后会重发同一批次。因此不能依赖Logstash保证只发送一次，必须由API使用`source_event_id`等稳定字段实现幂等。

还需要注意，Logstash HTTP output只根据HTTP状态判断请求是否成功，不读取响应正文中的`inserted`、`duplicated`或`rejected`。如果API返回HTTP 200但正文显示某些事件被业务拒绝，Logstash会把整个批次视为已完成，不会自动重投这些单条事件。

非法NDJSON、转换错误以及收到400或413的请求不属于上述至少一次保证范围。

## 持久化队列

Logstash启用持久化队列：

```yaml
queue.type: persisted
queue.max_bytes: 256mb
queue.checkpoint.writes: 1
queue.drain: false
```

PQ保存的是“Beats input已经接收，但尚未完成filter和output处理”的事件。对于正常合法事件，这通常就是“Filebeat已经交给Logstash，但尚未被API成功确认”的阶段：

```text
Filebeat磁盘队列
→ 保护尚未交给Logstash的事件

Logstash持久化队列
→ 保护尚未交给API的事件
```

`queue.checkpoint.writes: 1`要求每写入一个事件就生成检查点，偏向可靠性，但会增加磁盘写入并降低吞吐量。首个版本先选择更容易验证的可靠性配置，后续再根据压测结果调整。

`queue.drain: false`表示停止Logstash时不要求先排空全部队列。未完成事件保留到下次启动继续发送。若HTTP output正在无限重试，优雅停机仍可能等待较久，Docker最终可能在停止超时后终止进程。

队列上限为256MB，不是无限存储。队列满后，Logstash会向Filebeat传播反压，Filebeat可以继续使用自己的256MB磁盘队列缓冲；Filebeat队列也写满后，采集链路最终仍会阻塞。

Compose必须把Logstash的：

```text
/usr/share/logstash/data
```

挂载到命名卷。容器可写层中的PQ仍能应对同一容器的进程崩溃和停止后重启，但无法跨容器删除或重新创建恢复。PQ也不能防止宿主机磁盘损坏或整机丢失。

## 监控、端口和挂载

Logstash Node API监听：

```text
0.0.0.0:9600
```

它用于进程存活检查和读取pipeline、插件、队列指标。Node API可访问不代表下游Gin API可用、PQ已经清空或SQLite写入正常，不能把它单独当成端到端就绪检查。`monitoring.enabled: false`关闭向Elastic监控集群发送数据，不会关闭本地Node API。

计划中的Compose边界如下：

| 项目 | 容器路径或端口 | 权限/可见范围 | 用途 |
|---|---|---|---|
| `logstash.yml` | `/usr/share/logstash/config/logstash.yml` | 只读 | Logstash全局设置 |
| `logstash.conf` | `/usr/share/logstash/pipeline/logstash.conf` | 只读 | input、filter和output |
| `transform_event.rb` | `/usr/share/logstash/pipeline/transform_event.rb` | 只读 | 字段转换及内置测试 |
| `logstash-data`命名卷 | `/usr/share/logstash/data` | 读写 | PQ和运行状态 |
| Beats input | `5044` | Compose内部 | Filebeat写入 |
| Node API | `9600` | Compose内部 | 存活检查和运行指标 |
| Gin内部API | `api:8081` | Compose内部 | Logstash批量写入 |

当前内部传输未启用TLS，安全边界依赖受控的Compose网络。这三个端口都不应直接暴露到公网。

Filebeat的采集标签只能添加到`log-producer`。不要给Filebeat、Logstash或API自身添加该标签，否则可能把平台自身日志再次送回采集链路，形成递归放大。

## 启动入口

`pipeline/`目录同时包含Logstash配置和Ruby脚本。Logstash接收目录作为`path.config`时会拼接并解析目录中的所有文件，因此启动时必须明确指定：

```bash
-f /usr/share/logstash/pipeline/logstash.conf
```

Compose中的等价配置应为：

```yaml
command:
  - -f
  - /usr/share/logstash/pipeline/logstash.conf
```

否则`transform_event.rb`可能被当成Logstash pipeline配置解析，并出现类似错误：

```text
Expected one of ..., "input", "filter", "output"
```

## 配置校验

在项目根目录执行：

```bash
docker run --rm \
  --volume="$PWD/deploy/logstash/config/logstash.yml:/usr/share/logstash/config/logstash.yml:ro" \
  --volume="$PWD/deploy/logstash/pipeline:/usr/share/logstash/pipeline:ro" \
  docker.elastic.co/logstash/logstash:9.4.2 \
  -t \
  -f /usr/share/logstash/pipeline/logstash.conf
```

预期输出包含：

```text
Test run complete ... passed: 4, failed: 0, errored: 0
Configuration OK
```

`transform_event.rb`当前包含三个测试场景和四个断言，覆盖：

- 容器事件的API字段集合。
- 原事件写入`raw_event`。
- 文件输入转换为`source: "file"`。
- 类型不兼容事件被隔离，且原字段未被清空。

`-t`只验证配置语法和Ruby内置测试，不会验证Filebeat连接、HTTP请求或真实SQLite入库。使用`-f`时出现`Ignoring the 'pipelines.yml' file because command line options are specified`属于正常提示。

## 已验证行为

本分支已完成以下运行验证：

| 场景 | 验证结果 |
|---|---|
| Ruby转换测试 | 3个断言全部通过 |
| 根字段边界 | 输出仅包含内部API允许的字段 |
| 原事件保留 | `raw_event`可通过日志详情接口读回 |
| HTTP请求格式 | 单条事件仍以JSON数组发送 |
| 真实API首次接收 | `received=1, inserted=1, duplicated=0, rejected=0` |
| Query API | 列表和详情接口可读回转换后的字段 |
| 重复投递 | `inserted=0, duplicated=1`，SQLite总数仍为1 |
| 字段类型隔离 | 类型错误事件进入转换错误分支，不发送给API |
| PQ故障恢复 | API不可用并重启Logstash后，队列事件自动恢复并入库 |

### PQ故障恢复证据

故障恢复实验使用全新的PQ命名卷、SQLite文件和`source_event_id`，顺序如下：

1. 保持API关闭，启动Logstash和Filebeat。
2. Logstash出现连接拒绝并持续重试。
3. Filebeat指标显示`published=1`、`acked=1`、`active=0`。
4. 先停止Filebeat，避免后续由Filebeat重发。
5. 停止Logstash后运行`pqcheck`，得到`elementCount=1`和`fully-acked: NO`。
6. 启动API，只用同一个PQ卷重启Logstash，不重启Filebeat。
7. Logstash Node API显示`input.in=0`、`output.out=1`、`queue.events_count=0`。
8. API记录`received=1, inserted=1, duplicated=0, rejected=0`。
9. Query API返回`source_event_id=recovery-lab-000001`且`total=1`。

`input.in=0`说明重启后的Beats input没有接收新事件，`output.out=1`说明旧队列事件完成了HTTP输出。两者结合Filebeat没有重启，可以确认事件来自PQ自动恢复。

Filebeat的`acked=1`只表示Logstash已经接受事件，不表示SQLite已经写入。最终入库必须结合API日志或Query API验证。

恢复后`pqcheck`可能仍显示当前空head page为`fully-acked: NO`，应结合`elementCount=0`和Node API的`queue.events_count=0`判断队列已经没有待处理事件。

## 常见故障

### Filebeat连接5044失败

检查：

- Logstash pipeline是否已经出现`Pipelines running`。
- Filebeat和Logstash是否在同一个Compose网络。
- Filebeat是否使用`logstash:5044`而不是宿主机地址。

### Logstash无法连接API

检查：

- `INGESTION_API_URL`是否指向`http://api:8081/internal/v1/logs/bulk`。
- API内部服务器是否监听8081。
- Logstash和API是否在同一个Compose网络。

容器中的`127.0.0.1`表示容器自己，不能用它访问另一个服务。

### API返回400

HTTP 400表示请求级错误，例如非法JSON、未知根字段、不兼容的JSON类型或空数组。当前Ruby脚本会隔离已知字段类型错误，并把根对象重建为固定字段；如果仍然出现400，应优先检查Logstash与API的契约是否发生回归。400不会自动重试，同批事件会被确认并移出PQ。

### API返回200但rejected大于0

缺少必填字段、来源不合法、时间格式错误或正文超限属于单条业务拒绝。批量API仍返回HTTP 200，只在响应和API日志中增加`rejected`汇总值，不返回逐条拒绝详情。Logstash不会重投这些事件，因此需要监控API接收日志中的`rejected`。

### API返回413

检查Logstash批次大小、单条`raw_event`大小和API请求体上限。413不会自动重试。

### 重启后PQ事件消失

检查`/usr/share/logstash/data`是否挂载到同一个命名卷。不要在排障前删除该卷，也不要在没有确认队列损坏时运行`pqrepair`。

### Logstash把Ruby脚本当配置解析

确认启动命令包含：

```text
-f /usr/share/logstash/pipeline/logstash.conf
```

### 出现重复事件

在可重试的至少一次投递路径中，短暂网络错误导致重发属于正常情况。以API返回的`duplicated`和数据库最终条数判断幂等是否正确。

## 官方参考

- <https://www.elastic.co/docs/reference/logstash/plugins/plugins-inputs-beats>
- <https://www.elastic.co/docs/reference/logstash/plugins/plugins-filters-ruby>
- <https://www.elastic.co/docs/reference/logstash/plugins/plugins-outputs-http>
- <https://www.elastic.co/docs/reference/logstash/persistent-queues>
- <https://www.elastic.co/docs/reference/logstash/running-logstash-command-line>
- <https://www.elastic.co/docs/reference/logstash/docker-config>
