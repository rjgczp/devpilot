# add-monitoring-engine Verification

## 授权与实施范围

2026-10-19（Asia/Shanghai）用户通过正式Apply授权确认独立设计评审通过、方案A及全部产品/预算选择，允许实现、真实测试、提交并创建指向main的PR；不授权自动合并、Archive、部署、生产入口或激活。本文件是实现者的验证记录，不冒称第二轮独立代码安全评审。

初始HEAD/main/origin/main/连接器远端main均为`252c7d3c000c68de3e08d2fc8de4cece3a2d00e8`，当前实现分支`codex/add-monitoring-engine`。唯一初始未跟踪内容是本聊天生成的已批准Change目录，已保留。八阶段已归档；main的[Go CI run39210971110](https://github.com/rjgczp/devpilot/actions/runs/39210971110)为success。本次没有reset/覆盖用户工作。批准proposal/design/两份delta未改；tasks只按实际验收勾选，初始哈希见`evidence/baseline.json`。

新增`internal/monitoring`私有执行内核与系统只读reader。API、生产Worker/入口、OpenAPI、go.mod/go.sum和Migration1–6不变。配置包只新增纯离线复用函数，历史migration哈希守护加入6。没有Migration7、结果表、激活状态、公开/CLI触发、队列、通知或调度。独立`tests/execution`不修改`tests/network`的旧策略/白名单。

## 证据边界

本地环境：macOS arm64、Go1.24.3、pgx/v5 v5.8.0、x/net v0.49.0、固定Goosev3.26.0；实际自建可销毁PG18.6容器，数据库名`devpilot_registration_test`、只绑定随机回环端口，不连接应用库。PG镜像固定digest与既有CI一致。Linux测试运行于独立Docker network-none容器，受控fixture地址没有上游路由；不是公网目标探测，也不是生产出口验收。

每个`evidence/*.json`记录真实命令、UTC开始时间、耗时和exit，同名txt为原始输出。源树/构建/二进制/镜像来源分开记录；最终托管CI必须绑定具体实现SHA，不能把本地结果或旧main CI当作新head通过。当前本地结果如下；托管结果在创建PR后按实际SHA追加。

| 类别 | 实际验证 | 结果及证据 |
| --- | --- | --- |
| 静态/单元 | gofmt、go vet、全量Go unit、Go race | PASS；`unit.*`、`race.*`、`vet-final.*`；声明测试清单分别118项unit、195项带integration的顶层Test，清单不是额外一次运行结果 |
| 专项核心race | 进程共享gate、scope/冷却/速率/1024容量、DNS/IP/读取上限、安全分类 | PASS；`core-final-race.*`，以相同核心专项弥补最后TLS上限收紧后的相关复验 |
| 有界fuzz | DNS wire5秒/2worker；IP策略5秒/2worker | PASS；`dns-fuzz.*`实际220335次、`ip-fuzz.*`实际188481次，不能外推为穷尽证明 |
| Python | 旧审计/构建来源回归，新独立分类器 | PASS；`python-original.*`、`python-execution.*` |
| 真实Linux网络 | 独立TCP DNS、原生IPv4/IPv6、CNAME、TLS/SNI、重定向/代理、超时/取消、限制/资源 | PASS，76场景；最终核心证据`network-final-code/`与`network-final-code-run.*` |
| 真实PG18 | 全量integration、integration-race | PASS；`integration.*`、`integration-race.*`，串行执行所有schema变化，非mock/非Go race替代PG锁证明 |
| 系统PG专项 | 实际只读LOGIN角色、全scope、单语句快照、错误配置、权限/连接/锁、v7/v8真实HTTP联动 | PASS；`snapshot-pg-final.*`、`snapshot-pg-race.*`及`pg-final/`、`pg-race/`；4个顶层测试另含4个权限生命周期子场景 |
| 原配置API审计 | 原精确白名单/CGO0双工具来源检查/校准控制 | PASS；`config-network-run.*`与`config-network/`；control child0/audit1、1违规，real child0/audit0、4批准/0违规 |
| 独立出口/最小权限 | network-none、IPv4/6默认deny、精确tuple、原生形状DNAT/内部公网、UID/GID/caps/硬资源限额 | PASS，仅限该拓扑；见下文，不能称生产环境已安全准入 |

新代码没有skip路径；显式缺PG18/schema/工具/IPv6/namespace/ptrace/来源或新鲜PG联动目录会失败。旧归档证据未计入本轮通过。

## 执行安全与真实网络观测

`network-final-code/summary.json`的76场景包含72个正常审计exit0，以及4个**期望审计exit1**的故意绕过应用控制。所有child应为exit0；不把恶意控制的audit1说成零违规成功。具体命令、单独`.trace/.txt/.json`、接收端`events.jsonl`、`listeners.txt`和`firewall-v4/v6.txt`可复验。

- rebinding：第一次A为P，AAAA完成后fixture改变回答；本次实际只到P，第二次DNS看到危险Q拒绝。实际DNS连接2、目标连接1、GET1。repeat场景分别新查新连，DNS2/目标2。
- 混合公私网/另一家族危险/末记录/额外section、保留alias、mapped/NAT64/6to4/Teredo/ISATAP、metadata/共享/ULA/文档，以及链冲突/超深/空/失败/截断/ID/question/尾随/容量，均有明确安全分类和零目标TCP断言。
- SNI/Host保留原URL域名；CNAME不改变SNI。错名/过期/不可信证书拒绝；大证书response_limit。测试CA/序列化/fixture注入均在`*_test.go`，不存在生产网络注入构造API。
- 301/302/307/308作为首跳状态，expected302可成功；没有Location DNS/连接。大小写代理环境全部投毒，proxy接收0、无CONNECT/凭据。只GET/HTTP1、不复用、不fallback/重试。
- T1证明整体截断，T30分别真实证明DNS2、TCP3、TLS3、headers5，连续慢CNAME共享2秒。慢/大header、1xx/LF1xx洪泛/累计明文、101、无效HTTP、巨大/无限/chunked/gzip body、取消、FD/goroutine/32槽回收均有实际执行。
- 握手原始128KiB上限在握手后继续保留，防止TLS控制消息/填充绕过HTTP明文64KiB；这是更保守地保留原上限，可能使异常大的握手＋响应提前response_limit。应用body读取0不声称TLS/parser没有有限read-ahead。
- 冷却与32/4/2/1、速率、1024状态在unit/race验证；真实并发场景32个获准，额外无排队拒绝，取消释放。所有内核实例共享同一进程gate；不声称跨进程/跨实例全局配额或无限租户下公平。

独立最后防线：故意直连metadata IPv4、ULA IPv6、native-looking IPv6→隔离ULA的DNAT、内部公网形状地址，系统调用审计均记录违规attempt，防火墙REJECT/真实DNAT计数非零，危险监听fixture没有HTTP负载。正常HTTP/SNI/IPv6正向控制有实际接收，避免用“服务没跑”伪证零负载。`topology.json`无默认路由/外部NIC；Docker内核默认隧道模板必须DOWN且无地址/路由。子进程UID65534、CapEff/CapBnd0、NoNewPrivs1、nofile256；实际cgroup memory134217728、cpu100000/100000、pids128记录在resources.json。

失败均保留而非掩盖：

1. `network/`预检发现Docker Desktop暴露DOWN的内核隧道模板。修正为验证其确实DOWN/无地址/无路由，并拒绝任何外部NIC；没有放开其网络访问。
2. `network2/ok.trace`逐exec证明setpriv在Go exec前发出4次NSCD Unix socket尝试，旧策略正确拒绝。使用`python3 -I -S`数值降权，整个包装过程仍受审计，不增加Unix例外。`network3`起真实通过。
3. `network-final/addresses-boundary`测试数据误含运营deny地址，执行器正确blocked_target。仅将夹具换为32个都安全的地址；策略不变，`network-accepted`及最终套件通过。

为减少PR重复原始输出，较早的六个网络运行目录原样打包至`evidence/prior-network-runs.tar.gz`，每个文件SHA256在`prior-network-manifest.json`，解包到evidence目录可恢复上述原路径。最终`network-final-code/`保留全部76个case的JSON/测试输出和11份关键原始trace，其余65份trace无损存于该目录`additional-traces.tar.gz`（`trace-manifest.json`列出SHA256）。归档前逐项重读压缩包校验字节哈希，不删除或改写原始证据内容；解包到对应目录即可复用原审计命令。

## PG18并发、身份与无长事务

单参数化SELECT精确JOIN三元组，同一语句读取全部配置/version，Rows关闭后释放pool。实际独立LOGIN角色只读必要列，不继承角色，无superuser/BYPASSRLS/写/DDL/身份表权限。测试临时移除隔离库PUBLIC TEMP默认授予后还原，防止只读角色通过PUBLIC获得TEMP DDL；临时role清理不遗留，也不成为生产账号/Migration。

独立writer事务在提交前不可见、提交后取得完整新配置，受支持Owner条件更新再递增版本；不是sleep-only并发。异常URL可以通过DB子集CHECK，但Reader离线拒绝，返回未启动错误，不能生成假探测结果。pool等待受2秒预算；独立NOWAIT表锁和pg_locks证明读语句已结束。

live控制器以stdin pipe把自己的fixture快照交给独立无DB/Cookie的子进程，以独立HTTP接收端事件为屏障。v7 HTTP挂起时，pool acquired=0、Monitor锁0；Owner按If-Match7提交新URL/timeout1/status201的v8，随后Session撤销完成，新用户读取失权。目标延迟超过新1秒后释放，旧v7仍以原3秒/200成功；独立v8执行得到unexpected_status且version8。旧结果从未写入当前健康。退出/撤销/禁用/清理前后系统快照与Cookie权限分开测试，完整旧协议/未知Commit竞态由本轮全量PG normal/race回归覆盖。

## 审查索引与剩余风险

完整Requirement→Test→Evidence索引见`evidence-index.md`。原migration/schema/API/依赖及批准规划的哈希检查见baseline/source记录。OpenSpec本Change strict通过；全库strict的四份历史规格七项描述长度WARNING另列，不改写历史要求消警。

需要第二轮独立审查的主要风险：DNS wire链一致性/完整性和边界实现；固定IANA表的后续维护；TLS/HTTP双层读取限制与取消竞态；进程级admission状态保留/容量及公平限制；fixture与被审计进程/来源绑定。任意自定义转换、透明目的NAT和内部公网路由无法仅靠应用IP分类识别，测试namespace通过不等于生产出口通过。header-only成功不代表body完整/内容健康；单IP无fallback和保守特殊用途deny牺牲可用性。用户身份撤销不追溯取消系统已取得的版本；任何生产激活/Worker/持久化/分布式配额须另开Change。

## 托管交付状态

本文件初稿时尚未创建PR，不能声称托管CI通过。创建后在此按实际实现SHA和GitHub run/PR状态追加，保持PR Open等待第二轮独立安全审查，不合并/归档/部署/激活。

### 2026-10-19 实施提交与托管验收

实现提交为 **`d2f42ec98da816c668369ef2a89acafd32d6ad8e`**。[PR #9](https://github.com/rjgczp/devpilot/pull/9)指向main，保持Open、未合并。普通Git凭据首次推送因缺workflow scope被GitHub拒绝；已连接GitHub App成功发布完整提交，随后fetch验证远端与本地SHA相同。没有删除工作流、修改凭据权限或绕过任何CI/审计要求。

- [Push run39218841168](https://github.com/rjgczp/devpilot/actions/runs/39218841168)及[PR run39218941405](https://github.com/rjgczp/devpilot/actions/runs/39218941405)均completed/success且head_sha精确为上述实施SHA；三个job（verify、integration、execution-security）及其步骤全部成功。完整逐步骤状态见`hosted-implementation-push.json`和`hosted-implementation-pr.json`。
- 实际下载Push的execution-security与原monitor-network两个Artifact，逐项核对：托管Linux/Go1.24.3/CGO0构建来源，76网络场景、72个正常audit0及4个预期audit1控制，PG normal/race的v7成功与v8 unexpected_status版本结果，原配置API real4批准/0违规及control child0/audit1。
- 托管原始文件无损归档为`evidence/hosted-implementation-artifacts.tar.gz`，`hosted-artifact-manifest.json`列出所有文件SHA256；已从压缩包重读逐项验证。这里的托管证据是实际下载Artifact，不是从本地输出推断。
- OpenSpec任务34/34已根据实际验证勾选；Change strict通过。批准proposal/design/delta、Migration1–6、API、生产入口与依赖仍保持原基线；仅tasks状态和实施验证文档更新。

本段及Artifact作为后续文档交付提交记录上述**明确实施SHA**的结果；文档提交本身的最终PR head以`git rev-parse HEAD`/PR为准，交付前还须等待该最新head的Push与PR CI全部成功，不能把以上run冒充为后续head的运行。运行内核代码未因这次文档追加而改变。

### 2026-10-19 合并与归档授权

用户明确授权“可合并归档”，本节取代以上保持Open/禁止合并归档的交付等待状态，不改变禁止部署、激活和生产目标探测的边界。GitHub确认PR #9由rjgczp于2026-10-19T08:13:11Z合并，最终PR head为`e412e5c1d803499d41777e73340640281ab2d27e`，合并提交为`f14a63118f12a09f5fd3b8c17f4c83cd09fe02fa`。不据此虚构独立技术审查结果。

- 最终PR head的Push run39219803659和PR run39219807044均completed/success；两次运行各自的verify、integration、execution-security全部成功，原交付回执保存于`evidence/hosted-final-delivery.json`。
- 合并提交的[main run39221215464](https://github.com/rjgczp/devpilot/actions/runs/39221215464)也已completed/success，三个job均成功；合并、用户归档授权及逐job确认见`evidence/merge-archive-approval.json`。此证据属于合并提交，不冒充随后归档提交的CI。
- OpenSpec四份规划工件和34/34任务均完成。主规格新增http-monitor-execution的15条要求/45个场景，原文复制Purpose；http-monitor-persistence仅更新对应SSRF执行边界要求并保留原场景，其他要求、标题、Purpose不变。逐delta比对确认全部同步后，归档至`openspec/changes/archive/2026-10-19-add-monitoring-engine/`。
- 归档前Change strict通过；同步后全部15份主规格普通校验通过。本轮两份主规格无警告；全库strict仍有四份历史规格七项既存描述长度WARNING，未借归档改写历史要求。具体命令、退出码和结果见`evidence/archive-validation.json`。
- 归档只移动Change及追加交付记录、同步主规格；原始证据与批准规划保持字节不变，`verification.md`仅追加本节。原318个Git跟踪文件的归档前SHA256与移动后保留校验见`evidence/archive-preservation.json`，不将Finder自动更新的三个忽略项`.DS_Store`计为交付工件。生产代码、Migration1–6、依赖、API及启动路径在合并后未改变；无Migration7、无数据库迁移、无部署或监控激活。
