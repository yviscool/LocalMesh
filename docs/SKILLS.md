# Agent Skills 清单

本项目使用的 skills 安装在 Codex 全局目录 `.agents/skills`，来源和职责如下。

## Go（samber/cc-skills-golang）

- `golang-code-style`：格式、命名、可读性和生产代码约定
- `golang-error-handling`：错误分类、包装、边界和恢复
- `golang-concurrency`：context、channel、取消和并发安全
- `golang-testing`：单元、集成、基准和测试辅助
- `golang-project-layout`：Go 项目分层与目录布局
- `golang-security`：密钥、输入、文件和网络安全
- `golang-continuous-integration`：CI、lint、构建和发布
- `golang-troubleshooting`：编译、并发和生产故障定位
- `golang-observability`：日志、指标、追踪和诊断

## 生命周期与验证（addyosmani/agent-skills）

- `test-driven-development`
- `spec-driven-development`
- `incremental-implementation`
- `code-review-and-quality`
- `security-and-hardening`
- `ci-cd-and-automation`
- `observability-and-instrumentation`

## Superpowers（obra/superpowers）

- `superpowers-verification`
- `superpowers-systematic-debugging`
- `superpowers-writing-plans`

同名的 TDD skill 已由 addyosmani 版本提供，因此 Superpowers 的同名目录不重复安装。

## 本项目调用规则

涉及新功能时依次执行：

1. `spec-driven-development`：写场景、边界和验收条件。
2. `superpowers-writing-plans`：拆分可验证的小步任务。
3. `test-driven-development` + 对应 Go skill：先写失败测试，再实现。
4. `superpowers-verification`：运行测试、lint、集成检查并保留 pass/fail 证据。
5. `code-review-and-quality` + `security-and-hardening`：提交前检查回归和权限边界。

网络、协议和批量命令功能必须额外使用 `golang-concurrency`、`golang-testing`、`golang-observability` 和 `superpowers-systematic-debugging`。

架构、安全或持久化变更还必须阅读并落实：`golang-security`、`golang-error-handling`、`spec-driven-development` 和 `code-review-and-quality`。完成前使用 `verification-before-completion`，提交前使用 `requesting-code-review`。
