# 远程唤醒 · UGOS WOL

当前工作副本仍为 **0.2.4**，包含恢复左上角标题、左下角仅显示版本号和缩小底边距的调整；这些调整尚未重新打包，现有 `0.2.4.0001` 安装包不包含它们。

面向绿联 UGOS Pro 的 x86_64（amd64）原生应用。利用绿联远程访问，以 NAS 为中继，从外网唤醒家中的电脑。本应用通过 NAS 发送 WOL（网络唤醒）魔术包，唤醒与 NAS 同一局域网内的兼容设备。电脑启动后，可搭配已配置好的 UU 远程或 Sunshine + Moonlight，进行远程办公与游戏串流。本应用不用于唤醒 NAS 本身。 **不需要目标设备的 IP 地址，只需要网卡 MAC。**

包含中文桌面/手机界面、设备管理、网段自动识别、发送接口选择、一键唤醒、最近 100 条发送记录。采用 Go 静态可执行程序，无需 Docker、Python 或 Node.js 运行环境。

## 使用场景

出门在外需要使用家中电脑时：

1. 通过绿联远程访问，打开 NAS 上的「远程唤醒」。
2. 由 NAS 向局域网内的目标设备发送 WOL 魔术包，等待电脑启动。
3. 使用已配置好的 UU 远程，或 Sunshine（电脑端）+ Moonlight（客户端），连接电脑进行远程办公或游戏串流。

