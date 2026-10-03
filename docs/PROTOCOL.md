# LocalMesh 控制面协议

## 目标

控制面协议必须支持局域网不稳定、多个课堂共存、重复请求、版本演进和审计。传输协议是实现细节，消息语义不能依赖某一种网络库。

## 传输分层

```text
Application message
  -> Envelope
  -> authenticated session
  -> QUIC/TLS stream or datagram
  -> network interface
```

- **可靠命令**：QUIC reliable stream；必须收到明确结果。
- **状态/心跳**：QUIC stream 或小型 datagram；允许丢失，下一次状态会上报完整值。
- **屏幕/音视频**：独立数据面；不复用命令流，不阻塞命令。
- **本机 Service/User Agent**：受 ACL 保护的 named pipe；禁止开放 TCP loopback 作为唯一安全边界。

## Envelope

```json
{
  "protocol_version": 1,
  "message_type": "command.request",
  "message_id": "msg-uuid",
  "request_id": "req-uuid",
  "session_id": "session-uuid",
  "classroom_id": "CLASS-2026-001",
  "sender_id": "T-001",
  "sent_at": "2026-10-03T04:00:00Z",
  "deadline": "2026-10-03T04:00:10Z",
  "idempotency_key": "T-001:open:group-a:nonce",
  "body": {}
}
```

字段要求：

- `protocol_version`：主版本不兼容时拒绝；次版本只增加可选字段。
- `message_id`：单条消息唯一，用于日志和去重。
- `request_id`：同一批量命令的所有目标共享，用于结果聚合。
- `session_id`：必须绑定已认证会话，过期后拒绝。
- `classroom_id`：服务端必须检查会话是否属于该课堂。
- `deadline`：服务端不可延长客户端截止时间。
- `idempotency_key`：同一执行意图重复提交时返回原结果，不重复执行危险操作。

## 核心消息

| 类型 | 方向 | 语义 |
| --- | --- | --- |
| `discovery.announce` | Agent -> 发现者 | 提供候选设备信息，不含授权 |
| `pairing.request` | Agent -> Teacher/Server | 请求加入指定课堂 |
| `pairing.decision` | Teacher/Server -> Agent | approve/reject/revoke |
| `session.open` | 双向 | 建立认证会话和能力快照 |
| `session.heartbeat` | 双向 | 更新在线状态和版本 |
| `command.request` | Teacher -> Agent | 执行一个目标命令 |
| `command.accepted` | Agent -> Teacher | 已接受并排队 |
| `command.result` | Agent -> Teacher | 单目标最终结果 |
| `state.snapshot` | Agent -> Teacher | 完整设备状态，修复丢包后的增量缺口 |
| `audit.event` | Engine -> Store | 追加不可变审计事件 |

## 错误码

```text
AUTH_REQUIRED          未认证或会话过期
AUTH_FAILED            凭据无效
FORBIDDEN              无 capability 或不属于课堂
NOT_FOUND              目标不存在
CONFLICT               幂等键对应不同请求
UNSUPPORTED            设备不支持 capability 或协议版本
DEADLINE_EXCEEDED      截止时间已过
RATE_LIMITED           超过命令/配对/发现速率
TEMPORARY_UNAVAILABLE  可重试的网络或服务不可用
INVALID_ARGUMENT       字段格式或范围错误
INTERNAL               服务端内部错误，细节只写日志
```

错误响应只返回稳定错误码和安全摘要；内部堆栈、路径、token 和 SQL 错误不得下发给客户端。

## 兼容策略

1. 新字段必须可选，旧客户端忽略未知字段。
2. 删除字段或改变语义必须升级主版本。
3. Agent 在连接建立时声明支持的协议版本和 capability。
4. 不支持的命令必须返回 `UNSUPPORTED`，不能静默降级为相近操作。
5. 所有协议变更必须附 golden message、拒绝测试和迁移说明。

