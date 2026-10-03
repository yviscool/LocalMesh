对。这个项目如果认真做，**网络架构必须放在第一优先级**。因为“控屏软件”表面上是屏幕广播，实际上核心是：

> **如何让一个教师、一个课堂、一个局域网中的几十到几百台 Windows 机器，在复杂网络环境下，稳定地形成正确的控制关系。**

尤其你提出的这个场景非常关键：

```text
同一个 Wi-Fi
        │
        ├── 教师 A → 管理 01 班学生机
        ├── 教师 B → 管理 02 班学生机
        └── 教师 C → 管理 03 班学生机
```

甚至：

```text
同一个校园网
        │
        ├── 机房 1
        ├── 机房 2
        ├── 教室 301
        ├── 教室 302
        └── 教师办公室
```

**不能简单地“发现局域网内所有学生机，然后谁都能控制”。**

我建议我们把整个产品重新定义成下面这个架构。

------

# 一、项目总定位

暂时可以叫：

> **Classroom Control Platform**
>
> 现代 Windows 电子教室 / 机房控制平台

不是单纯：

```text
极域重写
```

而是：

```text
                  Classroom Platform
                         │
       ┌─────────────────┼─────────────────┐
       │                 │                 │
   Classroom           Network          Device
    管理域               网络              设备
       │                 │                 │
       ▼                 ▼                 ▼
   教师/学生          WiFi/有线          Windows
   课堂/分组          LAN/VLAN           Agent
```

------

# 二、先确定最重要的架构原则

我建议从第一天就确定 8 条原则。

### ① Local-first

没有互联网也能工作。

```text
Internet ❌
    │
    X
    │
LAN ─────────── Classroom
```

学校断网：

> 控屏依然正常。

------

### ② 网络不等于课堂

这是整个系统最重要的设计。

传统思维：

```text
IP
 ↓
学生
```

我们应该：

```text
Network
   ↓
Device
   ↓
Identity
   ↓
Classroom
   ↓
Teacher
```

也就是说：

> **网络发现设备 ≠ 获得控制权限。**

------

### ③ Device Identity 独立于 IP

学生电脑：

```text
Device ID

CPC-8F23-19A7
```

而不是：

```text
192.168.1.37
```

因为 Wi-Fi DHCP 会改变 IP。

甚至：

```text
今天：
192.168.1.37

明天：
192.168.1.102
```

依然是同一台电脑。

------

### ④ Teacher Identity 独立于电脑

教师 A：

```text
Teacher ID
T-001
```

今天用：

```text
PC-A
```

明天：

```text
PC-B
```

仍然是同一个教师。

------

### ⑤ Classroom 是核心实体

整个系统最重要的数据结构不是 Teacher，也不是 Computer。

而是：

```text
Classroom
```

例如：

```text
C++ 基础班
```

里面：

```text
Teacher
 └── 陈老师

Students
 ├── PC01
 ├── PC02
 ├── PC03
 └── PC04
```

------

### ⑥ 控制权必须显式存在

```text
发现
≠
连接
≠
认证
≠
授权
≠
控制
```

这五件事情必须分开。

------

### ⑦ 控制面和数据面分离

```text
Control Plane
    │
    ├── 锁屏
    ├── 重启
    ├── 打开程序
    ├── 文件
    └── 配置

Data Plane
    │
    ├── 屏幕
    ├── 音频
    └── 视频
```

这对性能和可靠性非常重要。

------

### ⑧ 所有功能都建立在 Capability 上

未来增加：

```text
Camera
Microphone
Audio
USB
Process
Browser
```

不应该重新设计架构。

------

# 三、整个系统架构

我建议最终：

