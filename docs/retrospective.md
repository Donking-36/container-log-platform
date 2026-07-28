# Project Retrospective

> 项目：容器化日志收集平台  
> 复盘日期：2026-07-28  
> 范围：第二阶段完整MVP实现与验收

## 1. 最终交付

本阶段完成了：

- Go、Gin、GORM和SQLite组成的日志接收、查询与统计服务。
- Public和Internal双HTTP Server及网络边界。
- 稳定`event_id`、SQLite唯一约束和幂等批量写入。
- stdout、stderr和NDJSON文件三类日志生产与采集。
- Filebeat自动发现、文件采集、registry和磁盘队列。
- Logstash字段转换、HTTP批量输出、非法事件隔离和持久队列。
- API和log-producer非root多阶段镜像。
- 四服务Docker Compose编排、健康检查、网络和持久化挂载。
- 单元、集成、竞态、性能、故障注入和Compose端到端验收。

正式验收结果见[Final Acceptance Test Report](test-report.md)。

## 2. 做得比较好的地方

### 2.1 先明确边界再写代码

需求、用例、架构和ADR先确定了组件职责。Filebeat负责采集，Logstash负责
传输转换，API负责业务校验与最终幂等，避免同一规则分散在多个组件中。

### 2.2 分层实现便于测试

Handler、Service、Repository和Ingestion职责分离后，可以分别验证HTTP行为、
业务规则、SQLite行为和事件规范化。核心业务包覆盖率均超过80%。

### 2.3 用稳定ID解决至少一次投递

Filebeat和Logstash都可能重试。平台没有假设链路只投递一次，而是通过稳定
`event_id`和数据库唯一约束保证重复投递不产生重复数据。正式重放1000条事件
后，数据库仍保持1000条唯一记录。

### 2.4 验收使用公开行为而不是直接查看数据库

E2E验证程序通过Public Query API检查数量、分页、排序、字段和唯一性，没有
绕过API直接查询SQLite。这同时验证了存储和用户真正使用的接口。

### 2.5 测试环境与开发环境隔离

验收使用独立Compose项目名、独立端口、临时SQLite目录和独立命名卷，并通过
run label清理fixture容器，避免正式验收污染开发数据。

## 3. 遇到的问题和得到的经验

### 3.1 Docker Desktop与WSL集成不稳定

早期使用Docker Desktop时出现`docker.sock`不存在和WSL桥接二进制I/O错误。
最终改为在Ubuntu中直接运行Docker Engine，命令链路更短，也更符合后续Linux
开发环境。

经验：先确定Docker daemon实际运行位置，再决定CLI、socket和代理配置，避免
同时维护两套Docker环境。

### 3.2 代理既影响镜像仓库，也影响localhost

曾出现Docker Hub连接重置，以及本地`curl`返回502。前者需要正确配置daemon
代理，后者需要：

```bash
curl --noproxy '*' http://127.0.0.1:8080/readyz
```

经验：容器镜像下载代理和应用访问代理是两个不同层次，排查时要分别验证。

### 3.3 Filebeat fingerprint可能让测试fixture相互碰撞

早期E2E文件的前64字节过于相似，Filebeat filestream fingerprint把两个不同
fixture误认为同一来源。把每个fixture唯一标识放到文件开头后，问题消失。

经验：测试数据不仅要保证业务ID唯一，也要符合采集器识别文件身份的规则。

### 3.4 单个队列指标不能完整描述流水线状态

API停止后Logstash已接收200条事件，但`queue.events`瞬时值为150；另外50条
位于正在处理但尚未确认的批次中。最终通过`events.in`增量、重建后的
`events.out`、队列归零和Public API精确数量共同证明200条全部恢复。

经验：故障恢复应使用多项互相印证的证据，不能只依赖一个瞬时指标。

### 3.5 自动化清理必须限制作用域

验收脚本不能按宽泛容器名或全局Docker资源执行删除。最终使用：

- 唯一Compose项目名。
- 验收run label。
- Docker返回的真实容器ID。
- 经过前缀校验的`/tmp`目录。
- 仅属于验收项目的命名卷。

经验：自动化脚本的清理路径和测试逻辑同样需要代码审查。

### 3.6 WSL非交互Shell的PATH与用户终端不同

正式性能测试第一次启动时，非交互命令继承了包含空格的Windows PATH，导致
Shell解析失败。切换为明确的Linux PATH后测试正常执行。

经验：CI和自动化命令应显式声明关键工具路径，不能假设交互式Shell配置一定
存在。

## 4. 当前取舍

以下内容是MVP的主动取舍，不是遗漏：

- 使用SQLite而不是Elasticsearch或服务型数据库。
- 不提供Web管理界面。
- 内部API依靠Compose内部网络隔离，暂未增加令牌或mTLS。
- Filebeat只读挂载Docker socket，保留已记录的权限风险。
- 不引入Kafka、Redis、Kubernetes、Prometheus或Grafana。
- 故障恢复只承诺在已验证容量和等待时间内无丢失。

这些取舍使项目能够在有限周期内完整实现并解释，而不是只搭建大量未打通的
组件。

## 5. 如果重新做一次

- 更早确定所有运行环境都使用WSL原生Docker Engine。
- 第一版测试fixture就加入文件身份唯一前缀。
- 验收脚本从开始就记录Git提交、环境、原始结果和清理策略。
- 更早把Public API与Internal API的端口边界写进README示例。
- 把耗时较长的Compose E2E设计成手动CI job，普通PR仍运行快速Go测试。

## 6. 后续建议

发布之后可以按优先级继续：

1. 增加日志保留和SQLite归档策略，避免数据库无限增长。
2. 增加Prometheus指标，直接观察接收量、拒绝量、队列和查询延迟。
3. 为内部接收接口增加服务身份认证。
4. 使用更大数据集和持续写入场景重新进行性能测试。
5. 学习Kubernetes前，先掌握镜像仓库、容器网络、存储卷和健康探针映射关系。

## 7. 阶段结论

P2-01至P2-12已经完成，P2-13只剩Git Flow发布动作。第二阶段实现和技术验收
已经完成，合并`feature/final-acceptance`后可以创建`release/v1.0.0`。
