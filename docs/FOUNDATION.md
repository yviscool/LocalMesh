# LocalMesh 基座设计

这份文档定义业务开发必须依赖的稳定边界。业务功能可以迭代，基座契约必须保持简单、可测试、可替换。

## 基础设施分层

```text
cmd/apps
   |
application use cases
   |
ports (interfaces)
   |
domain + protocol
   |
adapters: SQLite / QUIC / named pipe / Windows API
```

### Domain

只表达身份、课堂、配对、会话、命令和策略不变量。Domain 不导入网络、数据库、日志实现或 Windows API。

### Protocol

只表达跨进程/跨网络的消息契约、版本、大小限制、截止时间和错误码。Protocol 不决定消息如何传输。

### Application

编排完整用例：授权、幂等、重试、结果聚合和审计。Application 通过 Ports 使用外部能力，不直接创建数据库连接或 socket。

当前第一个完整用例是 `application/control.Service.Submit`：

```text
Validate Envelope
    -> Authorize target/capability
    -> Reserve idempotency key
    -> Persist command
    -> Append audit event
    -> Complete idempotency record
```

仓储失败会释放幂等 reservation；相同 fingerprint 的重试返回 replay；相同 key 的不同 fingerprint 返回 conflict。

会话认证用例 `application/sessionauth.Service.Authenticate` 的顺序是：

```text
TLS connection
    -> challenge DeviceID/SessionID binding
    -> Ed25519 signature verification
    -> session state activation
    -> SessionRepository persistence
```

身份认证成功只证明设备持有密钥；课堂成员、角色和 capability 检查仍由授权用例负责。

### Adapters

实现 Ports：SQLite repository、QUIC transport、UDP/mDNS discovery、named pipe、Windows Service 和 User Agent。当前 SQLite command/audit adapter、loopback TCP contract 和 TLS/mTLS contract 已落地，其余 adapter 按 Phase 0 顺序接入。Adapter 的失败必须转换为稳定错误码，不把驱动细节泄露到 UI。明文 TCP 不能直接用于生产 LAN 控制。

## 配置基线

- `config.Default()` 提供安全默认值。
- Teacher 默认只监听 loopback；对外监听必须由部署配置显式指定。
- Session TTL 必须大于 heartbeat interval。
- 命令 payload、发现包和重试次数都有上限。
- 数据库、审计保留和监听地址属于部署配置，不写死在业务逻辑。
- 配置解析失败时进程拒绝启动，不使用半配置状态。

## 审计基线

所有会改变课堂边界、权限、设备状态或文件状态的操作都写入 AuditSink。审计事件至少包含：

```text
event_id, occurred_at, actor_id, classroom_id,
action, target_kind, target_id, outcome, error_code
```

心跳、发现公告和高频指标进入 metrics/logs，不逐条写入审计表。

## 业务开发规则

1. 新业务先写领域不变量和 Application 用例。
2. 对外消息先更新 Protocol，再实现 transport。
3. 外部副作用必须通过 Port，禁止在领域对象中调用 SQL、socket 或 Windows API。
4. 任何批量操作必须有 deadline、idempotency key、单目标结果和审计结果。
5. 业务错误使用稳定错误码；底层错误只在日志中保留详细上下文。
6. 每个新 Port 必须有内存 fake 或 contract test，确保 adapter 可替换。
7. TLS 认证成功不等于课堂授权；设备公钥证明、session 绑定和 classroom capability 检查必须分层执行。
