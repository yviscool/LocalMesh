# LocalMesh 文档地图

## 先读

1. [ARCHITECTURE.md](ARCHITECTURE.md)：整体进程、分层和网络模式
2. [DOMAIN-MODEL.md](DOMAIN-MODEL.md)：实体、关系和不变量
3. [ROADMAP.md](ROADMAP.md)：阶段顺序和验收目标

## 实现契约

- [FOUNDATION.md](FOUNDATION.md)：分层、Ports、配置和业务开发基座
- [PROTOCOL.md](PROTOCOL.md)：控制面消息、版本、幂等和错误码
- [SECURITY.md](SECURITY.md)：信任边界、认证、授权和威胁模型
- [DATA-MODEL.md](DATA-MODEL.md)：SQLite 表、索引约束和事务边界
- [NETWORK-MATRIX.md](NETWORK-MATRIX.md)：网络拓扑、发现和诊断状态
- [TESTING.md](TESTING.md)：测试分层、故障注入和 Phase 0 验收
- [OPERATIONS.md](OPERATIONS.md)：运行状态、指标、重启和数据保留

## 决策与执行

- [DECISIONS.md](DECISIONS.md)：已确认的架构决策
- [PHASE-0-STATUS.md](PHASE-0-STATUS.md)：当前施工状态
- [SKILLS.md](SKILLS.md)：项目级 agent skills 和调用规则
- [superpowers/plans/](superpowers/plans/)：可执行实现计划

## 文档变更规则

协议、权限、数据模型或网络假设发生变化时，必须同时更新对应契约文档、ADR、测试验收条件和迁移说明。原始产品推演保留在根目录 [aaa.md](../aaa.md)，不作为单独的实现契约。
