# 交付路线图

## Phase 0：Network Foundation

设备身份、发现、配对、认证、会话、心跳、断线重连、协议 Envelope、幂等、配置基线、SQLite 基础模型。验收：同网段 100 台模拟 Agent 可加入指定课堂，网络切换和 DHCP 变化不改变身份；重复命令不会重复执行；无效配置拒绝启动。

## Phase 1：Control Plane

锁屏/解锁、消息、启动进程、结束进程、重启、关机、命令结果聚合和审计。验收：无权限命令被拒绝，批量命令支持超时、重试和部分成功。

## Phase 2：Screen Data Plane

屏幕捕获、编码、缩略图、单屏查看、教师广播。验收：数据面断开不影响控制面，限流和恢复行为可观测。

## Phase 3：File Jobs

发送、收集、分发任务、校验和、进度、重试、取消。验收：大文件传输可恢复，结果可按设备查询。

## Phase 4：Classroom Operations

教师、学生、分组、座位、多教师、RBAC + Capability、课堂切换。验收：同一 Wi-Fi 上多个课堂互不可见、互不可控。

## Phase 5：Teaching Features

举手、投票、抢答、作业收集、课堂记录和教学素材分发。

## Phase 6：Platform

插件、API、SDK、脚本和 Webhook。只有核心协议、权限和审计稳定后才进入此阶段。

## 当前首个里程碑

先实现 Phase 0 的最小纵切：`DeviceID -> Discovery -> Pairing -> Authenticated Session -> Heartbeat -> Reconnect`。不在此里程碑加入屏幕、文件或教学 UI，以便先验证网络和身份模型。

## 基座完成定义

Phase 0 进入业务开发前必须满足：

1. Domain 不依赖 transport、SQLite、Windows API 或 UI。
2. Application 用例只依赖 Ports，并能用内存 fake 完成测试。
3. Protocol 有版本、大小、deadline、错误码和兼容测试。
4. SQLite 有 migration、外键、幂等唯一约束和审计追加约束。
5. 本地 TCP/QUIC 与 named pipe 具备断线、超时、重放和权限拒绝测试。
6. CI 运行 race、vet、格式、100 Agent 仿真和安全扫描。
