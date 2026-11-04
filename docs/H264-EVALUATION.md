# H.264 硬编码评估（Media Foundation）— 结论：暂缓，JPEG+自适应已达局域网目标

## 现状
- JPEG 推流：1080p@15fps q70 ≈ 2-3 MB/s，FEC 8+2 保护，局域网实测流畅
- 自适应：FEC 丢失率反馈自动逐级降质（70→55→40→30），恢复后回升
- 令牌桶限速防突发（默认 4 MB/s 预算）

## H.264 方案对比
| 维度 | JPEG 现状 | MF H.264 硬编码 |
|------|----------|----------------|
| 带宽 | 2-3 MB/s | 0.5-1 MB/s（同观感） |
| CPU | JPEG 编码 ~10ms/帧 | 硬编 GPU/NPU，近零 |
| 实现成本 | 已上线 | MF COM 管线（Source Reader → Encoder → Muxer/网络封装）≈ 1-2 周；还需 H.264 解码侧（学生端）+ 容器/裸流协议 + 关键帧请求机制 |
| 依赖 | 零 | Windows 10+ 硬编支持面不一（含软编回退） |

## 触发条件（何时重启此方案）
1. 单机房 ≥ 100 机同时观看（JPEG 多播带宽 > 200 MB/s 不可行）
2. 跨网段/跨 VLAN 教学（多播不可达，需单播 H.264）
3. 4K 广播需求

## 推进路径（届时）
1. `internal/capabilities/video`：MF Source Reader → H.264 Encoder MFT（裸 Annex-B）
2. 广播协议 v2：ScreenFrame 增加 FormatH264 帧 + GOP 关键帧间隔 + 接收端 Media Foundation 解码渲染
3. JPEG 保留为回退（自适应切换）
