# UC-003：Docker Compose一键部署与恢复

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
| 用例ID | UC-003 |
| 用例名称 | Docker Compose一键部署与恢复 |
| 主要参与者 | 平台运维者 |
| 次要参与者 | Docker Engine、Docker Compose |
| 触发条件 | 运维者在项目根目录执行Compose启动命令 |
| 目标 | 无需进入容器手工初始化即可启动完整日志平台，并在组件重启后保持数据和处理进度 |
| 关联需求 | FR-010、FR-016～FR-020、NFR-001、NFR-003、NFR-004、NFR-006～NFR-010 |

## 2. 用例范围

本用例覆盖：

- API镜像构建。
- Compose服务、网络和挂载创建。
- 必需服务启动和健康检查。
- 公开API访问验证。
- 组件重启与数据恢复。
- 正常停止和再次启动。

本用例不包括：

- Docker Engine安装。
- 生产级TLS证书和域名配置。
- Kubernetes部署。
- 可选Nginx服务的核心验收。

## 3. 前置条件

1. WSL2 Ubuntu环境可用。
2. Docker Engine正在运行。
3. `docker compose version`能够成功执行。
4. 当前目录为项目根目录。
5. 项目代码、Dockerfile、Compose配置、Filebeat配置和Logstash pipeline完整。
6. 必需端口未被其他进程占用。
7. Docker能够访问所需镜像仓库，或依赖镜像已经存在于本地。
8. `.env.example`已经说明必需配置；实际`.env`不进入Git。

## 4. 必需服务

| 服务 | 职责 | 核心健康条件 |
|---|---|---|
| `api` | 写入SQLite并提供公开和内部API | `/readyz`成功且SQLite可访问 |
| `logstash` | 接收Beats事件并向内部API发送HTTP批次 | pipeline成功启动，5044端口可用 |
| `filebeat` | 采集目标容器和文件日志 | 输入启动并连接Logstash |
| `log-producer` | 生成可验证的样例日志 | 进程正常并按配置产生日志 |

SQLite不作为Compose服务。数据库文件由`api`通过`./data:/app/data`使用。

## 5. 持久化资源

| 资源 | 类型 | 作用 |
|---|---|---|
| `./data` | bind mount | 保存SQLite数据库文件，便于验收查看和备份 |
| `producer-logs` | named volume | 在`log-producer`和Filebeat之间共享文件日志 |
| `filebeat-data` | named volume | 保存Filebeat registry |
| `logstash-data` | named volume | 保存Logstash持久队列和必要状态 |

正常的`docker compose down`不得删除以上命名卷和`./data`。`docker compose down -v`属于明确的破坏性清理操作，不得作为普通停止命令写入快速开始流程。

## 6. 主成功流程：首次部署

1. 运维者确认位于项目根目录。
2. 运维者根据`.env.example`准备必要配置。
3. 运维者执行：

   ```bash
   docker compose up -d --build
   ```

4. Docker执行API多阶段构建，生成固定版本的运行镜像。
5. Compose创建内部采集网络、公开访问网络、命名卷和绑定挂载。
6. Compose启动API，API打开或创建`./data/log-platform.db`并执行可重复迁移。
7. API完成就绪检查。
8. Compose启动Logstash，Logstash加载Beats输入和HTTP输出pipeline，并准备持久队列。
9. Filebeat启动采集输入、恢复或创建registry并连接Logstash。
10. `log-producer`启动，向stdout、stderr和共享文件写入测试日志。
11. Filebeat采集事件并经Logstash发送到内部API。
12. API将事件写入SQLite。
13. 运维者执行`docker compose ps`，确认所有必需服务达到约定状态。
14. 运维者调用`/healthz`、`/readyz`和公开查询API。
15. 运维者确认至少一条由`log-producer`自动产生的事件可以被查询。

## 7. 后置条件

- 所有必需服务正在运行并具有可判断的健康状态。
- 公开API能够从宿主机访问。
- Filebeat和Logstash内部端口不向宿主机公开。
- SQLite数据库文件存在于`./data`。
- Filebeat registry和Logstash持久队列使用命名卷。
- 至少一条非人工POST创建的容器日志已经进入SQLite。

## 8. 主成功流程：正常停止和再次启动

1. 运维者记录当前日志总数。
2. 运维者执行：

   ```bash
   docker compose down
   ```

3. Compose停止并删除容器和网络，但保留命名卷和`./data`。
4. 运维者再次执行：

   ```bash
   docker compose up -d
   ```

5. 各组件从持久化状态恢复。
6. 运维者查询数据库并验证原有日志仍存在。
7. `log-producer`产生新日志，平台继续采集。

结果：停止和重建容器不会导致业务数据、registry或持久队列无故丢失。

## 9. 主成功流程：单组件重启

分别执行：

```bash
docker compose restart api
docker compose restart logstash
docker compose restart filebeat
docker compose restart log-producer
```

每次重启后：

1. 等待目标组件恢复预期健康状态。
2. 生成新的唯一日志事件。
3. 查询公开API。
4. 验证新事件最终入库。
5. 验证旧事件没有产生重复存储。

## 10. 备选与异常流程

### A1：API镜像构建失败

