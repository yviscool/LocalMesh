# Phase 0 初步施工状态

## 已完成

- `identity.DeviceID`：稳定设备标识，校验格式，不依赖 IP。
- `classroom.Classroom`：课堂成员模型和基础 capability 检查。
- `discovery.Observation`：网络发现结果模型；只能生成配对候选，不授予控制权。
- `session.Session`：`discovered -> pairing -> active -> closed` 状态机、TTL 和心跳过期。
- `discovery.Registry`：线程安全的短期发现缓存和过期候选过滤。
- `session.Backoff`：指数退避、最大延迟和 context 取消。
- `protocol.Envelope`：版本、消息类型、大小、截止时间和命令字段校验。
- `idempotency.Store`：并发安全的幂等键、结果重放和指纹冲突检测。
- `simulation.Run`：100 Agent 并发发现、心跳、地址变化和 session 重连验收。
- `config.Config`：角色、监听、Session、Discovery、Command 和数据保留的安全默认值与启动校验。
- `application` Ports：Discovery、Session、Command transport、Repository 和 AuditSink 的替换边界。
- `application/control.Service`：协议校验、授权、幂等、命令创建和审计编排。
- `storage/sqlite.Store`：Go 1.23 兼容的纯 Go SQLite adapter、顺序 migration、外键、命令、审计、配对、成员和会话生命周期写入。
- `transport/tcp`：标准库 loopback 控制传输、长度分帧、deadline、最大帧保护和集成测试；暂不作为生产 LAN 认证传输。
- `transport/tls`：TLS 1.3 默认、ServerName 校验、握手超时、客户端证书和服务端 mTLS contract test。
- `auth.Verifier`：Ed25519 公钥注册、随机 challenge、签名验证、过期和 replay 拒绝、公钥指纹。
- `application/sessionauth.Service`：将 TLS 后的设备签名证明绑定到 SessionID，激活并持久化 Active Session。
- `application/authorization.Service`：活跃 Session、课堂成员、角色 capability 和目标范围的默认拒绝授权。
- 项目本地 skill 集合：位于 `.agents/skills/vendor/`。

## 下一步

1. 为 SQLite repository 增加 pairing/member/session 的恢复查询和事务用例。
2. 将 session authorization 与课堂成员、角色和 capability policy 接通。
3. 增加 VLAN/客户端隔离和网络故障注入测试。
4. 建立 Windows Service/User Agent 的 named pipe contract test。

## 当前验证

```text
go test ./...  PASS
go test -race -shuffle=on ./...  PASS
go test -race -shuffle=on ./internal/simulation -run TestRunOneHundredAgents -count=1  PASS
```