```text
                         ┌─────────────────────┐
                         │    Teacher Client   │
                         │   TS + HTML/CSS     │
                         └──────────┬──────────┘
                                    │
                               Control API
                                    │
                         ┌──────────▼──────────┐
                         │ Classroom Controller│
                         │         Go          │
                         └──────────┬──────────┘
                                    │
                      ┌─────────────┼─────────────┐
                      │             │             │
                   Discovery      Control       Stream
                      │             │             │
                      ▼             ▼             ▼
                  Discovery       Command      Video
                   Service          Bus        Pipeline
                      │             │             │
                      └─────────────┼─────────────┘
                                    │
                           Network Transport
                                    │
        ┌───────────────────────────┼───────────────────────────┐
        │                           │                           │
     Ethernet                     Wi-Fi                     Mixed
        │                           │                           │
        └───────────────────────────┼───────────────────────────┘
                                    │
                             Student Agent
                                    │
                  ┌─────────────────┼─────────────────┐
                  │                 │                 │
              Windows            Capture            Input
              Service              │                 │
                  │              DXGI              Win32
                  │
        ┌─────────┼─────────┬──────────┬──────────┐
        ▼         ▼         ▼          ▼          ▼
      Process   File      Power      Audio      Device
```

------

# 四、网络架构才是整个项目的灵魂

至少考虑 **6 种网络环境**。

------

## 场景 1：传统有线机房

```text
              Switch
        ┌───────┼───────┐
        │       │       │
      Teacher  PC01    PC02
                │
              PC03
```

特点：

- 延迟低
- 丢包低
- 带宽高
- 设备固定

这是最简单的。

------

# 五、场景 2：教师 Wi-Fi + 学生 Wi-Fi

```text
              AP
       ┌───────┼────────┐
       │       │        │
   Teacher    PC01     PC02
                        │
                       PC03
```

这里马上出现一个问题：

### AP 是否允许客户端互相通信？

很多 Wi-Fi：

```text
Client Isolation
AP Isolation
无线隔离
```

打开以后：

```text
Teacher
   X
PC01
```

根本连接不上。

所以软件必须能够检测：

```text
Discovery Failed
```

并告诉教师：

> 当前 Wi-Fi 开启了客户端隔离，学生机之间无法直接通信。

而不是显示：

> 找不到学生。

------

# 六、场景 3：Wi-Fi + 有线混合

这是学校非常现实的情况。

```text
                  Switch
                /        \
             Teacher      AP
               │        / | \
            Ethernet   PC PC PC
```

教师：

```text
Ethernet
```

学生：

```text
Wi-Fi
```

必须正常。

所以：

> **不能假设所有机器在同一个物理网络介质。**

我们真正关心：

```text
IP Reachability
```

而不是：

```text
Wi-Fi / Ethernet
```

------

# 七、场景 4：同一个 Wi-Fi，不同教师

这是你提出的核心问题。

比如：

```text
WiFi: School-Computer
Subnet: 192.168.10.0/24
```

所有人：

```text
Teacher A
Teacher B

Student 01
Student 02
...
Student 60
```

如果单纯：

```text
Broadcast Discovery
```

结果就是：

```text
Teacher A 看见 60 台
Teacher B 看见 60 台
```

更严重：

```text
Teacher A
    ↓
Student 01
```

Teacher B 也可能控制 Student 01。

这是**架构级安全问题**。

------

# 八、解决方法：Classroom Namespace

每一个课堂拥有：

```text
Classroom ID
```

例如：

```text
CLASS-2026-001
```

学生加入：

```text
CLASS-2026-001
```

教师加入：

```text
CLASS-2026-001
```

于是：

```text
             Same Wi-Fi
                  │
       ┌──────────┼──────────┐
       │          │          │
    Teacher A  Teacher B  Teacher C
       │          │          │
     Class A    Class B    Class C
       │          │          │
    PC01-20    PC21-40    PC41-60
```

------

# 九、但是不要只依赖 Classroom ID

否则学生只要输入：

```text
CLASS-2026-001
```

就可能加入。

所以必须：

```text
Classroom
     │
     ├── Classroom ID
     ├── Secret / Token
     ├── Teacher Identity
     └── Policy
```

学生加入：

```text
Join Request
     ↓
Teacher
     ↓
Approve
     ↓
Student
```

或者：

```text
QR Code
```

教师创建：

```text
C++基础班

[ QR CODE ]
```

学生端扫描/输入：

```text
加入课堂
```

------

# 十、进一步：动态课堂

这会非常有意思。

老师打开软件：

```text
新建课堂

[C++ 第 8 课]

邀请码：

739-284
```

学生：

