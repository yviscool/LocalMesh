# 网络兼容矩阵

## 拓扑场景

| 场景 | 发现方式 | 控制路径 | 预期 |
| --- | --- | --- | --- |
| 同交换机有线 | multicast/broadcast | P2P | 支持 |
| 教师有线、学生 Wi-Fi | server/multicast | Teacher Controller 或 Server | 支持 |
| 同 Wi-Fi，客户端隔离关闭 | multicast/mDNS | P2P | 支持 |
| 同 Wi-Fi，客户端隔离开启 | 已知 Server | Server relay | 需 Server |
| 教师 VLAN 10、学生 VLAN 20 | Classroom Server | Server relay | 支持 |
| 多课堂同网段 | broadcast 后配对 | classroom-scoped session | 互不可控 |
| DHCP 地址变化 | 重连/registry | DeviceID 会话恢复 | 身份不变 |
| IPv4 only | broadcast/mDNS | QUIC/TLS IPv4 | 支持 |
| IPv6 only | multicast/mDNS | QUIC/TLS IPv6 | Phase 0 之后验证 |
| 互联网断开 | LAN discovery | 本地 Controller/Server | 核心控制可用 |

## 不可假设

- Wi-Fi 与 Ethernet 一定属于同一广播域。
- UDP broadcast 一定跨 VLAN。
- mDNS 一定未被 AP 或防火墙拦截。
- 所有设备都可被教师直接连入。
- IP 地址长期稳定。

## 诊断结果

用户界面应区分：

```text
Not discovered       未收到候选公告
Discovered            收到候选，但未建立信任
Reachable             网络可连接，但未认证
Authenticated         已认证，但未加入本课堂
Authorized            课堂成员和 capability 已通过
Online                心跳在阈值内
Degraded              可控制但数据面/部分能力不可用
Offline               超过心跳过期阈值
```

错误提示必须包含下一步诊断方向，例如检查 AP client isolation、VLAN ACL、Windows 防火墙或 Server 地址，不能只显示“找不到学生机”。

