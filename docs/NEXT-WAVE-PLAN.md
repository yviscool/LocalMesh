# LocalMesh 下一轮基础设施计划

## 目标

把当前“可测试的安全基座”推进为“可运行的本地课堂控制节点”。下一轮不做屏幕、文件或教学 UI，先完成会话恢复、发现、可靠传输和 Windows 本机权限边界。

## 当前基线

已经具备：

- Domain：Device、Classroom、Pairing、Session、Command。
- Application：命令提交、幂等、会话认证、课堂授权。
- Persistence：SQLite migration、命令、审计、配对、成员、Session 生命周期写入。
- Transport：TCP 分帧、TLS 1.3、可选 mTLS。
- Identity：Ed25519 challenge-response、过期和 replay 拒绝。
- Verification：race、vet、格式检查、100 Agent 仿真和 GitHub Actions。

尚未具备：

- Session 恢复查询和进程重启恢复。
- 真实 Discovery adapter。
- QUIC 生产传输。
- Windows Service/User Agent IPC。
- 真实网络故障注入矩阵。

## 纵切一：Session Recovery

### 目标

服务或 Agent 重启后，能够从 SQLite 恢复合法的课堂关系；过期、撤销和重复 Session 不得恢复为可控状态。

### 交付

- `SessionRepository` 查询：按 session、device、classroom 查询。
- `MemberRepository` 查询：有效成员、角色、撤销时间。
- 启动恢复器：关闭孤儿 Session，恢复未过期状态。
- Session 恢复策略：旧 Session 默认关闭，新连接重新 challenge。
- 审计事件：`session.recovered`、`session.expired`、`session.rejected`。

### 验收

- 进程重启不会恢复没有有效设备证明的 Session。
- 已撤销成员不能通过数据库中的旧 Session 控制设备。
- 同一设备最多一个 Active Session；旧 Session 被关闭并带原因。
- 时钟偏移、过期时间和数据库异常都有明确错误结果。

## 纵切二：Discovery Service

### 目标

把 Registry 从内存工具升级为可停止、可观测、可限流的发现服务。

### 交付

- `DiscoveryPort` 的 UDP multicast/broadcast adapter。
- announce 包 schema、版本、最大大小和 TTL。
- 接口枚举、广播范围和防火墙错误分类。
- 发现速率限制、重复公告去重和过期清理。
- Discovery 状态：`disabled/starting/ready/degraded/stopped`。

### 验收

- 公告只产生 pairing candidate，不创建成员或权限。
- 100/500/1000 Agent 公告不会导致无界内存增长。
- 重复、过大、错误版本和伪造字段被拒绝。
- 服务停止后 goroutine、socket 和 ticker 全部释放。
- 客户端隔离开启时，UI 能显示可诊断的网络原因。

## 纵切三：QUIC Control Transport

### 目标

在既有 TLS/Envelope 契约上提供生产级可靠控制通道，屏幕数据面保持独立。

### 交付

- QUIC stream adapter，实现 `CommandTransport`。
- TLS/mTLS 配置复用和 DeviceID 公钥绑定。
- 连接生命周期：open、authenticated、ready、degraded、closed。
- stream deadline、最大并发 stream、连接级限流。
- 断线重连和幂等重放策略。

### 验收

- 控制面断线后命令不会永久悬挂。
- 重连不会重复执行同一个 idempotency key。
- 数据面拥塞不会阻塞命令和审计。
- 不支持协议版本、错误证书和错误 DeviceID 都失败关闭。
- 丢包、延迟、重排和重复消息测试可重复运行。

## 纵切四：Windows Service / User Agent IPC

### 目标

建立特权 Service 与交互 User Agent 的最小权限本机边界，为后续锁屏、进程、输入和电源能力提供安全插槽。

### 交付

- Windows named pipe server/client contract。
- pipe ACL、消息长度限制、request ID、deadline 和错误码。
- Service 端再次认证和 capability 检查。
- User Agent 只负责当前用户会话，不持有 Service 私钥。
- Windows build tag 与非 Windows fake adapter。

### 验收

- 未授权用户/进程无法调用特权命令。
- Service 重启不会泄露或复用旧请求。
- pipe 断开、半包、超大包和重复请求可恢复或明确失败。
- Linux/macOS CI 使用 fake contract test，Windows CI 验证真实 named pipe。

## 纵切五：Network Compatibility Lab

### 目标

把网络矩阵变成自动化测试资产，而不是手工经验。

### 场景

```text
Ethernet / Wi-Fi 5 / Wi-Fi 6 / Wi-Fi 7
DHCP / static IP / IPv4 / IPv6
same subnet / VLAN / client isolation
teacher Ethernet + student Wi-Fi
multiple teachers / multiple classrooms
1% / 5% / 15% packet loss
10ms / 100ms / 500ms latency
```

### 验收指标

- discovery success rate
- pairing completion time
- session reconnect time
- command P50/P95 latency
- command loss/duplicate rate
- CPU、memory、bandwidth、open connections
- classroom isolation violations（必须为 0）

## 提交和发布顺序

每个纵切拆成独立提交：

```text
1. contract/spec + failing tests
2. domain/application implementation
3. adapter implementation
4. integration/fault tests
5. docs + CI gate
```

禁止把多个纵切合并成一个大提交。每个提交都必须能够单独回滚，并通过：

```text
go test -race -shuffle=on ./...
go vet ./...
gofmt -l .
git diff --check
```

## 不进入下一轮的内容

- 屏幕采集和编码
- 文件分发
- 教学 UI
- 插件和脚本
- 云端控制台

这些功能依赖控制面、授权、审计和网络恢复稳定后再进入 Phase 1/2。

