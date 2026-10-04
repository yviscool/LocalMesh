# 下一轮：多平台原生适配与安全启动链

本轮以“不维护旧版本兼容层”为前提，直接把新的多平台契约接入启动和运行时。

已完成的基石项：平台 Runtime 组合、Unix socket contract、跨平台默认
Capability Registry、Worker 状态和健康检查契约。Windows named pipe 的
真实 ACL/peer identity 仍需 Windows runner 和实验室证据。

## 目标

1. Windows named pipe、Linux Unix socket、macOS Unix socket 统一实现 LocalEndpoint。
2. 启动时组合 `platform.Runtime`、Capability Registry 和 Worker Supervisor。
3. 将 capability 支持状态暴露为可审计的诊断结果。
4. 建立 native/CI/lab 三层验证矩阵。

## 纵切

### A. 本机 IPC 原生适配

- Linux 使用 Unix domain socket，设置文件权限并拒绝不安全路径。
- macOS 复用 Unix socket 契约，补 launchd 生命周期说明。
- Windows named pipe 补 ACL 和连接身份检查。
- 三个平台共享 framing、request ID、deadline 和 capability recheck contract tests。

当前进度：Linux/macOS Unix socket contract 已落地；Windows native endpoint
仍需在 Windows CI 中完成 ACL 和身份绑定，当前会显式报告 unsupported，绝不
静默回退到不安全的 loopback TCP。

### B. 启动组合与配置

- `platform/common.RuntimeForCurrentOS` 改为注入真实 adapter。
- 注册核心 capability descriptor；默认只启用安全 capability。
- 平台不支持的默认 capability 使启动失败并返回稳定诊断码。
- 配置校验覆盖 OS、endpoint、worker 限制和服务账户。

### C. Worker 安全生命周期

- worker token 仅通过本机受保护通道传递，禁止命令行参数携带秘密。
- supervisor 记录 started/stopped/crashed/restarted 状态和审计字段。
- 崩溃重启使用上限退避，超过预算进入 disabled 状态。
- 同一 capability 只能有一个活动 worker。

### D. 多平台 CI 与实验室矩阵

- Windows、Linux、macOS 编译与 contract tests。
- Linux/Windows 可运行原生 IPC integration test。
- macOS 至少完成 Unix socket 和 launchd 配置验证。
- 屏幕、输入、电源等特权能力标记为 lab-only，未通过实验室不启用。

## 验收门槛

```text
go test -race -shuffle=on -count=1 -timeout=5m ./...
go vet ./...
gofmt -l .
git diff --check
```

任何旧协议或旧配置输入都必须明确拒绝；不添加兼容分支。
