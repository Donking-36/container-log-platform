# Filebeat采集配置

Filebeat负责读取`log-producer`的Docker stdout/stderr日志和共享NDJSON文件，将解析后的事件发送给Logstash。它不调用Gin API，也不直接写入SQLite。

## 固定版本

首个兼容性基线固定为：

```text
docker.elastic.co/beats/filebeat:9.4.2
```

禁止使用`latest`。Filebeat与Logstash应使用相同的`9.4.2`版本线。

## 输入一：Docker stdout和stderr

Filebeat使用Docker autodiscover监听容器生命周期，但只为带有以下标签的容器创建输入：

```yaml
labels:
  com.donking36.container_log_platform.collect: "true"
```

在autodiscover模板匹配阶段，标签名称中的点会被解释为字段层级，因此条件使用：

```text
docker.container.labels.com.donking36.container_log_platform.collect
```

`labels.dedot: true`作用于补充到最终事件中的标签字段。采集后的同一标签位于：

```text
docker.container.labels.com_donking36_container_log_platform_collect
```

匹配后，Filebeat只读取该容器自己的Docker日志目录：

```text
/var/lib/docker/containers/<container-id>/*.log
```

输入采用`filestream`，依次执行：

```text
container parser
→ 解开Docker日志封装并得到stream
→ ndjson parser
→ 把生产器JSON放入producer.*命名空间
```

旧的`log`和`container`输入不再使用。它们已经被弃用，并在Filebeat 9中默认禁用。

## 输入二：共享NDJSON文件

文件输入固定读取：

```text
/logs/*.ndjson
```

Compose阶段会把`producer-logs`命名卷：

- 以读写方式挂载给`log-producer`。
- 以只读方式挂载给Filebeat的`/logs`。

文件输入使用独立且稳定的ID：

```text
producer-file
```

每个`filestream`输入必须具有唯一ID，否则registry无法稳定区分读取状态。

## 解析后的字段边界

生产器原始字段位于：

```text
producer.source_event_id
producer.sequence
producer.level
producer.message
producer.logged_at
```

Filebeat保留并补充：

```text
agent.id
container.id
container.name
container.image.name
docker.container.labels.*
stream
log.file.path
log.offset
service.name
platform.input_kind
```

这样不会让生产器的`level`、`sequence`和`logged_at`覆盖Filebeat或ECS自身字段。

stdout和stderr来源必须由Docker日志的`stream`字段判断；文件来源由`platform.input_kind: file`判断，不能根据日志级别猜测来源。

## 非法JSON

`ndjson`解析器启用了：

```yaml
add_error_key: true
```

非法JSON会增加`error.type: json`和错误说明，但不会停止后续文件采集。Logstash阶段会识别并拒绝这类事件，防止它们进入内部日志接收API。

## Registry与磁盘队列

Filebeat的`path.data`将在Compose中使用`filebeat-data`命名卷持久化，其中保存：

- filestream registry。
- 有待发送的磁盘队列。

磁盘队列上限设置为：

```yaml
queue.disk:
  max_size: 256MB
```

达到上限时系统产生反压，不能承诺无限期缓存。该上限会在故障恢复测试中验证。

## Logstash输出

Filebeat只配置一个输出：

```yaml
output.logstash:
  hosts:
    - ${LOGSTASH_HOST:logstash:5044}
```

它通过Beats/Lumberjack协议连接Compose服务名`logstash`的5044端口。Filebeat不配置Elasticsearch、文件或控制台业务输出。

## 所需挂载与权限

Filebeat容器需要：

| 宿主机或卷 | 容器路径 | 权限 | 用途 |
|---|---|---|---|
| Filebeat配置 | `/usr/share/filebeat/filebeat.yml` | 只读 | 启动配置 |
| Docker日志目录 | `/var/lib/docker/containers` | 只读 | stdout/stderr日志 |
| Docker socket | `/var/run/docker.sock` | 只读 | autodiscover和容器元数据 |
| `producer-logs` | `/logs` | 只读 | NDJSON文件 |
| `filebeat-data` | `/usr/share/filebeat/data` | 读写 | registry和磁盘队列 |

访问Docker socket通常要求Filebeat容器以root运行。这扩大了容器对宿主机Docker信息的可见性，因此socket和日志目录都保持只读挂载。

## 配置校验

在项目根目录执行：

```bash
docker run --rm \
  --user=root \
  --volume="$PWD/deploy/filebeat/filebeat.yml:/usr/share/filebeat/filebeat.yml:ro" \
  docker.elastic.co/beats/filebeat:9.4.2 \
  filebeat test config -e --strict.perms=false
```

预期输出包含：

```text
Config OK
```

该命令只验证配置结构，不验证Filebeat能否连接Logstash。连接和事件字段将在Compose端到端测试中验证。

## 官方参考

- <https://www.elastic.co/docs/reference/beats/filebeat/running-on-docker>
- <https://www.elastic.co/docs/reference/beats/filebeat/configuration-autodiscover>
- <https://www.elastic.co/docs/reference/beats/filebeat/filebeat-input-filestream>
- <https://www.elastic.co/docs/reference/beats/filebeat/logstash-output>
- <https://www.elastic.co/docs/reference/beats/filebeat/configuring-internal-queue>