```text
输入 739-284
```

立即出现：

```text
张三
李四
王五
赵六
```

老师：

```text
[✓] 张三
[✓] 李四
[✓] 王五
```

然后：

> 加入课堂。

这样就解决了：

```text
同一个 Wi-Fi
多个老师
多个课堂
```

------

# 十一、再进一步：Seat / Computer Identity

学校固定机房则完全不同。

可以预配置：

```text
Room 301

A01 → PC-001
A02 → PC-002
A03 → PC-003
...
```

然后：

```text
学生坐下
 ↓
电脑自动识别
 ↓
学生登录
 ↓
自动进入课堂
```

于是：

```text
Teacher
    ↓
Room 301
    ↓
Seat A07
    ↓
Computer PC-007
    ↓
Student 张三
```

这个模型会比传统极域强很多。

------

# 十二、网络发现机制

我不会只设计一种。

而是：

```text
Discovery
├── mDNS
├── UDP Broadcast
├── UDP Multicast
├── Known Server
├── Manual IP
└── Classroom Code
```

优先级：

```text
1. Classroom Server
2. Multicast
3. Broadcast
4. mDNS
5. Manual
```

这样才能覆盖：

```text
普通局域网
Wi-Fi
VLAN
跨网段
复杂校园网络
```

------

# 十三、跨 VLAN 怎么办？

例如学校：

```text
VLAN 10
教师

VLAN 20
学生

VLAN 30
服务器
```

传统广播：

```text
Teacher
  ↓
Broadcast
  X
Student
```

因为广播不会跨 VLAN。

这时候就不能依赖 LAN Discovery。

需要：

```text
Classroom Gateway
```

或者：

```text
Teacher
   │
   ▼
Classroom Server
   │
   ├── VLAN 10
   ├── VLAN 20
   └── VLAN 30
```

所以我建议：

> **Server / Controller 模式必须从第一天存在，但普通小型课堂可以不部署独立服务器。**

------

# 十四、最终网络拓扑应该支持三种模式

## Mode A：P2P

小型机房：

```text
Teacher
 ├── PC01
 ├── PC02
 └── PC03
```

简单。

------

## Mode B：Teacher Controller

```text
              Teacher
                 │
       ┌─────────┼─────────┐
       ▼         ▼         ▼
     PC01      PC02      PC03
```

教师机承担 Controller。

适合：

> 20～50 台。

------

## Mode C：Classroom Server

```text
             Classroom Server
                    │
        ┌───────────┼───────────┐
        ▼           ▼           ▼
     Teacher A   Teacher B   Teacher C
        │           │           │
       PC01        PC21        PC41
```

适合：

> 学校 / 培训机构 / 多教室。

------

# 十五、屏幕传输架构

建议独立成：

```text
Screen Pipeline

Capture
 ↓
Frame Analysis
 ↓
Encoder
 ↓
Packetizer
 ↓
Transport
 ↓
Decoder
 ↓
Renderer
```

Windows：

```text
DXGI Desktop Duplication
```

或者现代 Windows 捕获 API：

```text
Windows.Graphics.Capture
```

编码：

```text
H.264
HEVC
AV1
```

硬件：

```text
NVENC
AMF
Intel Quick Sync
```

最终自动选择：

```text
GPU
 ↓
Hardware Encoder
```

------

# 十六、控屏不是只有“广播”

我建议功能体系直接分成：

```text
                    Classroom
                       │
        ┌──────────────┼──────────────┐
        │              │              │
       View           Control        Teach
        │              │              │
        ▼              ▼              ▼
     监看             控制             教学
```

------

# 十七、View

```text
实时监看
缩略图
单屏查看
多屏查看
全屏查看
屏幕录制
屏幕截图
状态监控
```

例如：

```text
┌──────┐ ┌──────┐ ┌──────┐
│ PC01 │ │ PC02 │ │ PC03 │
│      │ │      │ │      │
└──────┘ └──────┘ └──────┘

┌──────┐ ┌──────┐ ┌──────┐
│ PC04 │ │ PC05 │ │ PC06 │
│      │ │      │ │      │
└──────┘ └──────┘ └──────┘
```

------

