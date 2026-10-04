# Veyon 多平台架构调研

## 调研范围

参考仓库：`https://github.com/veyon/veyon`。本次分析基于浅克隆源码，研究目录位于 LocalMesh 工作区之外，不纳入版本控制，也不复制其代码。

Veyon 当前采用 Qt/C++，核心工程同时覆盖 Windows、Linux、macOS/Android 相关构建路径。它的关键价值不在于某个具体 UI，而在于把跨平台差异隔离到平台插件和服务适配层。

## 观察到的结构

| Veyon 层 | 作用 | LocalMesh 对应方向 |
| --- | --- | --- |
| `core/` | 稳定领域、配置、协议、插件接口、组件管理 | `internal/domain`、`internal/application`、`internal/protocol` |
| `service/` | 系统级守护进程入口和生命周期 | `apps/service`、Windows Service adapter |
| `server/` | Agent 端控制服务、会话和功能协调 | `apps/server`、Application/Ports |
| `worker/` | 将高风险或可选能力隔离为独立进程 | 后续 capability worker / privileged broker |
| `master/` | 教师端控制台 | `apps/teacher`、`web/` |
| `plugins/platform/{windows,linux,common}` | 平台能力实现与公共抽象 | `internal/platform` + build-tag adapters |
| `plugins/<feature>` | 屏幕、文件、电源、消息等可插拔能力 | Phase 1+ capability modules |
| `configurator/`、`cli/` | 配置和运维入口 | `cmd/`、后续管理工具 |

## 可借鉴设计

### 1. 平台能力接口优先于平台判断

Veyon 用 `PlatformPluginInterface` 暴露 core、filesystem、input、network、service、session、user 等能力；上层依赖接口，Windows/Linux 实现分别放在平台插件目录。LocalMesh 应采用同样方向：

- `internal/platform/ports.go` 只定义能力接口，不出现 Windows/Linux API。
- `internal/platform/windows` 实现 Service、named pipe、进程、电源和输入能力。
- `internal/platform/linux` 实现 systemd、Unix socket/DBus、进程和桌面能力。
- `internal/platform/darwin` 实现 launchd、Unix socket 和 macOS 权限能力。
- `internal/platform/common` 放协议无关的校验、生命周期和审计包装。

禁止在 Domain/Application 中散落 `runtime.GOOS` 分支；平台差异必须停在 adapter 边界。

### 2. Service、Server、User Agent 分开部署

Veyon 的 `service` 负责系统服务生命周期，`server` 负责网络控制端点，`worker` 负责按功能启动的隔离进程。LocalMesh 应把这三种职责明确拆开：

```text
Teacher App / Web UI
        │ authenticated IPC
User Agent (interactive user session)
        │ capability-checked IPC
Privileged Service (machine scope)
        │ local adapters
Windows/Linux/macOS platform APIs
```

User Agent 不保存 Service 私钥；Service 不把桌面 UI 状态当作授权依据。每层都执行 request ID、deadline、capability 和审计检查。

### 3. 可选能力使用 feature/module registry

Veyon 的 `Feature` 元数据包含 UID、父子关系、组件标志和启用状态，功能可声明运行于 Master、Service、Worker。LocalMesh 可引入轻量 registry：

- capability ID、版本、危险等级、支持平台
- 所需进程边界（teacher、agent、service、worker）
- 输入/输出契约和审计动作
- disabled-by-default 与配置校验

这比把所有锁屏、进程、电源、屏幕逻辑塞进一个 Service 更容易进行多平台演进。

### 4. Worker 进程是高风险能力的隔离阀

Veyon 为功能 worker 使用独立进程、认证 token 和本机通信通道，并在管理器中处理启动、停止、崩溃和重启。LocalMesh 后续对屏幕采集、输入接管、文件传输等高风险能力应优先采用 worker：

- Service 只授予最小 capability 和短期 token。
- worker 崩溃不会拖垮控制面。
- worker 生命周期可观测、可超时、可回收。
- worker 不直接决定 classroom authorization。

### 5. 构建矩阵和平台能力矩阵分离

Veyon 的 CMake 根据 `WIN32`、`APPLE`、`UNIX`、`ANDROID` 选择平台实现，CI 使用不同 Linux 发行版容器。LocalMesh 应区分：

1. **编译矩阵**：Windows amd64、Linux amd64/arm64、macOS amd64/arm64。
2. **能力矩阵**：每个平台对 discovery、TLS、named pipe/Unix socket、screen、input、power 的支持状态。
3. **验证矩阵**：可在 CI 运行的 contract tests、需要真实桌面的 integration tests、只能在设备实验室运行的 tests。

## 不直接照搬的部分

- Veyon 的历史功能规模和 Qt/C++ 插件 ABI 不适合直接移植到 Go。
- VNC/屏幕数据面的生命周期不能与 LocalMesh 控制面合并。
- 本地 TCP worker 通道的设计需要结合 LocalMesh 的 session、capability 和审计模型重新实现。
- GPLv2 项目代码不可直接复制到 LocalMesh；这里只吸收架构思想和公开结构信息。

## LocalMesh 多平台目标架构

```text
internal/
  domain/                 # platform-neutral policy and state
  application/            # use cases and ports
  protocol/               # versioned wire contracts
  transport/              # TCP, QUIC, IPC framing
  platform/
    common/               # lifecycle, capability wrappers, contract fakes
    windows/               # Service, named pipe, Windows APIs
    linux/                 # systemd, Unix socket, DBus adapters
    darwin/                # launchd, Unix socket, macOS adapters
  capabilities/
    registry/              # capability metadata and platform support
    workers/               # isolated high-risk feature processes
```

## 后续施工顺序

1. 建立 `internal/platform` Port 与各平台 fake/contract test。
2. 将现有 named pipe adapter 移入 Windows platform adapter；Linux/macOS 使用 Unix socket adapter。
3. 建立 `CapabilityDescriptor` registry，启动时校验平台支持和安全默认值。
4. 建立 worker supervisor：短期 token、崩溃回收、重启退避、审计。
5. 将 Phase 1 控制能力逐项接入 registry 和 worker，而不是直接写入 UI 或 Service 主循环。
6. CI 增加 Windows、Linux、macOS 编译矩阵；真实桌面能力进入标记化实验室测试。

## 结论

Veyon 证明多平台的关键是稳定的 core contract、显式 platform plugin、独立 Service/User Agent/Worker 和能力插件注册，而不是在业务代码中堆叠平台分支。LocalMesh 已有 Domain/Application/Ports、IPC 和 capability 基础，下一阶段应沿这四条边界扩展。
