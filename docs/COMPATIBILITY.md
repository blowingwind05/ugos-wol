# 平台与兼容性

## 支持范围

| 层次 | 范围 |
| --- | --- |
| 运行平台 | 支持原生 UPK 的绿联 UGOS Pro NAS，`nasync` 产品线 |
| 架构 | 源码分别构建 Linux `amd64`（x86-64 基线）与 `arm64`（ARMv8.0 基线） |
| 固件 | 配置声明最低 `1.13.0.0000`，实际兼容性取决于系统网关、SDK 和应用隔离环境 |
| 账号 | 仅管理员；同一 NAS 的管理员共享配置与记录 |
| 目标 | 支持标准 WOL 魔术包的局域网设备；不以品牌、系统或设备外形区分 |

官方平台支持和安装包架构规则见 [绿联开发准备](https://developer.ugnas.com/en/doc/backend/quick-start/prepare.html) 与 [产品线说明](https://developer.ugnas.com/en/doc/backend/application/product-series.html)。源码支持两种架构不等于已经覆盖所有机型或固件。

已有 amd64 NAS 的实际接入与启动验证记录。ARM64 需经过真实硬件上的安装、身份校验、网络接口识别和发包验证后，才能宣称实机兼容。

## 身份校验

前端使用官方 SDK 的 `getThirdToken`，只提交 `Ugreen-Ttk`。系统网关验证后注入用户 ID、用户名与角色，后台在每个 API 请求上验证管理员权限；仅接受本机回环来源，不信任转发来源头。

部分固件不提供 `Ugreen-User-Type`。兼容逻辑仍要求完整、经过网关认证的用户 ID 与用户名，再读取系统 `admin` 组进行精确成员匹配；不会把缺少角色的用户直接视为管理员。

如果旧固件提示无法读取管理员组，可在已安装应用的 NAS 上，由管理员检查并运行：

```bash
sudo bash scripts/enable-legacy-auth.sh
```

脚本只为 `com.local.wol.service` 添加 `/etc/group` 只读绑定，不修改组成员或应用运行用户。正常提供角色字段的固件不需要该步骤。系统升级或管理员组成员变更后，应重新验证应用权限；此机制不是对所有固件的替代认证方案。

认证与隔离环境依据：[登录认证](https://developer.ugnas.com/doc/backend/system-capabilities/login-auth.html)、[运行环境](https://developer.ugnas.com/doc/backend/application/runtime-environment.html)。

## Go 工具链

当前构建固定为 Go 1.26.11。已验证的某些 UGOS 环境会注入旧的 `GODEBUG=tlskyber=0`，较新的工具链可能在应用启动前拒绝已移除的选项。升级工具链前应重新验证原始固件环境，不应要求所有用户修改 NAS 全局设置。

## 网络与唤醒

- 动态枚举运行中的 IPv4 广播接口，不固定 NAS 地址、子网、MAC 或网口名。
- 排除回环、无广播能力的隧道及常见容器虚拟接口；保留常规 LAN 网桥、聚合接口和 VLAN 接口。
- 自动模式可能使用多个接口。需要限定广播出口时，选择具体接口。
- 不跨路由或 VLAN 中继广播，不提供任意目标 IP 发送、在线扫描或开机确认。
- 目标硬件的供电、驱动和电源状态决定是否能被唤醒。标准 WOL 不覆盖所有设备厂商的专有唤醒功能。

应用只声明网络访问权限，后台以系统分配的独立用户运行，数据位于 `UGAPP_DATA_DIR/state.json`。不依赖特定 NAS 用户名、目录卷号或个人设备。
