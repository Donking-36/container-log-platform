# UC-001：容器日志采集与分类存储

> 文档状态：待评审
>
> 优先级：Must
>
> 所属版本：v0.1.0
>
> 最后更新：2026-07-22

## 1. 用例信息

| 项目 | 内容 |
|---|---|
| 用例ID | UC-001 |
| 用例名称 | 容器日志采集与分类存储 |
| 主要参与者 | 系统 |
| 次要参与者 | 平台运维者、来源服务`log-producer` |
| 触发条件 | 目标容器向stdout、stderr或指定NDJSON文件写入一条新日志 |
| 目标 | 将目标日志可靠采集、分类、传输并幂等写入SQLite |
| 关联需求 | FR-001～FR-010、FR-018～FR-020、NFR-003～NFR-008 |

## 2. 用例范围

本用例从来源容器产生一条日志开始，到该日志以规范化记录写入SQLite并可被公开API查询为止。

链路包括：

```text
log-producer
→ Filebeat
→ Logstash
→ Gin内部接收API
→ GORM
→ SQLite
```

本用例不包括公开查询参数的详细处理，查询行为由UC-002定义。

## 3. 前置条件

1. Docker Engine和Docker Compose可用。
2. `api`、Filebeat、Logstash和`log-producer`容器已经由Compose创建。
3. API已完成SQLite初始化，`/readyz`返回HTTP 200。
4. Logstash Beats输入已经监听内部端口5044。
5. Filebeat已经加载有效配置并能访问目标Docker日志与`producer-logs`共享卷。
6. Filebeat registry和Logstash持久队列目录可写且已持久化挂载。
7. `log-producer`具有明确的采集标签，Filebeat不会采集其他未授权容器。

## 4. 输入

来源日志至少属于以下一种：

- 容器stdout日志。
- 容器stderr日志。
- 共享目录中的一行一个JSON对象的NDJSON文件日志。

规范化后事件至少应具备：

```text
event_id
container_name
service
level
message
source
logged_at
```

`container_id`、`log_path`、`log_offset`和`raw_event`允许根据来源情况为空。

## 5. 主成功流程

1. `log-producer`生成一条带有确定编号和发生时间的日志。
2. 日志被写入stdout、stderr或指定NDJSON文件。
3. Filebeat发现新增内容，并根据输入类型读取该事件。
4. Filebeat解析事件，补充容器名、容器ID、逻辑服务名、来源类型和文件位置等元数据。
5. Filebeat检查采集范围，确认事件来自允许采集的`log-producer`。
6. Filebeat通过Beats协议将事件发送到Logstash。
7. Logstash接收事件，按平台内部事件格式整理字段并组成HTTP JSON批次。
8. Logstash调用`POST /internal/v1/logs/bulk`。
9. API为本次HTTP请求生成或接收request ID。
10. API校验批次外层结构、每个事件的必要字段、时间格式和正文大小。
11. API统一日志级别、UTC时间、来源值和可选字段格式。
12. 对没有稳定`event_id`的事件，API根据ADR-002规定的算法计算幂等键。
13. API在数据库事务中通过GORM批量写入合法且不存在的事件。
14. SQLite唯一约束阻止相同`event_id`被重复保存。
15. API返回接收数、插入数、重复数和拒绝数。
16. Logstash收到成功响应后确认本批次已经处理。
17. Filebeat和Logstash推进各自已经确认的状态。
18. 新日志能够通过`GET /api/v1/logs`查询。

## 6. 后置条件

### 6.1 成功后置条件

- 合法新事件在SQLite中恰好存在一条规范化记录。
- 原始来源元数据与数据库分类字段一致。
- 传输组件已经确认成功处理该事件。
- API运行日志包含本次内部接收请求的request ID、结果计数和耗时。

### 6.2 最小保证

- 重复事件不会产生第二条SQLite记录。
- 单条非法事件不会导致API、Filebeat或Logstash进程退出。
- 暂时性下游故障不会要求人工修改registry或队列才能恢复。

## 7. 备选与异常流程

### A1：NDJSON行不是合法JSON

1. Filebeat解析一行日志失败。
2. Filebeat为该事件增加解析错误信息或输出自身错误日志。
3. Filebeat继续处理后续日志行。

结果：平台不中断；错误可通过Filebeat运行日志定位；该非法行不作为合法日志记录入库。

### A2：事件缺少必要字段

1. Logstash把事件发送给内部批量接口。
2. API发现`message`、`logged_at`或其他必要字段缺失且无法规范化。
3. API将该事件计入`rejected`，记录不含完整敏感正文的错误摘要。
4. 同一批次中的其他合法事件继续处理。

结果：批量响应明确报告拒绝数量；非法事件不进入SQLite。

### A3：日志级别缺失或未知

1. API无法将来源级别识别为约定级别。
2. API将`level`规范化为`UNKNOWN`。
3. 事件继续进入正常存储流程。

