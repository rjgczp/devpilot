# Design

## Context

动机和范围见 proposal。以下是 2026-10-19 的只读发现，而非本轮实施或测试结果：

- main/HEAD/本地 origin/main/连接器读取的远端 main 均为 `252c7d3c000c68de3e08d2fc8de4cece3a2d00e8`；初始无未提交工作，八个归档完整，初始无 active Change。只创建本 Change 文档，不切换/重置仓库、不覆盖工作。
- `go.mod` 为 Go1.24.3、pgx/v5 v5.8.0、x/net v0.49.0；本机工具链也为 go1.24.3。Goose 由现有 CI 固定为 v3.26.0，PG 镜像为固定 digest 的 PostgreSQL18。
- `migrations/00006_create_http_monitors.sql` 含 GET 配置、1–30秒 timeout、固定 interval 枚举、单 expected_status、bigint version、复合 `(workspace_id,project_id)` FK 和三元组唯一键；不存在状态、任务、结果。Migration6 SHA256 见 proposal，Migration1–5 与现有哈希守护匹配。
- `internal/monitor/url.go` 是纯离线规范化：ASCII/punycode DNS 名、仅 HTTP80/HTTPS443、禁止 IP literal/userinfo/query/fragment 等；域名可以在保存后解析为危险 IP。path 仍可能含秘密。
- `internal/monitor/store.go` 的用户读取绑定 Cookie 身份和完整 scope；写事务使用 User SHARE → Session SHARE → Workspace NO KEY UPDATE → Membership UPDATE → Project UPDATE → Monitor UPDATE，保护等待后再以新语句检查时效与实际 DML。PATCH 有 If-Match/version，不能加入目标网络 I/O。
- `cmd/api/main.go` 当前只注册 Session 清理 Worker。`internal/app/runner.go` 的通用 Workers 槽不代表已存在探测入口；本轮不得把新执行器放进去。
- `internal/monitor/network_test.go` 有配置包 import/call 守护；`tests/network/` 有独立 Linux syscall 审计、恶意正向校准、CGO=0 构建来源检查和真实配置 API 零目标尝试。现有审计只准精确 PG/本地 HTTP tuple，不能扩成执行器网络白名单。
- 已完整阅读 http-monitor-management、http-monitor-persistence、workspace-access 的相关要求及场景。workspace-access 中第七/八阶段范围是历史验收边界；本轮无业务 schema/API 扩展，不更改它。只修改 persistence 最后一项对未来执行器的阶段门禁，保留第八阶段历史证据含义。

## Goals / Non-Goals

**Goals:** 一个可复用但未接入生产入口的内部单次执行内核，具有可核验的网络/资源/快照契约；将真实网络与网络隔离证明作为独立验收对象。A 可以独立完成“读取特定版本配置→安全执行最多一次→返回该版本内存结果”的闭环，不依赖调度与结果持久化。

**Non-Goals:** 本轮不提供持续监控产品体验、生产执行二进制、公开/管理员 HTTP trigger、CLI 探测命令、cron、运行状态或结果 API。明确排除 Incident/Alert、通知/邮件/Webhook、AI诊断、前端/看板、计费套餐、任意方法/body、自定义 Host/header/认证、内网监控、历史自动激活、Redis/MQ、无关认证/Workspace 重构。只读最终响应头的成功不表示响应体完整、内容正确或可持续健康。

## Decisions

### D1. 推荐 A，并把 B 保留为后续独立 Change

| 维度 | A：单次执行器（推荐） | B：激活＋周期调度＋Worker |
| --- | --- | --- |
| 独立价值 | 在受控真实 DNS/TCP/TLS 上证明关键安全内核；后续直接复用 | 可交付周期监控，但必须同时证明安全内核和分布式状态机 |
| 本轮数据/API | 无 Migration/新 API/结果表；内存结果 | 至少 Migration7、新激活/暂停 API、任务/租约/结果和系统权限 |
| 网络风险 | 无生产调用路径，测试拓扑完全隔离 | 用户持续控制出站，滥用、成本、租户公平和取消风险增加 |
| 并发成本 | 只读快照＋单进程有界 admission，无业务写入 | 多实例领取、租约/代次、过期 reclaim、重复执行/未知提交、结果版本发布 |
| 验收成本 | 单次网络攻击矩阵、PG快照、资源和出口证明 | 再增加崩溃/重启/长暂停/时钟/重复领取/多实例公平/积压和迁移全矩阵 |

A 的“生产级”指执行内核按生产安全约束设计和验收，不表示可公开运行服务。没有出口保证不得给出“生产 SSRF 安全”结论。A 不使用 interval 做定时调度，interval 仅约束受控调用的最小间隔。

未选择的 B 最小设计成本（仅供后续决策，不是本轮实现任务）：

