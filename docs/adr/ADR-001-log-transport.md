# ADR-001：使用Logstash桥接Filebeat与Gin内部API

> 状态：Accepted
>
> 决策日期：2026-07-22
>
> 适用版本：v0.1.0

## 1. 背景

项目要求Filebeat采集容器日志，并最终由Go服务通过GORM写入SQLite。岗位用例描述了“Filebeat将日志推送给Golang HTTP接口”，但Filebeat标准输出中没有面向任意业务端点的通用HTTP输出。

如果只使用`output.console`，日志只能在Filebeat标准输出中看到，不能形成自动写入SQLite的完整链路。如果使用`output.file`和共享目录让Go轮询，则还需要自行实现文件轮转、偏移量、并发读取和确认协议，容易重复实现采集器职责。

因此需要一个能够同时接收Beats协议和调用通用HTTP接口的传输桥接组件。

## 2. 决策驱动因素

- 必须形成Filebeat到SQLite的真实自动链路。
- 保留Gin作为日志接收和查询服务。
- 不在Go项目中自行实现Beats/Lumberjack协议。
- 下游API暂时不可用时能够重试和缓存。
- 能够批量传输，避免每条日志建立一次HTTP请求。
- 配置和故障行为可以通过Docker Compose复现。
- 五天MVP范围内能够实现和解释。

## 3. 已考虑方案

### 方案A：Filebeat直接调用Gin HTTP接口

结论：不可采用。

原因：Filebeat没有通用HTTP业务输出，不能通过一个标准`output.http`配置直接调用任意Gin接口。

### 方案B：Filebeat `output.file`加Go文件导入器

优点：

- 不增加新的服务型中间件。
- 所有数据留在本地文件。

缺点：

- Filebeat file output主要面向测试场景。
- Go需要再次实现文件监听、偏移量、轮转、确认和恢复。
- 两套状态系统增加重复和丢失风险。
- 不符合岗位用例描述的HTTP接收逻辑。

结论：作为资源严重受限时的备选，不作为主方案。

### 方案C：Filebeat到Logstash，再由Logstash调用Gin HTTP接口

优点：

- Filebeat原生支持Logstash输出。
- Logstash原生支持Beats输入和通用HTTP输出。
- 可以批量、重试并启用持久队列。
- 传输职责与Go业务职责清晰分离。
- 与Elastic生态的标准使用方式一致。

缺点：

- 增加一个JVM容器，镜像和内存占用较大。
- 增加一份pipeline配置和一个故障点。
- 首次镜像拉取和启动时间更长。

结论：采用。

### 方案D：Filebeat到Redis或Kafka，再由Go消费

优点：具有明确的消息缓冲和消费模型。

缺点：

- 增加项目二技术范围外的中间件。
- Kafka属于后续分布式阶段，明显超出轻量MVP。
- 不再使用岗位用例描述的Gin HTTP接收链路。

结论：v0.1.0不采用。

### 方案E：Go实现Beats协议接收器

优点：不需要Logstash容器。

缺点：

- 协议实现、兼容性、TLS、确认和批处理复杂。
- 偏离项目核心目标。
- 五天内风险不可控。

结论：不采用。

## 4. 决策

采用以下链路：

```text
Filebeat
→ output.logstash
→ logstash:5044
→ Logstash beats input
→ 轻量字段映射
→ Logstash HTTP output，JSON批次
→ http://api:8081/internal/v1/logs/bulk
→ Gin + GORM + SQLite
```

## 5. 配置契约

### 5.1 Filebeat到Logstash

- 使用Compose服务名`logstash`进行DNS解析。
- 使用内部端口5044。
- 只配置一个Filebeat输出。
- Filebeat data目录持久化。
- 启用容量有上限的磁盘队列。
- 网络失败时退避重试。

### 5.2 Logstash输入

- Beats input只监听容器内部5044。
- 5044不发布到宿主机。
- 输入事件保留Filebeat来源元数据。

### 5.3 Logstash处理