结果：事件不因未知级别丢失，并可通过`level=UNKNOWN`查询。

### A4：日志正文超过64 KiB

1. API检测到正文超过上限。
2. API按最终架构约定拒绝或安全截断该事件。
3. API记录事件标识、来源、原始大小和处理结果，不完整打印正文。

结果：服务不崩溃、不无限重试同一永久无效事件，行为在API文档中保持一致。

### A5：Logstash暂时不可用

1. Filebeat无法建立或维持Beats连接。
2. Filebeat保留未确认事件并按退避策略重试。
3. Logstash恢复后，Filebeat继续发送积压事件。

结果：在约定缓冲容量内，恢复后事件最终进入SQLite。

### A6：API暂时不可用

1. Logstash调用内部API失败或收到可重试状态码。
2. Logstash保留待投递事件并重试。
3. API恢复并通过就绪检查后，Logstash重新投递。
4. 重试导致的重复请求由`event_id`唯一约束消除。

结果：在持久队列容量内，事件最终无缺失、存储无重复。

### A7：SQLite暂时不可写

1. API批量事务失败。
2. API回滚整个未完成事务。
3. API返回可重试的服务错误并记录request ID。
4. Logstash保留并重试该批次。

结果：SQLite中不存在半批写入产生的不一致状态；恢复后可重新处理。

### A8：收到重复event_id

1. API或SQLite发现`event_id`已经存在。
2. 该事件计入`duplicated`，不再次插入。
3. API仍将已正确处理的批次视为成功，避免永久重试。

结果：同一事件在SQLite中始终只有一条记录。

### A9：来源容器重启

1. 原`log-producer`容器停止，新容器实例启动。
2. Filebeat根据采集配置发现新的容器日志来源。
3. 新事件携带新的容器ID，但逻辑服务名仍为`log-producer`。
4. 采集链路继续工作。

结果：容器实例变化不会中断按服务查询，新事件仍能正确分类。

### A10：Filebeat重启

1. Filebeat容器退出并重新启动。
2. Filebeat从持久化registry恢复文件身份和读取位置。
3. Filebeat继续读取尚未确认的新增内容。
4. 可能的重复发送由API幂等处理。

结果：新日志继续入库，旧日志不产生重复存储。

## 8. 业务规则

1. `event_id`是存储幂等键，必须具有唯一约束。
2. 日志传输按至少一次语义处理，不假设传输层天然恰好一次。
3. 未知级别统一为`UNKNOWN`，已知级别统一大写。
4. 所有内部时间使用UTC，对外使用RFC3339。
5. Filebeat只能采集白名单来源，必须排除自身、Logstash和API运行日志，除非后续有独立采集设计。
6. 永久无效事件不得被无限重试；暂时性依赖故障必须返回可重试错误。
7. 批量写入使用事务，事务失败不得留下无法解释的部分状态。
8. 错误日志不得完整输出用户日志正文、密钥或敏感Header。

## 9. 可观测性要求

平台至少应能观察到：

- Filebeat是否启动目标输入和harvester。
- Filebeat到Logstash是否连接成功。
- Logstash持久队列是否出现积压。
- Logstash到API请求是否成功或重试。
- API每批received、inserted、duplicated和rejected数量。
- SQLite写入失败对应的request ID。
- 各组件重启后的恢复状态。

## 10. 验收场景

### 场景1：三类来源正常采集

```gherkin
Given 完整Compose平台已经健康运行
And log-producer被允许采集
When log-producer分别生成stdout、stderr和文件日志
Then 所有合法事件最终写入SQLite
And 每条记录的source字段与实际来源一致
And 可以通过公开查询API查到这些事件
```

### 场景2：重复投递不重复存储

```gherkin
Given SQLite中已经存在event_id为event-001的日志
When 内部接收接口再次收到相同event_id十次
Then SQLite中event-001仍然只有一条
And 批量响应正确报告重复数量
```

### 场景3：API中断后恢复

```gherkin
Given Filebeat和Logstash正在正常传输日志
When API停止期间log-producer继续生成200条唯一日志
And API随后恢复健康
Then 200条积压日志最终全部写入SQLite
And 缺失数为0
And 重复存储数为0
```

### 场景4：Filebeat重启后续读

```gherkin
Given 前1200条日志已经完成采集
When Filebeat重启并且log-producer再生成100条日志
Then SQLite最终共有1300条唯一日志
And 重启前日志没有被重复存储
```

## 11. 验收证据

本用例完成时必须保存：

- Compose服务状态。
- 三类来源的样例日志。
- Filebeat和Logstash关键运行日志。
- 内部批量接收计数摘要。
- SQLite总数与`COUNT(DISTINCT event_id)`结果。
- 故障注入命令、时间线和恢复结果。
- 对应自动化端到端测试输出。
