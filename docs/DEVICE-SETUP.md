# 设备准备指南

本应用发送标准 WOL 魔术包，不依赖目标设备的品牌或操作系统。目标网卡、固件和当前电源状态必须支持这种唤醒方式。

## 通用准备

1. 在设备固件、管理界面或操作系统中开启 WOL / Wake on Magic Packet。
2. 获取实际用于唤醒的网卡 MAC。多网卡设备应确认对应的物理接口，避免使用容器、隧道或随机化地址。
3. 让目标网卡在待机状态下保持供电。无线唤醒、USB 网卡及扩展设备需按厂商说明验证。
4. 让 NAS 与目标网卡处于同一广播域。访客网络、AP 隔离、不同 VLAN 或路由可能阻挡广播。
5. 从厂商支持的电源状态开始测试。睡眠成功不代表休眠或关机也能唤醒。

## 按设备系统查找 MAC

### Windows

在 PowerShell 查看网卡名称、MAC 与状态：

```powershell
Get-NetAdapter | Select Name,MacAddress,Status
```

在 BIOS/UEFI 及网卡属性中检查网络唤醒设置。电源状态、快速启动、驱动和固件都会影响结果；参考 [Microsoft WOL 行为说明](https://learn.microsoft.com/en-us/troubleshoot/windows-client/deployment/wake-on-lan-feature)。

### Linux

查看接口及 MAC：

```bash
ip -br link
```

可使用 `ethtool <网卡名>` 检查驱动报告的唤醒能力，并结合发行版的网络、电源管理工具配置魔术包唤醒。NetworkManager 提供对应的 [wake-on-lan 设置](https://networkmanager.dev/docs/api/latest/settings-802-3-ethernet.html)。接口名不固定，不应直接套用其他设备的网口名称。

### macOS

查看硬件端口及 MAC：

```bash
networksetup -listallhardwareports
```

部分机型在电池或节能设置中提供“唤醒以供网络访问”。其可用性、电源条件与工作方式依机型而异；该设置不能作为任意电源状态下标准 WOL 可用的保证。参考 [Apple 网络访问唤醒说明](https://support.apple.com/en-gb/guide/mac-help/mh27905/mac)，并对目标机型实测。

### 其他设备

工作站、服务器、网络存储及其他网络设备可从管理界面或厂商文档获取 MAC 和 WOL 设置。IPMI、BMC、厂商云端开机或专有网络唤醒协议与标准 WOL 不同，不在本应用的协议实现范围内。

## 添加与验证

添加设备时填写名称、目标网卡 MAC 和发送接口，无需填写目标设备 IP。自动模式向所有符合条件的局域网接口广播；多网段环境可指定与目标设备同一广播域的接口。

每个选中接口向其 IPv4 子网广播地址的 UDP 9 端口发送三个标准魔术包。发送结果只代表本地发包情况；目标是否启动，请通过目标设备或对应的远程管理工具确认。

完成局域网测试后，可通过绿联远程访问打开 NAS 上的应用，再验证外网触发。远程桌面、SSH、设备管理界面和游戏串流需分别配置其连接方式。