# 十八、Control

```text
远程鼠标
远程键盘
锁屏
黑屏
解锁
打开程序
关闭程序
结束进程
重启
关机
注销
音量
```

全部统一成：

```text
Command
```

例如：

```json
{
  "command": "process.launch",
  "target": "group-A",
  "payload": {
    "path": "C:\\Tools\\OJ.exe"
  }
}
```

------

# 十九、Teach

这里可以真正超越传统极域。

```text
广播教师屏幕
发送文件
收集作业
统一打开 IDE
统一打开网页
统一打开 OJ
课堂消息
举手
抢答
投票
随堂测试
```

甚至：

```text
教师：

打开 C++ 题目

       ↓

所有学生：

VS Code
   +
题目
   +
OJ
```

------

# 二十、文件系统

不要做成：

```text
Teacher → File → Student
```

而应该：

```text
Distribution Job
```

例如：

```text
Job #103

Source:
lesson08.zip

Target:
Classroom A

Policy:
overwrite = true
verify = true
```

然后：

```text
PC01 ✓
PC02 ✓
PC03 ✓
PC04 ✗
```

教师立即看到：

```text
48 / 50 completed
```

这就是现代化设计。

------

# 二十一、学生端 Agent

这是整个系统真正的“操作系统级身体”。

建议：

```text
Student Agent
│
├── Service
│
├── Network
│
├── Identity
│
├── Security
│
├── Screen
│
├── Input
│
├── Process
│
├── File
│
├── Power
│
├── Audio
│
└── Policy
```

------

# 二十二、Windows Service + User Agent

这里还有一个非常重要的 Windows 架构问题。

不要让所有东西都运行在普通用户进程。

推荐：

```text
Windows Service
       │
       ├── Privileged Operations
       ├── Network
       ├── Security
       └── Policy
              │
              ▼
         User Agent
              │
              ├── UI
              ├── Tray
              └── Session
```

因为：

```text
Service
```

和：

```text
Interactive User Session
```

在 Windows 上是不同的安全边界。

------

# 二十三、技术栈

我目前会倾向：

| 层             | 技术                                 |
| -------------- | ------------------------------------ |
| Teacher UI     | TypeScript                           |
| UI             | HTML/CSS                             |
| Desktop Shell  | Wails 3                              |
| WebView        | WebView2                             |
| Core           | Go                                   |
| Network        | Go                                   |
| Protocol       | Protobuf                             |
| Control RPC    | gRPC / 自定义 QUIC                   |
| Discovery      | mDNS + UDP                           |
| Stream         | WebRTC / 自定义低延迟 RTP            |
| Screen Capture | Windows.Graphics.Capture / DXGI      |
| Encoder        | Media Foundation / NVENC / QSV / AMF |
| Native         | C/C++ DLL                            |
| Database       | SQLite                               |
| Logging        | structured logging                   |
| Config         | TOML/YAML/JSON                       |
| Testing        | Go test + integration test           |
| Packaging      | MSI / MSIX                           |
| CI             | GitHub Actions                       |

------

# 二十四、为什么我不建议全部用 C++

传统极域式软件：

```text
C++
C++
C++
C++
```

当然可以。

但现在你会遇到：

```text
UI
网络
状态机
并发
配置
日志
插件
测试
协议
```

Go 在这些地方非常舒服。

而 Windows 特有能力：

```text
Capture
Input
D3D
Service
Security
```

让 C/C++ 做。

于是：

```text
             Go
              │
       ┌──────┴──────┐
       │             │
     Business      Native
       │             │
       │           C/C++
       │             │
       ▼             ▼
    Protocol      Windows API
    Network
    State
```

这比：

> 全部 C++

更适合 2026。

------

# 二十五、数据库

Teacher：

```text
SQLite
```

Student：

```text
SQLite
```

Classroom Server：

```text
SQLite
```

保存：

```text
Device
Teacher
Student
Classroom
Seat
Group
Policy
Session
Command
Job
AuditLog
```

例如：

```text
devices
classrooms
classroom_members
teachers
students
seats
groups
sessions
commands
command_results
files
distribution_jobs
policies
audit_logs
```

------

