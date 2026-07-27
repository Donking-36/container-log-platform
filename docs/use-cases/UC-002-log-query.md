# UC-002：日志条件查询与统计

> 文档状态：已评审
>
> 优先级：Must
>
> 所属版本：v0.2.0
>
> 最后更新：2026-07-24

## 1. 用例信息

| 项目 | 内容 |
|---|---|
| 用例ID | UC-002 |
| 用例名称 | 日志条件查询与统计 |
| 主要参与者 | 平台用户、第三方服务 |
| 次要参与者 | 平台运维者 |
| 触发条件 | 用户向公开REST API发送查询、详情或统计请求 |
| 目标 | 在可预测的响应结构和性能范围内获得符合条件的日志数据 |
| 关联需求 | FR-011～FR-016、FR-019、NFR-001、NFR-002、NFR-005、NFR-008、NFR-009 |

## 2. 用例范围

本用例覆盖：

- 日志列表查询。
- 单条日志详情查询。
- 按级别统计。
- 按服务统计。
- 组合过滤、分页、排序和参数错误处理。

本用例不负责采集和写入日志，入库行为由UC-001定义。

## 3. 前置条件

1. API容器已经启动。
2. `/readyz`返回HTTP 200。
3. SQLite表和索引已经初始化。
4. 数据库中允许存在零条或多条日志。
5. 用户能够访问公开API端口。

## 4. 查询接口基线

```http
GET /api/v1/logs
GET /api/v1/logs/:id
GET /api/v1/stats/levels
GET /api/v1/stats/services
```

列表查询参数：

| 参数 | 必填 | 规则 |
|---|---:|---|
| `container` | 否 | 精确匹配容器名 |
| `service` | 否 | 精确匹配逻辑服务名 |
| `level` | 否 | 不区分输入大小写，内部规范化为大写 |
| `start` | 否 | RFC3339，表示闭区间开始时间 |
| `end` | 否 | RFC3339，表示闭区间结束时间 |
| `page` | 否 | 正整数，默认1 |
| `page_size` | 否 | 正整数，默认20，最大100 |

统计接口至少支持`container`、`service`、`level`、`start`和`end`中适用于该统计维度的过滤条件。

## 5. 主成功流程：组合查询日志列表

1. 用户构造`GET /api/v1/logs`请求，并提供零个或多个过滤条件。
2. request ID中间件读取客户端request ID或生成新ID。
3. API解析并校验分页参数。
4. API解析RFC3339时间参数，并验证`start`不晚于`end`。
5. API规范化日志级别等查询值。
6. Handler把合法查询条件交给Service。
7. Service构造与业务规则一致的查询请求，交给Repository。
8. Repository通过GORM添加必要的过滤条件和排序，不拼接不可信原始SQL片段。
9. Repository查询符合条件的总记录数。
10. Repository按`logged_at DESC, id DESC`获取当前页记录。
11. Service组装列表和分页信息。
12. API返回HTTP 200、`data`数组、`pagination`对象和request ID。
13. 请求日志中记录方法、路径、状态码、耗时和request ID。

## 6. 主成功流程：查询日志详情

1. 用户请求`GET /api/v1/logs/:id`。
2. API验证`id`为合法正整数。
3. Repository查询对应日志。
4. API返回HTTP 200和日志详情。

列表响应省略体积较大的`raw_event`；详情接口返回该字段，并保持其原始JSON类型。未提供原始事件时返回`null`。

## 7. 主成功流程：日志统计

1. 用户请求级别或服务统计接口，并提供可选过滤条件。
2. API复用与列表查询一致的时间和过滤参数校验。
3. Repository使用数据库聚合查询统计数量。
4. 级别统计计算每个级别数量和占比。
5. 服务统计计算每个服务的日志数量。
6. API按稳定规则排序统计项并返回HTTP 200。

## 8. 后置条件

- 查询请求不会修改日志数据。
- 成功响应具有稳定JSON结构和request ID。
- 查询错误不会导致API进程退出。
- 请求日志能够根据request ID关联到相应响应。

## 9. 备选与异常流程

### A1：没有匹配数据

1. 查询条件合法，但数据库中不存在匹配记录。
2. API返回HTTP 200。
3. 列表接口返回`data: []`和总数0。
4. 统计接口返回约定的空结果。

结果：没有数据不是系统错误，不返回404。

### A2：时间格式错误

1. 用户提交不是RFC3339的`start`或`end`。
2. API停止处理该请求。
3. API返回HTTP 400、稳定错误码、具体字段提示和request ID。