本应用负责由 NAS 发出唤醒包；远程控制或串流连接由对应软件提供，其网络访问需预先配置。
Sunshine 与 Moonlight 的配合方式见 [Sunshine 官方说明](https://docs.lizardbyte.dev/projects/sunshine/latest/)。

## 安装与使用

1. 下载 `amd64_nasync_com.local.wol_0.2.4.0001.upk`，在 UGOS「应用中心」选择手动安装并上传。已安装旧版时选择更新，保留应用数据。仅适用于 x86_64 NAS，声明最低固件为 1.13.0.0000。
2. 用 NAS 管理员账号登录 UGOS 或绿联客户端，从应用图标打开「远程唤醒」。应用自动使用当前身份，无需创建账号或设置唤醒密码。
3. 进入设备列表，点击「添加设备」，填写名称和网卡 MAC。无需笔记本 IP。
4. 发送接口默认自动识别，多网口 NAS 可指定与笔记本处于同一局域网的接口。
5. 先让接电、有线联网的笔记本进入睡眠并测试；成功后，再从外网经绿联云打开应用验证。

旧版设置的 `WOL_PASSWORD` 在新版不再读取，也不参与认证。设备配置及旧发送记录兼容，旧记录会显示未记录操作人。

应用使用 `inner` 桌面窗口、绿联官方 SDK 和 `api/wol` 网关路径，支持 UGREENlink 远程访问。0.1.3 已经真实 UGOS 会话验证；新版 0.2.0 通过浏览器、后端及 NAS 独立进程检查，仍需应用中心更新后的窗口与外网验证。

**自开发应用安装权限：**绿联官方开发流程要求测试设备获得开发者授权。如果系统拒绝安装自开发 UPK 或提示缺少授权，请按官方准备文档完成授权。这是系统要求，不能仅靠更改 UPK 文件绕过。此包为个人开发版本，未上架应用商店。

## 笔记本准备

- BIOS/UEFI 中启用 Wake on LAN；Windows 网卡驱动中启用 Wake on Magic Packet，并允许网卡唤醒计算机。具体选项由设备厂商提供。
- 优先使用支持 WOL 的有线网卡。Wi-Fi 唤醒需要额外支持 WoWLAN；USB 网卡和扩展坞是否在睡眠/关机后继续供电也需确认。
- NAS 与目标网卡位于同一广播域。访客网络、AP 隔离、不同 VLAN 通常无法直接接收本应用的本地广播。
- 睡眠、休眠、关机是不同电源状态，不能因睡眠测试成功就认定关机也能唤醒。Windows 的快速启动、固件和驱动设置会影响结果。

Windows 查询 MAC：

```powershell
Get-NetAdapter | Select Name,MacAddress,Status
```

界面显示「唤醒包已发送」只代表 NAS 成功执行 UDP 发送。WOL 没有接收确认，因此本应用不会把发包成功显示成「电脑已开机」。

## 实现与数据

- 应用 ID：`com.local.wol`。运行端口：`127.0.0.1:28089`，由 UGOS 网关代理；不直接向 LAN 暴露后台端口。
- 不设置独立密码。前端从官方 SDK 的 `getThirdToken` 取得当前会话令牌，只发送 `Ugreen-Ttk`，不自行声称用户 ID 或角色。
- 系统网关验证令牌后，向后台注入 `Ugreen-User-ID`、`Ugreen-User-Name`、`Ugreen-User-Type`。后台要求身份字段完整且角色为 `admin`；缺失身份返回 401，普通用户返回 403。某些固件不传角色字段时，0.1.3 会改查系统 `admin` 组，以经过网关认证的用户名进行精确成员匹配。
- 后台仅接受回环来源并禁止绑定非回环地址，不信任 `X-Forwarded-For` 作为来源依据。该方案依赖 UGOS 网关覆盖并验证身份头，遵循官方原生应用的本机信任边界；不对本机其他程序建立额外密码屏障。
- 设备列表由 NAS 管理员共享，发送记录保存当次用户 ID、用户名及角色。这里的“绑定”是认证与操作归属，不是每个管理员隔离一份设备配置。
- 使用官方 `@ugreen-nas/core` 1.7.42 获取 `Ugreen-Ttk`，前端资源均在包内，无外部 CDN。
- 每个被选中的局域网接口向其子网广播地址的 UDP 9 端口发送 3 个标准 102 字节魔术包，间隔 120ms；两次操作至少间隔 3 秒。
- 不运行系统 shell 命令，不需要 root、原始套接字权限或 Docker 特权。只声明网络访问权限。
- 自动排除 Docker 接口和 libvirt 的 `virbr` 网桥；无广播能力的隧道接口不会被选中。其他特殊虚拟接口可通过手动选择物理网口避开。
- 设备和记录保存在 `UGAPP_DATA_DIR/state.json`，采用临时文件同步后原子替换。记录上限 100 条；损坏的原配置不会自动覆盖。**卸载会删除应用数据**，需要保留时先备份该文件。
- 登录过期时，前端尝试刷新令牌；若仍无效则隐藏操作界面，提示重新登录 NAS。应用没有独立的登录/退出账号体系。

## UGOS 启用兼容性

0.1.2 改用仍受维护的 Go 1.26.11 构建，并固定构建脚本的工具链检查。实际 NAS 的 systemd 管理器会向原生应用注入 `GODEBUG=tlskyber=0`；Go 1.27 会在进入应用代码前因这个已移除的旧选项退出。0.1.2 已在该原始环境参数下验证启动，不要求修改 NAS 全局环境。

本次设备已对 `com.local.wol.service` 添加专属兼容设置 `Environment=GODEBUG=`，并在实际 UGOS 沙箱中成功启动原先安装的 0.1.1。该设置位于 `/etc/systemd/system/com.local.wol.service.d/90-wol-go-compat.conf`，只影响本应用，设备配置和发送记录保留。应用已安装后的升级应由应用中心完成。

## 旧固件的身份角色兼容

实际 UGOS 1.21.1.0016 的网关会提供经过认证的 `Ugreen-User-ID` 和 `Ugreen-User-Name`，但不提供 `Ugreen-User-Type`。0.1.3 在这种情况下读取系统 `admin` 组确认管理员身份；不接受仅有用户名的请求，也不会把普通用户自动视为管理员。显式的非管理员角色仍然拒绝。

此 NAS 已完成一次性兼容设置：只为本应用的沙箱添加 `/etc/group` 只读绑定，配置位于 `/etc/systemd/system/com.local.wol.service.d/91-wol-legacy-auth.conf`。不会修改组成员或给应用 root 权限。管理员组成员调整后，应重启本应用以刷新文件绑定。

在其他同类旧固件设备首次安装时，若提示无法读取管理员组，可由 NAS 管理员在源码目录执行 `sudo bash scripts/enable-legacy-auth.sh`。新固件正常提供角色字段时，不需要这一步。

## 源码与构建

```text
backend/                    Go 后台与测试，web/ 为已构建前端
frontend/                   HTML/CSS/JS 源码，依赖与锁文件
packaging/project.yaml      UGOS 原生应用配置
packaging/rootfs_common/    图标和前端静态文件
packaging/rootfs_amd64/bin/  Linux amd64 可执行程序的构建输出
scripts/pack-prebuilt.sh    在 Linux 上打包已编译程序
scripts/build-upk.sh        重新编译、测试并打包
docs/VALIDATION.md          验证范围与未验证项目
```

Git 仓库不跟踪依赖、运行数据、编译程序或 UPK。首次从 Git 检出时，使用下方的源码编译流程；已经生成可执行程序后，可使用预编译打包流程。已构建的前端静态资源保留在仓库中，便于 Go 内嵌页面和离线打包。

只打包已经生成的可执行程序，无需安装 Go/Node/Docker：在 **x86_64 Linux** 安装官方 `ugcli` 1.1.0.25，然后执行：

```bash
UGCLI_TOOL=/absolute/path/to/ugcli BUILD_NUMBER=1 bash scripts/pack-prebuilt.sh
```

源码重新编译：准备 Go 1.26.11、官方 ugcli；需要修改前端时再准备 Node.js/npm。

```bash
# 默认使用已构建的前端
BUILD_NUMBER=2 bash scripts/build-upk.sh

# 修改前端后重新打包
REBUILD_UI=1 BUILD_NUMBER=2 bash scripts/build-upk.sh
```

正式 UPK 必须使用官方工具在 Linux 上生成。打包完成后自动将 UPK 移至当前用户的 `~/Downloads/`，也可通过 `UPK_OUTPUT_DIR` 指定用户目录中的其他交付位置。同一版本重复发布时递增构建号。源码包中没有 NAS 凭据、SSH 密钥、设备真实 MAC 或测试数据。

## 官方参考

- [UGOS 原生应用开发](https://developer.ugnas.com/en/doc/backend/quick-start/develop.html)
- [开发准备与设备授权](https://developer.ugnas.com/en/doc/backend/quick-start/prepare.html)
- [应用端口与绿联云访问](https://developer.ugnas.com/en/doc/backend/application/app-port.html)
- [登录鉴权与 SDK 接入](https://developer.ugnas.com/en/doc/backend/system-capabilities/login-auth.html)
- [运行环境与数据目录](https://developer.ugnas.com/en/doc/backend/application/runtime-environment.html)
- [ugcli 下载及使用](https://developer.ugnas.com/en/doc/tools/ugcli.html)
- [Microsoft：电源状态与 WOL 行为](https://learn.microsoft.com/en-us/windows/win32/power/system-power-states)
