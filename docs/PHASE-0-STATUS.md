# Phase 0 初步施工状态

## 已完成

- `identity.DeviceID`：稳定设备标识，校验格式，不依赖 IP。
- `classroom.Classroom`：课堂成员模型和基础 capability 检查。
- `discovery.Observation`：网络发现结果模型；只能生成配对候选，不授予控制权。
- `session.Session`：`discovered -> pairing -> active -> closed` 状态机、TTL 和心跳过期。
- 项目本地 skill 集合：位于 `.agents/skills/vendor/`。

## 下一步

1. 定义配对请求/审批/撤销模型，并加入幂等请求 ID。
2. 定义控制面消息 envelope：版本、会话、请求 ID、截止时间和错误码。
3. 增加内存版 discovery registry 和重连退避策略。
4. 用本地 TCP/QUIC 适配器替换测试桩，保留领域层无网络依赖。
5. 增加 100 个模拟 Agent 的课堂仿真测试。

## 当前验证

```text
go test ./...  PASS
```

