# LocalMesh Agent Guidelines

## 项目目标

LocalMesh 是面向 Windows 机房和电子课堂的本地优先控制平台。系统把设备发现、设备身份、课堂成员关系、显式授权和批量命令分开建模，避免“发现到设备就可以控制设备”。

## 规范优先级

1. 用户需求与安全边界
2. 本文件
3. `docs/` 下的架构和领域文档
4. 具体模块旁的 README 与代码注释

发生冲突时，必须在变更说明中记录决策，并同步更新受影响的文档。

## 仓库结构

```text
apps/             可执行应用（teacher、student、server）
cmd/              Go 程序入口
internal/         不对外暴露的领域、应用、基础设施代码
proto/            协议定义与生成代码入口
native/           Windows 原生能力适配（C/C++）
web/              教师端 TypeScript/Web UI
docs/             架构、协议、ADR、路线图
tests/            集成、网络兼容性和课堂仿真测试
tools/             开发与代码生成工具
deploy/            安装包、服务注册和发布配置
.agents/skills/    项目专用 agent skills；
```

## 设计约束

- Local-first：无互联网时，局域网内核心控制仍可用。
- 网络发现不等于控制授权。任何控制操作都必须经过认证、会话和 capability 检查。
- `DeviceID`、`TeacherID`、`ClassroomID` 是稳定身份；IP、主机名和网络接口只是可变属性。
- 控制面与数据面分离：命令、状态和审计不与屏幕/音视频流共用生命周期。
- 所有批量操作采用 `Target + Command + Policy + Result` 模型，支持超时、重试、部分成功和取消。
- Windows 特权操作运行在 Windows Service；交互界面运行在 User Agent/Teacher App，禁止把特权逻辑直接放入 Web UI。
- 默认最小权限，危险能力（关机、输入接管、进程终止等）必须显式授权并写入审计日志。
- 协议、状态机和持久化模型优先于 UI；先写契约，再写实现。
- 基座边界见 `docs/FOUNDATION.md`：业务代码通过 Application/Ports 使用基础设施，禁止跨层直连。

## 开发约定

- Go 代码使用 `gofmt`、`go vet` 和表驱动单元测试。
- TypeScript 使用严格类型检查；禁止把后端响应当作 `any` 使用。
- 公共协议变更必须更新 `proto/`、兼容性说明和对应测试。
- 日志使用结构化字段，禁止记录 token、私钥或屏幕内容。
- 数据库迁移必须可重复执行，并提供回滚策略或明确不可回滚原因。
- 新功能先补文档中的领域模型、权限和失败语义，再进入实现。
- 新增配置必须有安全默认值和校验测试；启动时拒绝无效配置。
- 新增基础设施必须提供可替换的 Port、内存 fake 或 contract test。

## 验证门槛

提交前至少运行：

```text
go test ./...
go vet ./...
go test ./tests/...           # 存在对应测试时
```

涉及协议、网络或权限的变更，还必须补充相应集成测试；涉及 Windows API 的代码必须在 Windows CI 或兼容环境中验证。

## Agent 工作方式

1. 先读取 `aaa.md`、`docs/ARCHITECTURE.md` 和相关 skill。
2. 明确变更影响的边界、权限和失败模式。
3. 小步修改，保持每次变更可构建、可测试、可回滚。
4. 完成后报告改动文件、验证命令和未解决风险。

项目 skill 以 `.agents/skills/vendor/` 为准。该目录中的 skill 副本随项目版本化，避免开发依赖个人全局安装状态。
