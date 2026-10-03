# Architecture Planning Skill

## 用途

为 LocalMesh 设计或评审跨进程、跨网络和跨 Windows 权限边界的功能。

## 流程

1. 阅读 `docs/ARCHITECTURE.md`、`docs/DOMAIN-MODEL.md`、`docs/PROTOCOL.md`、`docs/SECURITY.md` 和 `docs/TESTING.md`。
2. 写清用户场景、参与者、目标对象、权限和成功/失败结果。
3. 判断变更属于控制面、数据面、身份、授权、发现、持久化还是 UI。
4. 先定义接口/协议契约，再决定实现；明确超时、重试、幂等、断线和部分成功语义。
5. 检查 Windows Service、User Agent、Teacher App 的信任边界和最小权限。
6. 设计审计事件、指标、日志字段和诊断信息。
7. 为正常、重复请求、断线、过期、无权限和协议不兼容路径补测试。

## 输出

```markdown
## 背景
## 用户流程
## 领域模型变化
## 接口/协议
## 权限与安全
## 失败与恢复
## 测试与验收
## 文档和迁移
```

## 禁止事项

- 不用 IP 地址替代设备身份。
- 不把广播发现结果直接转成控制权限。
- 不把 token、私钥或屏幕内容写入日志。
- 不添加没有截止时间、幂等键和结果聚合策略的批量命令。
- 不绕过领域层从 handler 直接修改数据库。