# 二十六、最重要的一个数据库设计

```text
Device
```

不要绑定：

```text
IP
```

而应该：

```text
Device
│
├── device_id
├── hostname
├── mac_addresses
├── machine_guid
├── platform
├── version
├── capabilities
└── last_seen
```

网络信息单独：

```text
NetworkInterface
│
├── interface
├── type
├── ip
├── subnet
├── gateway
└── connection_state
```

所以：

```text
WiFi → Ethernet
```

不会改变：

```text
Device ID
```

------

# 二十七、权限模型

最终建议 RBAC + Capability。

```text
Role
│
├── Teacher
├── Assistant
├── Student
└── Admin
```

权限：

```text
screen.view
screen.broadcast

input.control

process.launch
process.kill

file.send
file.collect

power.shutdown
power.reboot

classroom.manage
student.manage
```

这样以后：

> “助教可以监看，但不能关机。”

只需要：

```text
Assistant
- screen.view
- screen.broadcast
- file.send

+ no power.shutdown
```

------

# 二十八、审计系统

这个传统软件非常容易忽略。

应该记录：

```text
10:31:22 Teacher A
→ Lock Classroom A

10:31:25
→ PC01 success

10:31:25
→ PC02 success

10:31:27
→ PC03 timeout
```

甚至：

```text
谁
什么时间
对谁
执行了什么
结果是什么
```

这对学校环境非常重要。

------

# 二十九、测试架构

这个项目如果想真正成为“2026 年重写”，**测试必须从架构设计阶段进入。**

不能：

```text
写完 → 老师拿去试
```

应该：

```text
Unit Test
     ↓
Protocol Test
     ↓
Agent Test
     ↓
Network Test
     ↓
Integration Test
     ↓
Classroom Simulation
     ↓
Real Hardware
```

甚至模拟：

```text
1000 Student Agents
```

测试：

```text
Teacher Broadcast
```

看看：

```text
CPU
Memory
Bandwidth
Latency
Packet Loss
Reconnect
```

------

# 三十、网络测试矩阵

必须专门做一个：

> **Network Compatibility Lab**

至少测试：

```text
Ethernet
Wi-Fi 5
Wi-Fi 6
Wi-Fi 6E
Wi-Fi 7

DHCP
Static IP

IPv4
IPv6

Same subnet
Different subnet
VLAN

Client Isolation ON
Client Isolation OFF

Packet Loss
Latency
Bandwidth

Teacher Wi-Fi
Student Ethernet

Teacher Ethernet
Student Wi-Fi

Multiple Teachers
Multiple Classrooms
```

这会成为整个项目最有价值的工程资产之一。

------

# 三十一、真正完整的产品架构

最终我会把它设计成：

```text
                         ┌──────────────────────┐
                         │     Teacher App      │
                         │  TS + Wails + WebUI  │
                         └──────────┬───────────┘
                                    │
                              Classroom API
                                    │
                         ┌──────────▼───────────┐
                         │   Classroom Engine    │
                         │          Go           │
                         └──────────┬───────────┘
                                    │
             ┌──────────────────────┼──────────────────────┐
             │                      │                      │
        Identity                 Policy                 Session
             │                      │                      │
             └──────────────────────┼──────────────────────┘
                                    │
                              Network Layer
                                    │
        ┌──────────────┬────────────┼────────────┬──────────────┐
        │              │            │            │              │
     Ethernet         Wi-Fi       VLAN         P2P          Server
        │              │            │            │              │
        └──────────────┴────────────┼────────────┴──────────────┘
                                    │
                              Student Agent
                                    │
                 ┌──────────────────┼──────────────────┐
                 │                  │                  │
              Service             User             Native
                 │                Agent             Layer
                 │                  │                  │
                 ▼                  ▼                  ▼
              Policy              UI              Windows API
              Network           Session          DXGI / D3D
              Security          Tray             Input
              Command                            Process
```

------

# 三十二、开发顺序我会非常克制

**第一阶段不要碰“教学功能”。**

先完成：

### Phase 0：Network Foundation

```text
Device Identity
Discovery
Pairing
Authentication
Session
Reconnect
Heartbeat
```

先证明：