1. Go编译、测试或镜像构建阶段失败。
2. Compose返回非零状态并保留明确构建错误。

结果：不得使用旧二进制伪装成功；根据错误修复后重新构建。

### A2：公开端口被占用

1. Docker无法绑定公开API端口。
2. Compose明确报告端口冲突。

结果：部署不宣称成功；部署文档说明如何检查占用并通过环境变量调整宿主机端口。

### A3：镜像仓库暂时不可访问

1. Docker拉取Filebeat、Logstash或基础镜像失败。
2. Compose输出网络或认证错误。

结果：已运行容器不被无意义删除；运维者修复网络或代理后可以重试。

### A4：SQLite目录不可写

1. API因挂载权限问题无法打开数据库文件。
2. API启动失败或`/readyz`保持非200。
3. API日志说明数据库目录不可写，但不泄露敏感配置。

结果：Compose状态能够体现服务未就绪，不把平台误报为健康。

### A5：Logstash配置错误

1. Logstash无法解析pipeline配置。
2. Logstash容器日志指出配置位置和原因。
3. Filebeat显示下游连接失败并重试。

结果：日志不被静默丢弃；修复配置并重建Logstash后可以继续处理。

### A6：Filebeat无权读取Docker日志

1. Filebeat无法访问Docker日志目录或获取容器元数据。
2. Filebeat日志记录权限或路径错误。

结果：部署状态不视为端到端成功；运维者根据部署文档核对只读挂载和Docker Engine实际路径。

### A7：API未就绪

1. API进程存在，但SQLite初始化尚未完成或失败。
2. `/healthz`可表示进程存活，`/readyz`返回非200。
3. Logstash将暂时失败的事件保留并重试。

结果：未就绪服务不接收不可安全处理的日志批次。

### A8：单个组件异常退出

1. 某一必需组件非正常退出。
2. Compose根据明确的重启策略尝试恢复。
3. 持久化状态用于继续处理积压事件。

结果：在重启策略和缓冲容量内，平台无需人工删除状态即可恢复。

## 11. 业务规则

1. 所有镜像使用明确版本，不使用`latest`。
2. API使用多阶段构建和非root运行用户。
3. 构建阶段与运行阶段分离，最终镜像不包含Go构建缓存和Git目录。
4. 内部采集流量只走Compose内部网络。
5. 宿主机只映射公开API端口；内部接收和Beats端口不公开。
6. SQLite使用绑定挂载，Filebeat和Logstash状态使用命名卷。
7. `depends_on`只解决部分启动顺序，不替代组件自身的重试和健康检查。
8. 健康检查必须检测真实服务能力，不能只检查容器进程存在。
9. 正常停止不使用`down -v`。
10. 首次镜像拉取和构建时间不计入API三秒启动指标。

## 12. 启动时间要求

API启动时间定义为：

```text
api容器进程开始运行
→ /readyz首次返回HTTP 200
```

验收方法：

1. 预先完成镜像构建。
2. 连续启动API至少5次。
3. 每次记录开始时间和首次ready时间。

通过条件：每次均不超过3秒。

## 13. 可观测性要求

部署期间至少能够判断：

- API是否存活、是否就绪、数据库路径是否可写。
- Logstash pipeline是否成功加载、持久队列是否可用。
- Filebeat输入是否启动、registry是否恢复、下游是否连接。
- `log-producer`是否按预期生成三类日志。
- 端到端事件是否已经入库。

运维者不应仅根据“容器状态为running”判断平台成功。

## 14. 验收场景

### 场景1：干净环境一键部署

```gherkin
Given Docker Engine和Docker Compose可用
And 项目配置完整
When 运维者执行docker compose up -d --build
Then 所有必需服务最终达到约定健康状态
And 公开API可以访问
And 至少一条log-producer日志自动进入SQLite
```

### 场景2：API容器重建后数据保留

```gherkin
Given SQLite中已经存在日志数据
When 运维者删除并重建api容器
Then 原有日志仍能通过公开API查询
And 数据库文件仍存在于宿主机data目录
```

### 场景3：Compose停止后重新启动

```gherkin
Given 平台已经采集日志并保存处理状态
When 运维者执行docker compose down后再次执行docker compose up -d
Then 原有SQLite数据仍存在
And Filebeat和Logstash从持久化状态恢复
And 新日志可以继续入库
```

### 场景4：API启动时间达标

```gherkin
Given API镜像已经构建完成
When 连续五次启动api并探测ready接口
Then 每次从进程启动到ready成功均不超过三秒
```

### 场景5：内部端口未公开

```gherkin
Given Compose平台正在运行
When 从宿主机检查已发布端口
Then 只能看到公开查询入口
And 无法直接访问Logstash Beats端口和Gin内部接收入口
```

## 15. 验收证据

- `docker compose config`校验结果。
- `docker compose build`结果。
- `docker compose up -d`和`docker compose ps`输出。
- 各必需服务健康状态。
- 公开API健康、就绪和查询结果。
- 宿主机`./data`中的SQLite文件。
- Docker命名卷列表与挂载检查。
- 单组件重启测试结果。
- Compose停止和重建后的数据核对结果。
- 五次API启动时间原始记录。