- runtime 状态以完整 scope 引用 Monitor，默认 disabled，显式 desired_state、activation_configuration_version、activation_generation、next_due_at；分离 configuration version 与运行代次。旧行全部 inactive，不能依靠“已有配置”推导意图。
- 当前 Workspace Owner 以强条件明确激活、暂停/停止；Member 只读。停止等价暂停并清掉未来 due，不删除配置。任意配置版本变化使旧激活失效；URL 改动尤其必须重新确认新版本。Owner 意图属于 Workspace，用户退出/Session撤销不自动撤销已经确认的系统运行意图，若需不同语义须另审。
- 内部 Worker 使用独立服务身份/最小数据库权限，无 Cookie。按有界批量扫描到期项（如最多32）、短事务 `FOR UPDATE SKIP LOCKED` 领取，持久化 run_id、配置版本/必要快照、lease_owner、lease_until 和 fencing generation，确认提交后才发送；数据库锁绝不跨 HTTP。
- 租约需覆盖执行上限加提交余量，过期可回收；结果按完整 scope/run_id/代次幂等写入，陈旧 fencing 不能发布为当前结果。租约无法撤回已发出的 HTTP；崩溃在“已发出、未记结果”之间时重领可能重复，不能宣称 exactly-once。若选择至少一次尝试，明确允许重复 GET，终端重复结果可去重；GET 也可能在恶意目标产生副作用。
- claim Commit 未知时不发送、不立刻重放，等待可确认状态/租约回收；重启扫描只为每 Monitor 安排一个当前 due，不补历史 N 次任务。失败退避有上限和 jitter；暂停/版本变化取消本地匹配代次并禁止后续领取，无法保证撤回远端已收到的请求。
- 分布式并发/速率不能直接复用 A 的进程内 gate 冒充全局上限。需另选 PG 配额/领取协议并验证跨实例资源租约、租户公平、锁序及权限。Migration7、结果保留/清理、故障注入和新的激活 API 验收共同扩大范围，不在本次“顺手加入”。

### D2. 信任边界与剩余威胁

不可信：Monitor 的全部字段、DNS 所有 RR、CNAME、目标 HTTP/TLS、重定向、响应 headers/body、运行环境代理变量、调用时已经过时的配置。配置 API 的准入不构成目的网络授权。

可信计算基：审核代码/固定依赖、受保护内部调用者、运营指定的固定 IP 递归 DNS、操作系统路由/TLS roots、独立网络命名空间及出口防火墙。域名拥有者即使合法签名 DNS，也能回答私网；DNSSEC 不消除 SSRF。resolver 的正确响应只作为候选，任何目标 IP 仍需分类。

```text
受控测试 controller（受保护PG18；只读系统snapshot）
       │ 不可变配置＋完整scope/version（无Cookie/DB凭据）
       ▼
独立无特权 probe test process（有界admission）
       ├─ 固定resolver IP:53/TCP → 有界A/AAAA/CNAME → 完整分类
       └─ 已批准literal IP:80/443 → 核对RemoteAddr → TLS原域名 → GET
                   │ namespace/出口策略是独立最后防线
                   ▼
              受控HTTP/TLS fixture；无通向真实网络的路由
```

测试 controller 与 probe 子进程之间是受信 pipe/进程生命周期，没有监听端口。不能把传入 scope 或系统 actor 字符串当成调用者认证。A 的身份边界是受控测试进程/私有模块调用，不提供可从互联网获得该能力的入口；测试 fixture builder、CA、可替换 resolver/transport 只存在测试代码，生产构造路径不能接受用户注入这些对象。

防御目标是禁止向非批准地址建立目标连接/发送 payload，限制对公网目标的资源消耗。OS/防火墙被控制、公共服务自行转发到其内网、全网 DDoS 防护及任意运营网络拓扑无法由此库单独保证。公开服务上线必须另行验收隔离和激活/滥用策略。

### D3. DNS：可观测的完整答案、有界查询、任何危险即拒绝

拒绝仅用 `LookupIPAddr` 后再单独 `LookupCNAME` 拼接“完整链”的方案：两个调用不能证明同一观察的别名链，高层 API 也不暴露完整 DNS 消息。选用现有 x/net/dns/dnsmessage 实现窄范围 wire adapter，不增加 DNS 依赖；该适配器仅是递归 resolver 客户端，不实现递归 DNS 服务器。

