# 运行与故障恢复

## 组件健康

Teacher App、Classroom Engine、Classroom Server 和 Student Service 都暴露结构化健康状态：版本、协议版本、会话数、最后成功心跳、队列长度、错误计数和磁盘可用空间。

## 重启语义

- Teacher App 重启：Server/Agent 保留成员关系；重新建立教师会话。
- Classroom Engine 重启：从 SQLite 恢复课堂和未完成命令，过期命令标记为 `TimedOut`。
- Student Service 重启：User Agent 重新绑定；旧 session 关闭，新 session 使用相同 DeviceID。
- Server 重启：客户端指数退避；不重复创建 pairing 或 command。

## 可观测性

所有事件至少包含：`event_id`、`occurred_at`、`component`、`classroom_id`、`actor_id`、`device_id`、`request_id`、`outcome`。高频心跳不逐条写审计表，采用指标和采样日志。

建议指标：

```text
discovery_candidates_total
pairing_requests_total{status}
active_sessions
session_reconnects_total
command_requests_total{command, outcome}
command_duration_seconds
command_queue_depth
screen_stream_bitrate_bytes
audit_write_failures_total
```

## 数据保留

- 审计事件默认保留 180 天，可按学校策略调整。
- 发现观察和心跳只保留最近窗口，不作为长期历史。
- 屏幕帧默认不落盘；录屏是单独启用、单独授权、单独保留策略。
- 删除或导出课堂数据必须生成审计事件。

