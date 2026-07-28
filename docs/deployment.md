# Docker Compose部署说明

本文说明如何在Linux或WSL2 Ubuntu中启动完整的容器日志采集链路：

```text
log-producer
→ Filebeat
→ Logstash
→ Gin Internal API
→ SQLite
→ Gin Public API
```

## 1. 前置条件

在项目根目录确认：

```bash
docker version
docker compose version
docker info | grep -E 'Logging Driver|Docker Root Dir'
```

当前Compose配置依赖：

- Docker数据目录为`/var/lib/docker`。
- 宿主机的8080端口可用，或通过`API_PORT`改为其他端口。

Compose已经单独把`log-producer`的日志驱动固定为`json-file`，因此不要求
Docker daemon的全局默认日志驱动也是`json-file`。

## 2. 准备环境变量

复制环境变量示例：

```bash
cp .env.example .env
```

检查当前Linux用户：

```bash
id -u
id -g
```

API和`log-producer`镜像默认使用UID/GID `1000:1000`运行。若输出不是
`1000`，请修改`.env`：

```dotenv
APP_UID=<id -u的输出>
APP_GID=<id -g的输出>
```

这样非root容器才能写入宿主机的`./data`目录。

`PRODUCER_RUN_ID`默认不需要配置，生产器每次创建容器时会自动生成唯一值。
需要做可重复验收时，才在当前Shell中设置固定值：

```bash
export PRODUCER_RUN_ID=compose-acceptance-001
```

这里必须使用Shell环境变量。当前Compose配置不会把`.env`中的
`PRODUCER_RUN_ID`直接传给生产器。

## 3. 校验并启动

先检查Compose语法：

```bash
docker compose config --quiet
docker compose config --services
docker compose config --images
```

然后构建并等待四个服务健康：

```bash
docker compose up -d --build --wait --wait-timeout 180
docker compose ps
```

预期服务为：

| 服务 | 作用 | 宿主机端口 |
|---|---|---|
| `api` | 持久化日志并提供查询API | `${API_PORT:-8080}` |
| `logstash` | 转换事件并批量调用内部API | 不发布 |
| `filebeat` | 采集容器与文件日志 | 不发布 |
| `log-producer` | 生成stdout、stderr和文件日志 | 不发布 |

启动依赖保持单向：

```text
api healthy
→ logstash healthy
→ filebeat healthy
→ log-producer
```

`depends_on`只负责启动协调。运行期间的短暂故障仍由Filebeat磁盘队列、
Logstash持久队列以及各组件的重试机制处理。

## 4. 验证端到端链路

验证API：

```bash
curl --noproxy '*' -fsS http://127.0.0.1:8080/healthz
curl --noproxy '*' -fsS http://127.0.0.1:8080/readyz
```

本节命令使用默认宿主端口8080。若修改了`API_PORT`，请同步替换以下URL
中的端口。

等待数秒后查询日志：

```bash
curl --noproxy '*' -fsS \
  'http://127.0.0.1:8080/api/v1/logs?service=log-producer&page_size=100'
```

响应中应能看到三种`source`：

```text
stdout
stderr
file
```

若已安装`jq`，可以只查看来源和业务事件ID：

```bash
curl --noproxy '*' -fsS \
  'http://127.0.0.1:8080/api/v1/logs?service=log-producer&page_size=100' \
  | jq -r '.data[] | [.source, .source_event_id] | @tsv'
```

查询某条记录的详情：

```bash
curl --noproxy '*' -fsS \
  http://127.0.0.1:8080/api/v1/logs/<id>
```

详情中的`raw_event`保存了Filebeat交给Logstash的原始事件，可用于排查字段
转换问题。

## 5. 持久化资源

| 资源 | 类型 | 内容 |
|---|---|---|
| `./data:/app/data` | bind mount | SQLite数据库及WAL文件 |
| `producer-logs` | named volume | 生产器NDJSON文件 |
| `filebeat-data` | named volume | Filebeat registry和磁盘队列 |
| `logstash-data` | named volume | Logstash持久队列和运行状态 |

正常停止并重新启动：

```bash
docker compose down
docker compose up -d --wait --wait-timeout 180
```

`docker compose down`会删除容器和网络，但保留上述数据。以下命令会删除
三个命名卷中的生产器文件、Filebeat状态和Logstash状态，因此不能作为普通
停止命令：

```text
docker compose down -v
```

即使使用`down -v`，绑定挂载的`./data/log-platform.db`仍会保留。若要做
完全干净的验收，还必须先备份并单独处理`./data`中的SQLite文件。

## 6. 常用运维命令

查看状态和日志：

```bash
docker compose ps
docker compose logs --tail=100 api
docker compose logs --tail=100 logstash
docker compose logs --tail=100 filebeat
docker compose logs --tail=100 log-producer
```

重启单个组件：

```bash
docker compose restart api
docker compose restart logstash
docker compose restart filebeat
docker compose restart log-producer
```

重新构建本项目的两个Go镜像：

```bash
docker compose build api log-producer
```

## 7. 常见问题

### API为unhealthy

先查看：

```bash
docker compose logs --tail=100 api
ls -ldn ./data
```

如果日志提示SQLite目录不可写，确认`.env`中的`APP_UID`、`APP_GID`与
`id -u`、`id -g`一致，然后重新构建镜像。

### Filebeat未采集stdout或stderr

确认：

```bash
docker info | grep -E 'Logging Driver|Docker Root Dir'
docker inspect log-producer
```

`log-producer`必须：

- 使用`json-file`日志驱动。
- 带有`com.donking36.container_log_platform.collect=true`标签。

Filebeat读取Docker socket具有较高权限；相关安全边界见
[Filebeat采集配置](filebeat.md)。

### 8080端口被占用

在`.env`中改用其他宿主端口：

```dotenv
API_PORT=18080
```

随后通过`http://127.0.0.1:18080`访问公开API。容器内部端口仍为8080。

### curl访问本机却返回代理错误

显式绕过代理：

```bash
curl --noproxy '*' http://127.0.0.1:8080/healthz
```

### Logstash或Filebeat启动较慢

首次启动需要拉取Elastic镜像，Logstash还需要启动JVM。使用：

```bash
docker compose ps
docker compose logs --tail=100 logstash
docker compose logs --tail=100 filebeat
```

判断服务是正常启动中还是配置失败，不要只依据容器是否处于`running`状态。