1. 只向运营指定的一个 literal IP:53 发 TCP DNS，固定 resolver 不由 Monitor/环境搜索域决定；不使用系统 resolver、hosts、mDNS、NSS、search suffix、DoH、SRV/HTTPS/SVCB 或隐式 nameserver failover。测试 resolver 是独立 namespace 的确切 tuple。resolver 的基础设施私有地址是专用通道例外，绝不进入目标拨号白名单。
2. 使用原规范域名的绝对 FQDN（尾随点），查询 A 与 AAAA。验证 TCP 长度、随机 transaction ID、唯一 question 的 name/type/class、QR/opcode/RCODE、完整报文、无 TC/解析歧义；不接受部分读取的答案。每次执行最多一条 DNS TCP 连接，无失败重连。
3. 检查两个家族报文中的全部 A/AAAA 以及 CNAME。链必须无环、每 owner 单一且无冲突、A/AAAA 的链与终端名一致，不允许 CNAME owner 同时作为地址 owner。别名继续遵守域名安全语法/保留名策略。缺少终端 RRset 时查询该终端，两家族完整结束后才允许目标拨号；不取先到达家族抢跑。
4. 只采用正确链终端、相应 question 返回的 IN A/AAAA；additional/glue 不能作为目的地址。保守检查所有接收 section 中的 A/AAAA，有任何危险 IP 整次 blocked_target，即使另有安全答案。无关 RR 不用于连接，解析/计数仍受预算限制；遇不支持的 DNAME/矛盾链 fail closed。
5. 一家族明确 NOERROR/NODATA 而另一家族有完整安全地址可执行。两家族都 NODATA/空、NXDOMAIN、SERVFAIL/REFUSED、格式错/截断/链不一致或任一家族失败均 dns_failed；任一家族超时为 timeout（内部 phase=dns）。不能将超时当作“没有IPv6”，也不使用缓存旧好答案。
6. 最大8次 CNAME 跳转、18个 query/response、单报文16KiB、总DNS消息64KiB、总RR128、终端去重地址32；超限拒绝，不截成前32个“安全”IP。整个 DNS 阶段共享2秒硬预算，不能每次 query 重置2秒。TCP framing/报文长度先校验再分配。
7. 安全答案去重后按稳定规则选择一个：IPv4 优先、同家族按地址升序；IPv6-only 同样支持原生 IPv6。本轮一次只尝试一个 IP，不 happy-eyeballs、不失败换 IP、不重试。故可有可用性损失，后续新的一次受控调用必须完整重解析。

“完整”是该固定 resolver 在本次有界 A/AAAA/CNAME 观察中给出的全部相关记录，不声称穷举权威服务所有轮转地址、地理视图或形成跨 DNS 查询的全局原子快照。域名可在下一毫秒改变；安全依靠当前获准 IP 的连接绑定，不依靠 DNS 不变。TTL=0 可用于本次立即绑定，不跨调用缓存/复用批准票据。两家族查询观察不一致一律失败，不能拼装有利答案。

### D4. IP 策略：普通公网候选才允许，特殊用途默认拒绝

