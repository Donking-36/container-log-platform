# REST API 使用说明

本文档记录当前已经实现并经过测试的 HTTP 接口。默认情况下：

- Public Server：`http://127.0.0.1:8080`
- Internal Server：`http://127.0.0.1:8081`

公开查询接口只注册在 Public Server；日志接收接口只注册在 Internal Server。

## 1. 通用约定

### 1.1 Request ID

客户端可以传入：

```http
X-Request-ID: client-request-001
```

合法值会同时出现在响应头和 JSON 响应的 `request_id` 中。请求头缺失或格式不合法时，API 会生成新的 Request ID。

### 1.2 就绪门禁

`/api/v1/*` 和 `/internal/v1/*` 只在服务已就绪且 SQLite 可访问时处理业务请求。未就绪时返回：

```json
{
  "error": {
    "code": "SERVICE_UNAVAILABLE",
    "message": "service is not ready"
  },
  "request_id": "example-request-id"
}
```

状态码为 HTTP 503。`GET /healthz` 和 `GET /readyz` 不受业务路由门禁影响。

### 1.3 统一错误结构

```json
{
  "error": {
    "code": "INVALID_ARGUMENT",
    "message": "page must be a positive integer"
  },
  "request_id": "example-request-id"
}
```

SQL、数据库文件路径、堆栈和日志正文不会写入公开错误响应。

## 2. 查询日志列表

```http
GET /api/v1/logs
```

### 2.1 查询参数

| 参数 | 必填 | 规则 |
|---|---:|---|
| `container` | 否 | 精确匹配容器名，首尾空白由 Service 去除 |
| `service` | 否 | 精确匹配逻辑服务名，首尾空白由 Service 去除 |
| `level` | 否 | 不区分输入大小写；已知级别规范化为大写，其他非空值按 `UNKNOWN` 查询 |
| `start` | 否 | RFC3339 时间，按 `logged_at` 进行闭区间过滤 |
| `end` | 否 | RFC3339 时间，按 `logged_at` 进行闭区间过滤 |
| `page` | 否 | 正整数，默认 `1` |
| `page_size` | 否 | 正整数，默认 `20`，默认最大值为 `100` |

`DEFAULT_PAGE_SIZE` 和 `MAX_PAGE_SIZE` 可以通过环境变量修改。显式传入超过最大值的 `page_size` 会返回 HTTP 400，不会自动截断。

规则：

- `start` 不得晚于 `end`。
- 时间在业务层统一转换为 UTC 后查询。
- 默认排序固定为 `logged_at DESC, id DESC`。
- 未知参数、重复参数、错误百分号编码和其他畸形查询串均返回 HTTP 400。
- 无匹配记录时仍返回 HTTP 200，且 `data` 为 `[]`。
- 列表响应不返回体积较大的 `raw_event`。

### 2.2 示例请求

```bash
curl --noproxy '*' \
  'http://127.0.0.1:8080/api/v1/logs?container=local-producer&level=info&page=1&page_size=20'
```

时间参数应进行 URL 编码：

```bash
curl --noproxy '*' --get \
  --data-urlencode 'start=2026-07-24T10:00:00+08:00' \
  --data-urlencode 'end=2026-07-24T12:00:00+08:00' \
  http://127.0.0.1:8080/api/v1/logs
```

### 2.3 成功响应

```json
{
  "data": [
    {
      "id": 1,
      "event_id": "v1:example",
      "source_event_id": "local-example-1",
      "container_name": "local-producer",
      "container_id": "local-container",
      "service": "log-producer",
      "level": "INFO",
      "message": "hello from local test",
      "source": "stdout",
      "log_path": null,
      "log_offset": null,
      "logged_at": "2026-07-24T10:00:00Z",
      "ingested_at": "2026-07-24T10:00:01Z"
    }
  ],
  "pagination": {
    "page": 1,
    "page_size": 20,
    "total": 1
  },
  "request_id": "example-request-id"
}
```

## 3. 查询日志详情

```http
GET /api/v1/logs/:id
```

`id` 必须是大于零且不超过 `int64` 范围的十进制整数。

示例：

```bash
curl --noproxy '*' http://127.0.0.1:8080/api/v1/logs/1
```

响应中的日志字段与列表接口一致，并额外包含：

```json
{
  "raw_event": {
    "original": "value"
  }
}
```

`raw_event` 以原始 JSON 值返回，不会被二次编码成 JSON 字符串；未提供原始事件时返回 `null`。合法 ID 不存在时返回 HTTP 404。

## 4. 日志统计

统计接口支持以下可选参数：

| 参数 | 规则 |
|---|---|
| `container` | 精确匹配容器名 |
| `service` | 精确匹配逻辑服务名 |
| `level` | 不区分输入大小写 |
| `start` | RFC3339 时间，按 `logged_at` 闭区间过滤 |
| `end` | RFC3339 时间，按 `logged_at` 闭区间过滤 |

统计接口不支持 `page` 和 `page_size`。未知参数、重复参数、错误时间格式以及 `start` 晚于 `end` 均返回 HTTP 400。

统计由 SQLite 使用 `GROUP BY` 完成，不会先读取全部日志再在 Go 内存中统计。结果按照数量降序排列；数量相同时，按照统计维度名称升序排列。

### 4.1 按日志级别统计

```http
GET /api/v1/stats/levels
```

示例：

```bash
curl --noproxy '*' \
  'http://127.0.0.1:8080/api/v1/stats/levels?container=integration-container'
```

成功响应：

```json
{
  "data": [
    {
      "level": "ERROR",
      "count": 2,
      "percentage": 66.7
    },
    {
      "level": "INFO",
      "count": 1,
      "percentage": 33.3
    }
  ],
  "total": 3,
  "request_id": "example-request-id"
}
```

`percentage` 按匹配日志总数计算并四舍五入到一位小数，因此多个比例相加时可能存在轻微舍入误差。

### 4.2 按服务统计

```http
GET /api/v1/stats/services
```

示例：

```bash
curl --noproxy '*' \
  'http://127.0.0.1:8080/api/v1/stats/services?container=integration-container'
```

成功响应：

```json
{
  "data": [
    {
      "service": "integration-service",
      "count": 2
    },
    {
      "service": "worker-service",
      "count": 1
    }
  ],
  "total": 3,
  "request_id": "example-request-id"
}
```

没有匹配日志时，两个统计接口都返回 HTTP 200：

```json
{
  "data": [],
  "total": 0,
  "request_id": "example-request-id"
}
```

## 5. 查询与统计接口错误码

| HTTP 状态码 | 错误码 | 含义 |
|---:|---|---|
| 400 | `INVALID_ARGUMENT` | 查询串、分页参数或详情 ID 不合法 |
| 400 | `INVALID_TIME_RANGE` | `start` 晚于 `end` |
| 404 | `LOG_NOT_FOUND` | 指定日志不存在 |
| 503 | `SERVICE_UNAVAILABLE` | 服务未就绪，或 SQLite 处于 `BUSY/LOCKED` 等临时不可用状态 |
| 500 | `INTERNAL_ERROR` | 未预期的内部查询错误 |

## 6. 健康检查

| 接口 | 成功状态 | 含义 |
|---|---:|---|
| `GET /healthz` | 200 | API 进程存活 |
| `GET /readyz` | 200 | 服务接受流量且 SQLite 可访问 |

`/readyz` 未通过时返回 HTTP 503 和 `{"status":"not_ready"}`。