- 只做必要字段映射和裁剪。
- 不在Logstash中实现最终业务去重。
- 不把所有原始元数据无条件发送给API。
- 永久字段规则以内部接收DTO为准。

### 5.4 Logstash到API

- 目标为`http://api:8081/internal/v1/logs/bulk`。
- Content-Type为`application/json`。
- 使用JSON batch格式。
- 对429和5xx重试。
- 对连接中断后的不确定POST允许重试，因为下游存储是幂等的。
- 对成功处理但包含重复或永久拒绝项的批次，API返回200和计数，避免无限重试。

### 5.5 持久队列

- Logstash启用persistent queue。
- `/usr/share/logstash/data`挂载`logstash-data`命名卷。
- 队列最大容量显式配置。
- 普通容器重启和`docker compose down`后队列数据保留。

## 6. HTTP状态码契约

| API结果 | 状态码 | 是否重试 |
|---|---:|---:|
| 批次完成，包括插入、重复和已分类永久拒绝 | 200 | 否 |
| 整体JSON无法解析 | 400 | 否 |
| 请求整体过大 | 413 | 否 |
| 临时限流或过载 | 429 | 是 |
| 未就绪或SQLite暂时不可用 | 503 | 是 |
| 未预期内部错误 | 500 | 是 |

永久无效事件必须进入`rejected`计数和结构化错误日志，不能通过返回5xx让整个批次永久循环。

## 7. 数据与确认语义

- Filebeat到Logstash和Logstash到API按至少一次处理。
- HTTP成功只能表示API已经完成该批次的分类和持久化决定。
- 网络在响应返回前后中断时，Logstash可以重试同一批次。
- API不得依赖“请求只来一次”，必须通过ADR-002定义的`event_id`实现幂等。
- “最终不重复”是SQLite存储效果，不是对每一网络跳都声称恰好一次。

## 8. 正面影响

- 满足岗位用例中的HTTP接收能力。
- 不需要在Go中实现Beats协议。
- 传输失败和业务失败有明确边界。
- 能使用持久队列覆盖API短时中断。
- 后续可以把HTTP输出替换为Kafka或其他存储，而不改变Filebeat输入。

## 9. 负面影响与成本

- Logstash增加内存和镜像体积。
- Compose服务数量增加。
- 需要维护Elastic组件版本兼容性。
- Logstash启动时间可能明显大于Go API，因此“三秒启动”指标只针对API。
- 多一级传输增加少量延迟和排查维度。

## 10. 风险缓解

- 使用最小pipeline，不引入不必要插件和复杂过滤。
- 固定Filebeat与Logstash兼容版本。
- 限制JVM内存和队列大小。
- 为Filebeat到Logstash、Logstash到API分别保留可识别日志。
- 使用端到端事件编号验证缺失和重复。
- 把Logstash配置校验加入部署验收。

## 11. 验证方式

必须通过：

1. 正常1000条端到端采集。
2. API停止期间继续生成200条事件，恢复后全部入库。
3. Logstash重启后持久队列恢复。
4. Filebeat重启后registry恢复。
5. 重复批次不会产生重复SQLite记录。
6. 8081和5044没有发布到宿主机。

## 12. 重新评估条件

以下任一情况出现时重新评估本ADR：

- 评审方明确禁止增加Logstash。
- Logstash资源占用导致目标环境无法运行。
- 项目进入Kafka分布式传输阶段。
- Filebeat未来提供满足需求且稳定的通用HTTP输出。
- 平台吞吐量显著超过当前SQLite和HTTP批量架构目标。

## 13. 参考资料

- Filebeat输出配置：<https://www.elastic.co/docs/reference/beats/filebeat/configuring-output>
- Filebeat Logstash输出：<https://www.elastic.co/guide/en/beats/filebeat/current/logstash-output.html>
- Logstash Beats输入：<https://www.elastic.co/docs/reference/logstash/plugins/plugins-inputs-beats>
- Logstash HTTP输出：<https://www.elastic.co/docs/reference/logstash/plugins/plugins-outputs-http>
- Logstash持久队列：<https://www.elastic.co/docs/reference/logstash/persistent-queues>
