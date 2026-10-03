# 领域模型

## 核心实体

| 实体 | 责任 | 稳定标识 |
| --- | --- | --- |
| Device | Windows 设备及能力 | `device_id` |
| Teacher | 操作者身份 | `teacher_id` |
| Student | 课堂中的人员身份 | `student_id` |
| Classroom | 权限与成员边界 | `classroom_id` |
| Group | 课堂内批量目标 | `group_id` |
| Seat | 座位到设备的映射 | `seat_id` |
| Session | 一次认证连接 | `session_id` |
| Command | 对目标执行的意图 | `command_id` |
| Policy | 超时、重试和授权约束 | `policy_id` |
| Capability | 设备支持的能力 | `capability_id` |
| PairingRequest | 设备请求加入课堂的审批对象 | `request_id` |
| AuditEvent | 不可变的管理操作记录 | `event_id` |

## 关系

```text
Classroom 1--N Group
Classroom 1--N Member(Teacher|Student)
Group N--N Device
Seat 1--1 Device (可选)
Device 1--N Session
Command -> Target(Group|Device|Student|Classroom)
Command -> Policy -> Result
PairingRequest -> Classroom + Device
AuditEvent -> Actor + Target + Outcome
```

设备的 IP、MAC、网络接口和在线状态是可变观测值，不能作为外键。成员关系必须带来源、创建时间、审批者和撤销时间。

## 命令生命周期

```text
Draft -> Authorized -> Dispatched -> Running ->
Succeeded | PartiallySucceeded | Failed | TimedOut | Cancelled
```

所有命令必须可幂等处理；批量结果按目标聚合并保留单目标错误原因。

## 不变量

- 一个活跃 Session 只能绑定一个 DeviceID、一个 ClassroomID 和一个已认证主体。
- 未经课堂成员关系和 capability 检查，不能创建可执行 Command。
- 已撤销成员不能通过旧 session 继续执行命令；授权检查在执行前再次进行。
- 一个 `idempotency_key` 只能对应一个命令意图；payload 或 target 不同必须返回冲突。
- 设备可拥有多个历史 Session，但同一 transport 的旧 Session 必须在新 Session 激活时关闭。
- 审计事件只能追加，任何失败的授权和安全相关拒绝也要记录。
