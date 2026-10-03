# 测试与验收策略

## 测试层级

| 层级 | 内容 | 运行方式 |
| --- | --- | --- |
| Unit | ID、状态机、授权、策略、聚合 | `go test ./...` |
| Race | registry、session、command worker 并发 | `go test -race -shuffle=on ./...` |
| Protocol | schema、版本、错误码、golden message | `go test ./internal/protocol/...` |
| Integration | 本地 TCP/QUIC、SQLite 事务、断线重连 | `go test -tags=integration -count=1 ./tests/...` |
| Simulation | 100/500/1000 Agent、心跳和广播 | `go test -tags=simulation ./tests/...` |
| Windows | Service、named pipe、捕获、输入和安装 | Windows CI / 专用实验机 |
| Compatibility | Wi-Fi、VLAN、隔离、DHCP、丢包和延迟 | Network Compatibility Lab |

## Phase 0 验收

- 100 个 Agent 在同一课堂内完成发现、配对和认证。
- 两个课堂共享一个网段时，教师只能看到自己课堂的授权设备。
- Agent IP 改变后，DeviceID、成员关系和审计关联不改变。
- 心跳丢失进入 `Offline`，恢复后通过 backoff 重连，不创建重复 session。
- 重复提交同一 idempotency key 不重复执行命令。
- 断开控制连接后，命令结果最终为成功、失败或超时之一，不能永久悬挂。

本地质量门禁：

```text
make check
make protocol-test
make simulate
git diff --check
```

CI 必须运行 `go test -race -shuffle=on ./...`、`go vet ./...`、格式检查和 100 Agent 仿真；任何一个失败都不能合并。

## 故障注入

- 丢包：1%、5%、15%
- 延迟：10ms、100ms、500ms
- 重排和重复消息
- DHCP 地址变化、多网卡切换
- AP client isolation on/off
- Server 重启、Agent Service 重启、教师端崩溃
- SQLite 写入失败、磁盘满、时钟偏移

## 证据要求

每个网络/权限变更至少保存：测试命令、拓扑参数、通过/失败结果、关键日志 ID 和已知限制。性能测试同时记录 CPU、内存、带宽、连接数、P50/P95 延迟和重连时间。
