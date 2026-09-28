# Hysteria2 自动端口跳跃转发

此分支将网页中的 Hysteria2 `UDP Hop / Hop Ports` 同步为服务器 nftables NAT 规则。
网页配置是唯一数据源，无需为每个入站再次执行端口转发命令。

## 使用

1. 在运行 Xray 的 Linux 主机上安装此分支编译的面板，确保 `nft` 可执行文件在面板服务的 PATH 中。
2. 面板进程需要 `CAP_NET_ADMIN`，通常由原生 root 服务提供。需要支持 inet NAT 的 Linux 内核（5.2 或更新版本）。
3. 新建或编辑 Hysteria2 入站，保留实际监听端口，例如 `443`；开启 `UDP Hop`，填写 `20000-30000,40000-50000`，保存。
4. 客户端重新获取订阅或分享链接，取得新的跳跃范围。

如主机尚未安装 nftables，Debian/Ubuntu 可使用 `apt install nftables`。无需添加规则文件，也无需启用 nftables 的持久化服务；面板自行同步。

## 行为

| 操作 | 自动行为 |
| --- | --- |
| 新建或修改入站 | 根据已提交的数据库配置，整体替换专用 NAT 表 |
| 改变实际监听端口 | 修改转发目标 |
| 关闭 UDP Hop、禁用入站、删除入站（含批量删除） | 移除对应规则 |
| 重新启用入站 | 恢复对应规则 |
| 面板启动 | 立即恢复规则 |
| 防火墙重载、规则被清空、定时任务禁用入站 | 最迟下一次 30 秒检查时同步 |
| nft 执行失败 | 网页明确说明配置已保存但转发失败，后台继续重试并记录日志 |

仅管理 `inet xui_hysteria_hop` 表，不调用 `flush ruleset`，不修改其他防火墙表。替换在一次 nft 事务中完成；执行失败不会先删掉已有规则再留下空窗。所有 nft 调用均有 10 秒超时，不通过 shell 执行网页输入。

端口范围必须在 1–65535 内，支持逗号分隔、单端口和闭区间。重复、相邻范围会合并。保存时拒绝同一监听范围内的两个活动 HY2 跳跃范围重叠，以及跳跃范围覆盖面板中其他活动 UDP 入站监听端口；TCP 端口不受影响。新增、修改、启用的校验在面板串行数据库事务中执行。

兼容 `finalmask.quicParams.udpHop.ports` 和 `finalmask.udp[].settings.remotePorts`，后者优先。`udphop` 是 Xray 客户端专用 mask，生成 HY2 服务端配置时会移除它，数据库和订阅所需配置保持完整。

## 支持范围与边界

- nftables 后端；本版本未实现 iptables fallback。
- IPv4、IPv6、双栈通配监听，以及具体 IP 的目的地址匹配。IPv6-only 遵循 `sockopt.v6only`。不支持带 hopping 的 loopback、域名或 Unix socket 监听。
- 只处理当前主机 `node_id IS NULL` 的入站。主面板不会替远程 Node 配置本机 NAT；远程节点必须也部署此分支，才能在本机保存镜像入站后自动同步。
- Docker 仅支持面板与 Xray 位于相同、正确的网络命名空间并具备权限的部署；推荐 host 网络。不会自动配置 Docker 宿主机端口发布。
- 不修改云安全组、上游路由器、UFW/firewalld 的过滤策略。外部防火墙需允许客户端使用的 UDP 跳跃范围；本机 INPUT 过滤看到的目的端口已经转换为实际监听端口，该端口也必须允许。
- 冲突检测覆盖面板数据库中的入站，不检测面板之外的其他程序、辅助动态端口或其他 NAT 规则。选择范围时仍需避开这些服务。
- 禁用或修改规则不会主动清理已有 conntrack 状态；已建立 UDP 流可能持续使用旧映射直到超时。
- 停止面板不删除表，以免中断独立运行的 Xray；最后一个 hopping 入站被关闭或删除时会删除表。卸载或回退到不支持此功能的版本时，可执行 `nft delete table inet xui_hysteria_hop` 清理。
- 同一网络命名空间只允许一个面板实例管理这张表。

## 验证

```bash
go test ./internal/porthop ./internal/database/model ./internal/web/service -count=1
go test -race ./internal/porthop ./internal/web/service -run 'TestHysteriaHop|TestParse|TestFromStream|TestScriptAddress|TestApply' -count=1
nft list table inet xui_hysteria_hop
```

自动化测试使用替代 nft 执行器验证事务、清理、错误传播、超时和数据库生命周期；并不替代真实服务器上的 UDP 连通性验收。部署后应从另一台主机验证两个不同跳跃端口，再验证修改范围、禁用和服务器重启的行为。
