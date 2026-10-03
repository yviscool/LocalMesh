# Phase 0 初步施工状态

## 已完成

- `identity.DeviceID`：稳定设备标识，校验格式，不依赖 IP。
- `classroom.Classroom`：课堂成员模型和基础 capability 检查。
- `discovery.Observation`：网络发现结果模型；只能生成配对候选，不授予控制权。
- `session.Session`：`discovered -> pairing -> active -> closed` 状态机、TTL 和心跳过期。
- `discovery.Registry`：线程安全的短期发现缓存和过期候选过滤。
- `session.Backoff`：指数退避、最大延迟和 context 取消。
- 项目本地 skill 集合：位于 `.agents/skills/vendor/`。

## 下一步

1. 用本地 TCP/QUIC 适配器替换测试桩，保留领域层无网络依赖。
2. 增加 100 个模拟 Agent 的课堂仿真测试。
3. 将配对和命令领域模型接入 SQLite 仓储与审计事件。

## 当前验证

```text
go test ./...  PASS
```