### A3：开始时间晚于结束时间

1. `start`和`end`均能解析，但`start > end`。
2. API返回HTTP 400和明确错误信息。

### A4：日志级别大小写不同

1. 用户提交`level=error`。
2. API规范化为`ERROR`后执行查询。

结果：与提交`level=ERROR`得到相同结果。

### A5：非法分页参数

1. `page`或`page_size`不是正整数，或者超过约定范围。
2. API返回HTTP 400；如果最终选择限制到最大值，该行为必须在API文档中固定且测试一致。

### A6：详情ID格式错误

1. 用户提交非正整数ID。
2. API返回HTTP 400。

### A7：详情不存在

1. ID格式合法，但数据库中没有该记录。
2. API返回HTTP 404和request ID。

### A8：SQLite查询失败

1. Repository执行查询时出现数据库错误。
2. API记录内部错误和request ID，不向客户端暴露SQL、文件路径或堆栈。
3. SQLite处于`BUSY/LOCKED`等暂时不可用状态时返回HTTP 503。
4. 其他未预期查询错误返回HTTP 500；若服务整体未就绪，则健康入口反映相应状态。

### A9：统计总数为零

1. 过滤范围中没有日志。
2. API不执行除零计算。
3. API返回HTTP 200和空统计结果。

## 10. 业务规则

1. 所有过滤条件均为可选，并支持任意合法组合。
2. 时间范围针对`logged_at`，不是`ingested_at`。
3. 时间边界使用闭区间，除非API文档在实现前明确采用其他约定。
4. 默认排序为`logged_at DESC, id DESC`，保证相同时间下分页稳定。
5. `page`默认1，`page_size`默认20且最大100。
6. 无匹配列表数据返回HTTP 200和空数组。
7. 不允许客户端传入原始SQL、列名或任意排序表达式。
8. 未知级别使用`UNKNOWN`查询。
9. 统计结果必须由数据库聚合产生，不把全部记录读取到Go内存后再统计。
10. 占比显示产生的四舍五入误差不超过0.1%。

## 11. 响应结构基线

### 11.1 列表成功响应

```json
{
  "data": [],
  "pagination": {
    "page": 1,
    "page_size": 20,
    "total": 0
  },
  "request_id": "example-request-id"
}
```

### 11.2 错误响应

```json
{
  "error": {
    "code": "INVALID_TIME_RANGE",
    "message": "start must not be later than end"
  },
  "request_id": "example-request-id"
}
```

最终字段名称和错误码在API文档中固化，但所有接口必须保持同一响应风格。

## 12. 性能要求

组合查询性能测试条件：

- SQLite中预置10,000条日志。
- 10个并发客户端。
- 持续30秒。
- 查询同时包含容器名、级别和时间范围。
- 记录p50、p95、p99和错误率。

通过条件：p95不超过500ms，错误率为0。

## 13. 可观测性要求

请求日志至少记录：

```text
event=http_request
request_id
method
path
status
latency_ms
client_ip
```

为避免敏感信息和高基数字段污染日志，默认不记录完整查询结果，不完整打印用户日志正文。

## 14. 验收场景

### 场景1：组合查询成功

```gherkin
Given SQLite中存在多个容器、级别和时间范围的日志
When 用户按container、level、start和end组合查询
Then API返回HTTP 200
And 每条记录都满足全部过滤条件
And 结果按logged_at和id倒序排列
```

### 场景2：无匹配数据

```gherkin
Given 查询参数格式合法
And SQLite中没有匹配记录
When 用户查询日志列表
Then API返回HTTP 200
And data为空数组
And pagination.total等于0
```

### 场景3：时间范围错误

```gherkin
Given start晚于end
When 用户查询日志列表
Then API返回HTTP 400
And 错误响应指出时间范围无效
And 响应包含request_id
```

### 场景4：统计结果正确

```gherkin
Given 过滤范围内存在可知数量的INFO、WARN和ERROR日志
When 用户请求级别统计
Then 每个级别数量与数据库一致
And 各级别占比计算正确
```

### 场景5：查询性能达标

```gherkin
Given SQLite中存在10000条测试日志
When 10个并发客户端持续30秒执行组合查询
Then p95响应时间不超过500毫秒
And 请求错误率为0
```

## 15. 验收证据

- Handler和Service单元测试结果。
- Repository集成测试结果。
- 查询API示例请求与响应。
- 参数错误响应样例。
- SQLite预置数据生成方式。
- 性能测试命令、原始摘要和环境说明。
- 需求中各组合条件对应的自动化测试名称。