> **100 台 Windows 机器能够稳定地组成一个课堂。**

------

### Phase 1：Control

```text
Lock
Unlock
Message
Shutdown
Restart
Launch
Process
```

------

### Phase 2：Screen

```text
Capture
Encoding
Streaming
Thumbnail
Full Screen
Teacher Broadcast
```

------

### Phase 3：File

```text
Send
Receive
Collect
Distribution Job
Progress
Retry
Checksum
```

------

### Phase 4：Classroom

```text
Class
Group
Student
Seat
Teacher
Multiple Teachers
Permission
```

------

### Phase 5：教学

```text
举手
答题
抢答
投票
作业
自动收集
课堂记录
```

------

### Phase 6：平台化

```text
Plugin
API
SDK
Automation
Script
Webhook
```

这时候才开始真正产生“生态”。

------

# 三十三、我认为最值得你抓住的一个设计

这个软件最终不要以：

> **“控制电脑”**

为核心。

而应该以：

> **“控制一个课堂里的计算机群体”**

为核心。

这两个产品的架构完全不同。

传统：

```text
Teacher
   ↓
Computer
```

我们的：

```text
                 Classroom
                     │
          ┌──────────┼──────────┐
          │          │          │
        Group A    Group B    Group C
          │          │          │
        PC01-10    PC11-20    PC21-30
          │          │          │
       Student    Student    Student
```

然后所有操作都变成：

```text
Target
 +
Command
 +
Policy
 +
Result
```

例如：

```text
Target = Group A
Command = Open("VSCode")
Policy = Retry(3)
Result = 9/10
```

或者：

```text
Target = Student 张三
Command = ViewScreen
```

或者：

```text
Target = Classroom A
Command = Broadcast
Source = Teacher Screen
```

**这才是整个项目的核心抽象。**

如果继续往下做，我下一轮建议直接把它提升到**“正式软件设计文档（SAD）”级别**：我们把 `Device / Teacher / Student / Classroom / Group / Session / Network / Discovery / Pairing / Command / Stream / Policy / Capability` 全部定义成领域模型，再画出**进程架构、网络协议、消息格式、状态机、数据库 ER 图、Windows Service/User Agent 边界，以及 Wi-Fi/有线/VLAN/多教师场景的完整时序图**。这一步完成后，基本就可以开始建立 GitHub 仓库和第一版 `v0.1` 的代码骨架了。
<!--
LocalMesh 产品原始设计稿。
正式的可执行规范位于 docs/，协作与代码约定位于 AGENTS.md。
当前实现入口：docs/ROADMAP.md 的 Phase 0。
-->

# LocalMesh 文档入口

这份文档保留完整的产品思考和场景推演。开始实现前请先阅读：

- [AGENTS.md](AGENTS.md)：仓库协作、代码边界和验证门槛
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)：进程、分层、网络和安全架构
- [docs/DOMAIN-MODEL.md](docs/DOMAIN-MODEL.md)：核心实体与命令生命周期
- [docs/ROADMAP.md](docs/ROADMAP.md)：阶段目标与验收标准
- [docs/DECISIONS.md](docs/DECISIONS.md)：已确认的架构决策
- [.agents/skills/architecture-planning/SKILL.md](.agents/skills/architecture-planning/SKILL.md)：架构规划 agent skill
- [docs/PROTOCOL.md](docs/PROTOCOL.md)：控制面消息、版本和错误码
- [docs/SECURITY.md](docs/SECURITY.md)：信任边界、密钥、授权和威胁模型
- [docs/DATA-MODEL.md](docs/DATA-MODEL.md)：SQLite 表、约束和事务边界
- [docs/NETWORK-MATRIX.md](docs/NETWORK-MATRIX.md)：Wi-Fi、有线、VLAN 和隔离场景
- [docs/TESTING.md](docs/TESTING.md)：测试层级、故障注入和 Phase 0 验收
- [docs/OPERATIONS.md](docs/OPERATIONS.md)：运行状态、观测指标和恢复语义

当前里程碑是 **Phase 0：Network Foundation**，先完成设备身份、发现、配对、认证会话、心跳和断线重连，再进入控制和屏幕能力。
