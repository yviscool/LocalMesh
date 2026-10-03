# LocalMesh 安全架构

## 信任边界

```text
Untrusted LAN
  -> Discovery candidate
  -> Pairing approval boundary
  -> Authenticated session
  -> Classroom authorization
  -> Privileged Service boundary
  -> Windows capability
```

局域网被视为不可信网络。任何同网段设备都可能伪造广播、重放旧消息、耗尽连接或尝试加入错误课堂。

## 身份与密钥

- `DeviceID` 是标识符，不是秘密；设备密钥必须单独存储。
- 设备首次安装生成不可导出的随机密钥对，私钥只由 Windows Service 使用。
- Pairing code 只用于短期建立信任，不能直接作为长期凭据。
- 会话使用 TLS 证书/公钥绑定和短期 session token；token 不写入日志。
- TLS 握手后仍执行设备 challenge-response：服务端发送一次性随机 nonce，设备使用绑定私钥签名，服务端用已登记公钥验证；nonce 过期或重复使用必须拒绝。
- 公钥指纹用于设备注册和审计关联；指纹不是秘密，私钥永不离开 Windows Service。
- Teacher 身份与设备身份分离；换电脑登录不应生成新教师。
- 密钥轮换、撤销和重新配对必须留下审计事件。

## 授权

授权判断顺序固定为：

```text
authenticated session
 -> classroom membership
 -> role
 -> capability
 -> target policy
 -> command-specific confirmation
```

默认拒绝。`power.shutdown`、`input.control`、`process.kill`、文件收集等高风险能力需要更严格的 role/capability 和可选二次确认。

## STRIDE 风险与控制

| 风险 | 示例 | 控制 |
| --- | --- | --- |
| Spoofing | 伪造学生 Agent | 密钥绑定、TLS、配对审批 |
| Tampering | 修改命令目标或 payload | 加密传输、消息认证、schema 校验 |
| Repudiation | 教师否认关机 | 不可变审计事件、操作者/目标/结果 |
| Information disclosure | 课堂屏幕泄露 | classroom scope、最小权限、数据面单独授权 |
| Denial of service | 广播/配对洪泛 | 速率限制、大小限制、连接上限、退避 |
| Elevation of privilege | User Agent 伪造 Service 请求 | named pipe ACL、Service 端再次鉴权 |

## 安全默认值

- 不监听 `0.0.0.0`，除非明确选择并记录网卡/端口。
- 所有输入有最大长度、最大嵌套深度和截止时间。
- 文件操作限制在配置的根目录，拒绝路径穿越、符号链接逃逸和压缩炸弹。
- 日志只记录 ID、状态、耗时和错误码，不记录 token、私钥、屏幕帧和完整文件内容。
- 失败关闭：认证、授权、解密或策略解析失败时不执行操作。