采用版本化、代码内审核的 prefix 表，来源为 IANA IPv4/IPv6 Special-Purpose 注册表，并覆盖 multicast/reserved 及运营环境 deny CIDR。普通公网候选还需落在固定IANA分配数据认可的已分配空间；这里的“已分配”仅指该注册表层级，不能证明下级RIR已委派、BGP可达或网络目的安全。Apply 固定来源日期/内容哈希及测试向量；运行时不下载更新，不信任 `Globally Reachable=true` 自动豁免特殊用途。新增规则收紧可审查发布；注册表更新需定期人工审核，不能以表过时仍宣称完整生产保证。[IANA IPv4](https://www.iana.org/assignments/iana-ipv4-special-registry/)、[IANA IPv6](https://www.iana.org/assignments/iana-ipv6-special-registry/)、[IPv4分配表](https://www.iana.org/assignments/ipv4-address-space/)、[IPv6单播分配表](https://www.iana.org/assignments/ipv6-unicast-address-assignments/)

- IPv4：有效原生地址且不在全部特殊用途/保留表或运营 deny；覆盖至少0/8、RFC1918、100.64/10、127/8、169.254/16、192.0.0/24、192.0.2/24、192.88.99/24、198.18/15、198.51.100/24、203.0.113/24、224/4、240/4。共享100.64/10同时覆盖100.100.100.200元数据端点，不能只封169.254.169.254。未知/未分配空间也不作为普通公网候选；Apply固定分配数据版本与prefix测试。
- IPv6：只考虑原生2000::/3且排除特殊用途和运营 deny；拒绝ULA、link/site-local、unspecified/loopback、multicast、discard、文档/基准/保留段等；包含2001::/23、2001:db8::/32、3fff::/20、2002::/16等保守拒绝。特殊用途即使声明全球可达也不放行。
- 拒绝所有 IPv4-mapped AAAA（在 Unmap 前识别，公网映射同样拒绝）、IPv4-compatible、well-known NAT64 `64:ff9b::/96`、local-use `64:ff9b:1::/48`、6to4、Teredo，以及已知ISATAP接口标识格式（后64位开头0000:5efe或0200:5efe）；不是只检查内嵌IPv4的低32位。Zone地址不可进入DNS批准集。[RFC5214](https://www.rfc-editor.org/info/rfc5214/)
- **任意网络自定义 NAT64 前缀无法从128位地址本身可靠识别**（RFC6052有多种前缀长度）；ISATAP/运营隧道/本地路由也可能把公网形状地址映到内部。部署门禁要求无DNS64/NAT64/6to4/Teredo/透明目的NAT/隧道，显式deny所有已知环境转换prefix、服务/宿主/集群/管理网络（含使用公网地址的内部网段）。环境不能给出保证时不可批准生产执行，不能假装前缀表解决了它。[RFC6052](https://www.rfc-editor.org/info/rfc6052/)

`netip.IsGlobalUnicast` 最多是辅助条件，不能作允许谓词；Go1.24.3 的本地源码明确其可返回 private/ULA 为 true。由单一策略函数输出批准/拒绝，dial前再次检查选中的 literal 和策略版本。运营 deny 优先，调用者不能覆写。

### D5. 连接绑定与 TOCTOU

每个 probe 使用独立 transport 和仅该次有效的 dial 闭包。闭包先核对 transport 要求的 origin/端口和 frozen snapshot 一致；只允许一次目标 TCP 拨号，额外调用安全失败。获准新连接在闭包内执行 D3/D4，然后立刻用 `tcp4` 或 `tcp6` 和 `JoinHostPort(approvedIP,80|443)` 调用受控 Dialer；绝不把原域名传给默认 dial。

闭包必须捕获本次probe的原始context/deadline，并与transport传入的dial context共同约束实际操作；不能假定transport为连接管理传来的context仍携带请求取消。执行器持有活动连接的关闭能力，取消主动close，不能仅等待transport内部goroutine自然返回。

TCP完成后，在 TLSClient/HTTP 写入前将 `RemoteAddr` 规范化为 IP/port 并与本次 approvedIP/port 精确相等比较；不符/不可解析立即 close 并 blocked_target，零 HTTP/TLS payload。该检查不能发现内核之后发生的透明DNAT，所以仍依赖 D8。批准票据不跨 probe、不跨连接、不由调用者提供；重试即使由库触发也在第二次拨号前拒绝，不静默换IP。

持有配置 v7 时DNS先给公网 P，之后改为私网 Q：本次只连接 P；下一次完整查询看到 Q 就拒绝。不存在“检查域名后默认Dial再次解析”的窗口。DNS连接与目标TCP连接分别计预算，不把DNS基础设施例外传播给target。

### D6. HTTP/TLS：有限协议表面

- 请求仅配置GET、无body；URL保持原规范 origin/path，Host为原始域名（不是CNAME或IP）；不转发入站headers、Cookie、Authorization、Proxy-Authorization、Referer等，不使用cookie jar/client cert。仅固定最小headers，例如 Accept-Encoding: identity。生产trust roots由受控镜像提供，测试CA仅测试构造；不读取用户提供的CA/代理环境设置。
- 只用HTTP/1.1，不启用HTTP2/h2c/HTTP3、Alt-Svc或协议升级。每次独立transport，`Proxy=nil`、`DisableKeepAlives=true`、`DisableCompression=true`、`MaxConnsPerHost=1`，无共享idle pool/TLS session cache。结束必close body、底层连接和idle资源，不drain；以新连接/一次dial约束阻断自动重试。
- HTTP使用安全DialContext。HTTPS使用显式安全DialTLSContext：先执行同一个解析/绑定/RemoteAddr检查，随后 `tls.Client` 以**原URL hostname**作为ServerName、正常roots/hostname验证、TLS>=1.2、ALPN仅http/1.1，`HandshakeContext` 自己设置 min(3秒,总剩余)；不使用 InsecureSkipVerify。自定义DialTLSContext下Transport的TLSClientConfig/TLSHandshakeTimeout不生效，不能只配置它们就声称握手有界。两个dial入口都必须安装，不留默认fallback。[Go1.24.3 transport源码](https://github.com/golang/go/blob/go1.24.3/src/net/http/transport.go)
- 不跟随任何重定向：选择直接单次RoundTrip（没有Client redirect循环）。301/302/307/308 的Location不解析、不查询、不连接。按原配置精确比较返回的最终状态：预期302且实际302可以success，它仅表示首跳状态符合预期。禁止将“不跟随”误写成所有3xx必定探测失败。
- 推荐**响应头状态探测**：完整最终200–599状态与expected_status相等才success；不读取响应体（应用body预算0），不解压、不下载、不保存。匹配200但body无限/慢/损坏时，返回头部成功并立刻关闭；不以没有读完body记body失败。101升级或最终状态超出200–599被protocol_error拒绝，非终端1xx至多4个，累计响应字节也受硬上限。
- `MaxResponseHeaderBytes=32KiB`；解密后的HTTP流（HTTP时为原流，HTTPS时为握手后的tls.Conn包装）最多从底层读取64KiB，包括header、1xx与有限read-ahead，单次Read也截断到剩余，不先超读再计数。body应用读取0并不声称内核/TLS/parser完全未预取body。TLS握手另有原始读取128KiB与3秒上限，避免证书链/握手数据耗尽；TLS不得获取AIA/OCSP网络资源。
- 若使用Got1xxResponse计数，需同时计累计header预算，因为Go在设置该回调后会重置单次header limit；计数超过4关闭，64KiB流限不能重置。chunked/trailer无需消费，任何关闭逻辑不得隐式drain大body。网络deadline同时覆盖写请求，阻断服务端不读请求的耗尽。

### D7. 时间、字节与多租户资源预算

所有数值是提案推荐的固定服务器上限，用户不能通过Monitor放大。T为同一配置快照中的timeout_seconds（1–30秒）；在 admission 成功时记录单调时钟起点，`D=min(parentDeadline,start+T)`。DB快照预处理另有2秒上限，不持槽或网络资源，不能让它无限等待。每阶段取 `min(阶段上限,D剩余)`，不重新开始T；总耗时包含DNS/建连/TLS/headers/close，取消立即关闭活动连接。

| 预算 | 推荐值/语义 |
| --- | --- |
| DNS | 整阶段2秒；1条DNS TCP；18个消息交换/8别名跳转；单消息16KiB、总64KiB、RR128、终端IP32 |
| TCP目标连接 | 3秒；最多1次，无fallback |
| TLS握手 | 3秒；原始读128KiB；无外部证书网络查找 |
| 最终响应头等待 | 从请求写完起最多5秒，同时受总D；请求写也受总D |
| 响应headers/stream/body | header32KiB；累计明文HTTP读取64KiB；应用body读取0；1xx最多4个 |
| 单进程并发 | 32个active probe；无待处理任务queue |
| Workspace/Project/Monitor并发 | 4 / 2 / 1；完整scope key，不按裸Monitor ID |
| admission速率 | 全局32次/秒burst32；Workspace4次/秒burst4；Project2次/秒burst2；Monitor连续两次admission至少其快照interval_seconds（60–3600秒） |
| gate状态 | 最多1024个scope/cooldown记录总和；未过期不能被LRU逐出以绕过限流，容量满拒绝；活动记录不能逐出 |
| 隔离probe进程示例硬限 | memory128MiB、CPU1核、pids128、nofile256；非无限goroutine/连接 |

Monitor冷却使用上次admission记录的next_allowed和last_start+新快照interval的较严格界，不因换version/缩短interval立即清空冷却；此次失败/blocked/canceled同样消耗已接受的速率。Monitor的last_start至少保留至last_start+3600秒（最大合法interval），active记录直到结束不能清理，避免旧短interval到期后删记录、再改长interval绕过最小间隔。全局/租户gate在一处原子检查并预留，拒绝无目标DNS/连接、无排队goroutine且不部分泄漏token；锁仅保护短内存操作，不跨网络。所有active计数完成/取消都释放，其余速率记录只有不改变后续准入结果时才能清理，均计入1024状态上限。T=1时所有阶段共享1秒，绝不能串成2+3+3+5秒。单probe至多DNS＋目标两个socket；进程硬限另覆盖运行时及少量管道FD。

这是**单进程**上限。A无生产实例扩容路径、不声称多实例总量或严格公平；未来Worker扩容需独立分布式配额。单租户最多4槽可为其它租户留下容量，但高租户数下不承诺调度公平。不同租户的取消、冷却、result和version不串用。

### D8. 出口隔离是必要的独立控制

A不交付生产探测进程。真实测试由外层harness建立一次性Linux network namespace/container，内部unprivileged probe test子进程无NET_ADMIN/RAW权限，不使用host网络、不挂Docker socket，不共享API/应用数据库网络与凭据。测试controller可使用独立PG18，probe只经受信pipe收到snapshot；PG凭据不传入probe。父harness设置防火墙，测试子进程不能修改。

未来生产出口准入至少要求：独立进程/容器/网络身份，最小filesystem/能力、无云metadata凭据或宿主socket；默认deny出站，独立IPv4/IPv6规则，专用resolver仅精确IP TCP53，目标只TCP80/443且先拒绝特殊用途、环境私网/集群/管理/宿主/metadata/内部公网CIDR和转换prefix。无UDP/隧道协议/不受控proxy；可允许不改变目的语义的普通源NAT，但禁止目标NAT/透明代理/IPv6转换绕过；检查实际路由、所有出口接口及容器重建后的规则仍有效。防火墙规则不能因允许DNS而放行该IP的80/443。

需要独立证据证明网络层在**绕过应用检查**时仍拒绝危险TCP和payload；必须包含IPv6，不能仅“容器运行成功”。公开调用路径必须待出口评审通过才可设计开放；A没有这样的入口，缺少可运行Linux环境时该安全验收是未通过/阻塞，不能由unit替代。隔离只能证明对应镜像/拓扑/规则，未来部署必须重新证明，不继承开发机结论。[OWASP SSRF分层防御](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html)

### D9. 不可变配置、版本与系统身份

新增内部只读snapshot reader，与用户Cookie Store分开。输入必须是受信系统调用能力＋规范完整scope，单条参数化SELECT JOIN workspaces→projects→http_monitors，以三元组约束取id、归属、规范URL、GET、interval、timeout、expected_status、version；不是先查裸ID再补归属，也不先查URL再查version。只读语句/取回/解码完在2秒内关闭Rows并释放PG连接，随后才admission/DNS。取到的快照需要纯离线验证（含URL规范形式/version>0/字段范围）；DB的URL CHECK仅子集，直接SQL损坏配置要安全拒绝invalid_configuration，不能执行。

系统读取不是“冒用Owner Cookie”：A仅受保护测试controller持独立只读DB角色（临时测试role，明确列/表SELECT，不能读users/sessions/members或写/DDL，无superuser/BYPASSRLS/角色继承）。角色fixture使用隔离PG管理员创建/清理，不是Migration7，不在应用启动配置生产role。无需按某个创建者当前Membership反复授权系统读取；用户API仍逐请求实时授权，原锁/CSRF/时效全不变。未来Worker应使用运营配置的独立服务身份、最小scope读取及经过审核的领取/结果写权限，不得沿用API全权DB账号或把session digest传给后台。

在SELECT快照v7后Owner修改URL/timeout/status提交v8：已读到v7的调用允许按v7完成，整体deadline和期望状态不能中途换成v8；后续受控读取取v8。PG MVCC查询开始前/后提交分别得到完整旧/新状态，不承诺查询返回瞬间仍是最新版本。没有长事务锁跨网络；配置PATCH/退出/撤销/禁用不应被探测网络拖住。用户退出Workspace/Session撤销只撤销新用户API访问，不取消系统已取得的v7测试执行；A没有后台自动任务可联动。取消来自父context/shutdown，不能假定用户HTTP请求结束就停止未来系统任务。

结果必须包含完整scope和configuration_version；从不写monitor status/last_result，不返回给用户。未来持久化至少复合FK三元组、不可变configuration_version和run_id；FK不能证明版本内容，需在领取时固定必要配置快照或受控版本历史，敏感URL另受存储/保留策略保护。结果展示为“当前配置”需显式匹配结果版本与当前version/activation_generation，旧结果可作历史但不能冒充新URL结果。A不提前创建历史/结果表。

### D10. 结果、失败与重试契约

纯内部内存结果（不是新HTTP DTO）：`workspace_id/project_id/monitor_id`、`configuration_version`、`started_at/finished_at`（UTC）、`duration`（单调时钟）、可空`http_status`、`success`布尔、可空固定`failure_code`。不带URL、DNS答案、CNAME、IP、Location、response headers/body、证书或原始error。超时可能已有部分状态，只有已完整得到的最终头才填写status。

| code | 条件 |
| --- | --- |
| invalid_configuration | snapshot预检违反现存配置契约，属于未启动错误，零网络且不构造结果 |
| dns_failed | NXDOMAIN/NODATA全空/SERVFAIL/协议/链/预算错误，无部分答案fallback |
| blocked_target | 任一危险IP/别名策略拒绝、选中IP失去批准或RemoteAddr不符；不发HTTP负载 |
| connect_failed | 明确拒绝/不可达等TCP错误（没有命中超时） |
| tls_failed | 信任链/hostname/协议/握手失败（没有命中超时/资源限制） |
| timeout | 总/阶段deadline届满，可内部附固定phase枚举，不输出目的细节 |
| canceled | 调用者取消，关闭资源；不代表远端未处理过GET |
| response_limit | HTTP头/1xx/流或TLS握手字节预算超限 |
| protocol_error | 无效HTTP/101升级/响应协议故障 |
| unexpected_status | 完整最终状态不等于快照expected_status |

上表为统一内部失败分类；invalid_configuration在预检返回，其余执行期错误可组成ProbeResult。分类按确定观察决定：结构化blocked/limit先保留；其余context明确取消→canceled、明确deadline/net timeout→timeout，其余按失败阶段分类；已完成最终结果不被随后取消改写。取消与timeout竞态用实际记录的terminal cause稳定分类，不能匹配原始error字符串。预检错误、admission拒绝/不存在或混scope的SELECT失败是**未启动**返回（如invalid_configuration/resource_limited/snapshot_unavailable），不伪造目标失败结果，不发DNS/HTTP，不持久化它们。允许result IDs只流入受信测试断言；普通日志仅固定reason/phase和无租户label的有界聚合，禁止tenant/monitor/version成为高基数labels，禁止fmt打印net/url.OpError或recover值。

一次调用最多一个HTTP请求，零自动重试；结果丢失/取消/进程崩溃时远端是否已收到未知，不做补偿调用。进程内gate避免同scope重叠但不提供跨进程幂等，显式重调用在冷却后可能重复GET。A无DB业务写Commit，所以不存在新“网络＋数据库”原子提交；已有配置写的未知Commit协议不变：不假定回滚/自动重放，由新snapshot观察已提交状态，不能在配置POST/PATCH后触发probe。

### D11. 激活及存量处置

A中“保存配置”始终不表示启动。历史、新建、PATCH后的Monitor全部不自动执行；没有enabled字段、后台扫描、startup hook、服务路由、命令或环境开关可启用探测。即使schema6存有一万条Monitor，API启动只运行既有Session清理。源码依赖检查确保 `cmd/api` 不依赖 `internal/monitoring`，路由/API白名单完全不扩大。

用户在本轮**没有启动监控的产品入口**。受控测试只能使用自己的隔离PG数据与fixture域名，通过内部系统能力调用；不得枚举/执行生产Monitor或从环境接收任意生产目标。下一Change必须让当前Owner显式对特定配置版本确认激活，定义暂停/停止；URL变化需重新确认，不能将迁移或旧配置当作默认授权。该未来契约不通过本轮增加保留字段偷跑。

### D12. 测试与CI：四类证据单列

本次Propose只做文档/规格校验；以下全部是**后续Apply验收计划，尚未执行**。每类保存实现SHA、Go/依赖和镜像来源、命令、stdout/stderr、exit、预期/实际断言。未运行明确写未运行，不能拿第八阶段归档通过或本次文档验证替代。

| 类别 | 要证明什么 | 必需证据/门禁 |
| --- | --- | --- |
| 静态/单元 | 地址prefix/边界、DNS报文解析/计数、无隐式fallback、gate/版本/result/脱敏 | gofmt/vet、unit/race、DNS parser/URL/IP有界fuzz、泄漏sentinel；不能称为真实网络证明 |
| 受控真实网络 | 真实socket/DNS wire/TLS/HTTP按D3–D7运行 | 独立Linux DNS/HTTP/TLS夹具、抓包/进程归属、fixture接收计数、正向校准和负向零尝试；不能用mock替代 |
| 真实PG18 | 同语句完整scope/version、权限、旧新版本并发、原锁协议/回归 | 守护隔离库、独立连接/屏障/pg_locks观测、全integration/race；网络用暂停fixture而非持锁mock |
| 独立出口证明 | 应用检查失效时网络仍阻断 | 外层namespace规则/路由/无上游证明、IPv4/6计数/抓包、故意绕过分类器的blocked connect正向控制、未获目的payload证据；不宣称证明未来生产拓扑 |

真实网络runner必须先安装隔离再启动fixture/probe：无宿主网络、无Internet default route或external DNS；fixture专用veth/namespace上模拟“普通公网形状地址”和危险私网/metadata/转换地址，均没有向真实地址转发的路径。URL使用满足现有离线规则的fixture域名（例如probe-a.example.com），解析完全由固定测试resolver回答；不得为方便允许.test/IP literal穿过生产策略，不真正访问云metadata/生产内网。普通公网形状fixture地址只在隔离拓扑使用，不向实际公网分配地址发包。

网络攻击矩阵及证据：

1. DNS第一次返回安全P，验证后/连接前切换Q；断言只有P收到连接，系统resolver/默认dial没有第二次解析；下一次admission使用新快照/新查询拒绝Q。另测同host两次合法probe各自完整查询、不跨probe缓存。
2. 混合A/AAAA、公私网任意排序、危险答案在最后、CNAME终端危险/保留名、环/超深、两个家族不一致、NODATA单家族/两家族、NXDOMAIN、失败/慢/截断/超长/多地址；断言危险情况目标TCP尝试为0且HTTP负载0（允许专用DNS）。
3. IPv4/IPv6 prefix首末/相邻、mapped公网/私网、NAT64多种RFC6052前缀和运营deny、6to4/Teredo等；区分unit分类和真实隔离路由阻断证据。网络映射未知前缀绕过应用分类的演示必须仍被独立出口策略阻断，不据此声称应用可识别任意prefix。
4. HTTPS fixture验证SNI/Host原域名与IP绑定同时成立；CNAME不替换SNI；正确CA成功、错误hostname/过期/不可信链失败、无InsecureSkipVerify。测试CA只在该测试进程注入。
5. 301/302/307/308到危险域名/IP、多跳和跨协议；首跳状态按配置分类，目标DNS/TCP/HTTP接收数0。设置HTTP_PROXY/HTTPS_PROXY/ALL_PROXY/NO_PROXY及小写变量，malicious proxy fixture计数0，HTTPS无CONNECT。
6. 连接后RemoteAddr不符（可通过受控连接wrapper另做unit）在TLS/HTTP前关闭；真实重绑定链用wire/抓包证明，不能用wrapper测试冒充真实网络。
7. TCP不接受、TLS挂起/证书链过大、慢/无限headers、1xx洪泛、101、chunked/大body、gzip bomb、读写deadline、parent取消、T=1/30；检查时间、计数、FD/goroutine回落，body0且不drain、不解压。目标收到GET后取消不得宣称GET被撤销。
8. 同scope重复、不同Workspace/Project并发、32/4/2/1边界、速率/1024状态满、拒绝零网络、取消释放；高负载下另一个tenant可在其预算内完成，API进程独立存活；不夸称完全公平。
9. PG屏障：v7 snapshot后挂起HTTP，Owner PATCH为v8，返回的旧结果仍是v7/旧expected_status/旧timeout；新snapshot完整v8；URL/status/timeout与version不可撕裂。探测挂起时配置写/退出/撤销能正常完成，pg_locks无probe长事务。双租户全scope替换拒绝，内部role写入/身份表读取被PG权限拒绝。
10. 独立防火墙正向控制：临时的测试专用恶意probe绕过应用分类直拨危险fixture，必须被网络规则挡住并记录违规**尝试**，接收端无HTTP负载；正常allowed fixture计数>0确保测试实际执行。child0不等于audit0；不得把“连接失败”当零尝试。

CI保留现有 verify/integration 及 `run-monitor-audit*.sh` 配置API审计原策略、CGO=0来源门禁/违规NSCD回归。新增隔离执行器job/runner及独立输出目录/分类器策略，不扩大旧精确白名单；probe进程仅准固定resolver和该case获准fixture，PG连接只归属controller。工具下载/build先于审计区间，固定Linux镜像/Goose/PG digest；报文和trace使用无秘密fixture，仍不记录实际用户URL。

显式安全job缺Linux/namespace/防火墙工具/IPv6/ptrace/构建来源或PG18依赖时fail，不静默skip。Go race不能代替PG锁/网络证据；测试内存TLS不代表出口通过。托管CI以最终实现SHA重新验收；本轮不改CI/运行新安全测试。

## Risks / Trade-offs

- [自写窄DNS客户端仍复杂] → 复用现有dnsmessage、严格边界与fuzz/真实DNS夹具；不写递归解析器，不用高层API拼链声称完整。
- [特殊用途表/环境deny会过时] → 固定来源与哈希、发布审核、边界回归；缺环境转换/路由保证则生产门禁不通过。
- [恶意但看似公网的DNS及内部公网路由] → pin literal防重解析，独立出口/路由隔离补足；不能仅用IsGlobalUnicast或RemoteAddr宣称闭环。
- [全RR拒绝/IPv4优先/不fallback降低成功率] → 明确选择安全和确定性，有限可用性留待后续独立优化。
- [header-only健康含义较窄] → 产品确认后按此验收；不把部分body/压缩/内容检查偷偷加入，若要body完成检查需改本提案预算和分类。
- [库误接入生产/测试构造逃逸] → API依赖图和无触发契约守护；fixture注入只测试；将未来运行入口视为新安全评审。
- [进程内配额不覆盖多实例] → A不部署Worker；B需独立分布式协议，不能把A通过当B通过。
- [没有结果表/状态API] → A以内部结果/夹具断言可验证，不提供用户健康面板；后续持久化必须独立Migration7和保留策略。

## Migration Plan

本次只新增规划文件。未来A的Apply仍**无Migration7**，Migration1–6逐字节不变、schema固定6、已有失败next-version fixture仍用临时7，不改历史迁移边界。运行受保护PG18回归时才允许现有测试迁移；本次不执行任何迁移。

未来A交付的包不注册到API，不需要生产DB角色/运行配置；合入代码也不激活任何Monitor。回退仅移除内部库/测试入口，无生产数据回滚、无down。增加生产进程、调度、持久化、用户触发或激活时重新提Change，若首次改schema使用Migration7（届时核实是否已被其它已批准Change占用），不得先写空Migration占号。

## Product decisions for review

以下为本提案已经具体建模的**待确认推荐**，不是可在Apply自行决定的开放实现问题。用户请求允许先提出这些选择；本轮不要求即时回复，也不视为已批准。独立技术评审后须另行授权Apply；若选择不同，先更新proposal/design/specs/tasks再实施。

1. 选A：内部单次内核＋隔离真实测试，不提供任何生产自动/手动调用入口；B另开Change。
2. 选header-only：以首跳最终状态精确匹配为成功，body0；expected_status=302时302可成功且不跟随。
3. 选保守地址/答案策略：任一危险RR整体拒绝、转换地址全拒绝、单IP无fallback；接受由此产生的可用性损失。
4. 选本轮无Migration/结果表/启停字段：结果仅内存，历史与新Monitor全不运行；未来Owner显式对版本激活，URL变化须重确认。
5. 接受32/4/2/1并发、表中时间/字节/速率预算及出口隔离硬门禁；上限调整必须保持不可放大的安全边界并同步验收。
6. 选快照语义：v7已开始任务允许完成且只属v7；用户退出/撤销不追溯取消系统已取得的快照，父context取消仍生效；不将旧结果发布为当前健康。

## Research references

读取日期2026-10-19；来源支持设计依据，不证明本提案已实现。实际实现绑定Go1.24.3本地源码，不能拿最新版默认行为替代。

- [Go1.24.3 net/http transport](https://github.com/golang/go/blob/go1.24.3/src/net/http/transport.go)：核对自定义TLS dial、pool/retry及header边界；本提案显式收窄默认行为。
- [Go1.24.3 netip](https://github.com/golang/go/blob/go1.24.3/src/net/netip/netip.go)：本地已读IsGlobalUnicast实现；private/ULA不自动拒绝。
- [x/net v0.49.0 dnsmessage](https://pkg.go.dev/golang.org/x/net@v0.49.0/dns/dnsmessage)：已有依赖提供wire parser/builder，而非完整安全resolver策略。
- [RFC1035](https://www.rfc-editor.org/info/rfc1035/)：DNS RR/CNAME及TCP framing；D3的容量/链一致性是本项目更严格的策略。
- IANA IPv4/IPv6、RFC6052、OWASP链接见对应决策。所有数值预算、无生产入口和分层门禁是本项目设计选择，不是来源宣称的安全认证。
