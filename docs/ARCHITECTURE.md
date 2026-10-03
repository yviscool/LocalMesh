# LocalMesh 系统架构

## 目标

LocalMesh 控制一个课堂中的设备群体，而不是提供单机远程控制。核心关系是：

```text
Network -> Device -> Identity -> Classroom -> Teacher
```

网络可达性只解决“能否通信”，身份和授权才决定“能否控制”。

实现契约见：[FOUNDATION.md](FOUNDATION.md)、[PROTOCOL.md](PROTOCOL.md)、[SECURITY.md](SECURITY.md)、[DATA-MODEL.md](DATA-MODEL.md)、[NETWORK-MATRIX.md](NETWORK-MATRIX.md)、[TESTING.md](TESTING.md) 和 [OPERATIONS.md](OPERATIONS.md)。

## 进程架构

```text
Teacher App (Wails/WebView2)
        | local IPC / Control API
Classroom Engine (Go)
        | discovery | session | command bus | policy | audit
        | control transport (QUIC/TLS)
Student Windows Service (privileged)
        | named pipe / session bridge
Student User Agent (interactive session)
        | Windows APIs
        +-- screen capture/encode
        +-- input, process, file, power, audio capabilities
```

可选的 Classroom Server 提供跨 VLAN 的注册、转发和多教师协调，但小型课堂必须支持 Teacher Controller 或 P2P 模式。

## 分层

- **Domain**：Device、Teacher、Student、Classroom、Group、Session、Capability、Policy、Command。
- **Application**：发现、配对、会话、批量命令、文件任务、屏幕广播等用例。
- **Infrastructure**：SQLite、mDNS/UDP、QUIC/TLS、日志、Windows Service、原生 DLL。
- **Presentation**：教师端 UI、托盘、状态和错误呈现。

依赖只能从上层指向下层；领域层不得依赖网络、数据库或 UI。

```text
Presentation -> Application -> Domain
                    |
              Ports/Interfaces
                    |
Infrastructure adapters
```

Application 层编排用例并调用端口；Infrastructure 实现端口。禁止在 HTTP、QUIC、named pipe handler 中直接修改领域对象或执行特权 Windows API。

## 控制面和数据面

控制面承载身份、命令、状态、策略和审计；数据面承载屏幕、音频和视频流。两者使用独立连接、队列和限流策略。控制面必须在数据面不可用时继续提供设备状态和命令结果。

## 网络模式

1. P2P：小规模、同网段课堂。
2. Teacher Controller：教师端作为临时控制器。
3. Classroom Server：跨 VLAN、多教师、多课堂和集中审计。

发现顺序：已知 Classroom Server、受信 multicast、受限 broadcast、mDNS、人工地址。发现结果进入待配对状态，不自动加入课堂。

每种模式都实现相同的 `DiscoveryPort`、`SessionPort` 和 `CommandTransport` 接口，模式选择不改变授权语义。Transport 只负责投递和接收，重试、幂等和结果聚合由 Application 层负责。

## 安全边界

流程固定为：`Discover -> Connect -> Authenticate -> Authorize -> Execute -> Audit`。

设备使用稳定 DeviceID 和设备密钥；教师使用 TeacherID 和会话凭据；课堂使用短码/二维码加审批或预注册。权限采用 RBAC + Capability，默认拒绝，危险命令需要二次确认和审计。

详细威胁模型、密钥生命周期和安全默认值见 [SECURITY.md](SECURITY.md)。

## 技术基线

| 领域 | 选择 |
| --- | --- |
| Core | Go |
| Teacher UI | TypeScript + Wails/WebView2 |
| Protocol | Protobuf |
| Transport | QUIC/TLS；必要时 gRPC/HTTP API |
| Discovery | mDNS、UDP multicast/broadcast、已知服务器 |
| Persistence | SQLite |
| Capture | Windows.Graphics.Capture / DXGI |
| Native | C/C++ Windows adapter |
| Packaging | MSI/MSIX |

## 关键非功能目标

- 断网可用；互联网不是运行时依赖。
- 100 台设备课堂的发现、心跳和批量控制稳定。
- 命令有明确超时、重试、幂等和部分成功结果。
- 所有管理操作可审计，可定位操作者、目标、时间和结果。
- 服务重启、网络切换和 DHCP 地址变化不改变 DeviceID。

## 运行时状态

```text
Installed -> Starting -> Ready -> Degraded -> Stopping -> Stopped
                         |          |
                         +----------+
```

组件进入 `Degraded` 时仍可提供的能力必须明确。例如控制面正常而屏幕编码失败时，命令和审计继续运行，UI 显示数据面不可用；认证或策略存储不可用时必须拒绝控制命令。

## 业务扩展路径

```text
Feature Spec
   -> Domain invariant
   -> Application use case
   -> Protocol/Port contract
   -> In-memory tests
   -> Infrastructure adapter
   -> Integration test
   -> UI / packaging
```

任何业务功能都必须沿这条路径进入系统，避免从 UI 直接连接数据库或从网络 handler 直接调用 Windows 特权 API。
