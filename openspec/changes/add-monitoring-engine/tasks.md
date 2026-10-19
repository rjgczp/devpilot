# Tasks

以下全部为未来Apply任务，当前未执行。实施前置条件：用户确认本提案产品选择、独立技术评审通过，并另行明确授权Apply；Propose完成或CLI提示ready不等于这些批准。若更改A/header-only/无入口等选择，先更新规划，不自行扩大实现。B比较不是实施清单。

## 1. 内部边界与结果基础

- [x] 1.1 建立独立 `internal/monitoring/` 的不可变snapshot/内部result/固定错误契约及受信构造边界，fixture注入只放测试路径；用类型/校验与result字段单元测试验证scope、正version、UTC时间、单调duration、可空状态和不含URL/网络详情，补内部契约说明。
- [x] 1.2 保持执行器与API依赖分离，增加依赖图/路由白名单回归断言；以 `go list -deps ./cmd/api` 不含执行器、现有配置包无network守护及启动仅Session清理的测试证明没有生产入口/环境开关/隐藏trigger，不更改API schema。
- [x] 1.3 固定Migration1–6全部SHA256守护（6为proposal记录值），确认schema仍6及失败next-version fixture仍临时7；以哈希/文件清单和现有migration单测验证无新增Migration/业务表，记录无schema变更决定。

## 2. 全scope只读系统快照与PG18证明

- [x] 2.1 实现独立内部系统snapshot reader：单条参数化JOIN绑定wid/pid/mid、同一快照完整执行字段及version、2秒DB预算、关闭Rows释放连接后才交给执行器；用受保护真实PG18测试正确scope、三层混ID/裸mid拒绝及无额外存在查询，记录SQL/权限边界。
- [x] 2.2 最小复用现有纯离线配置校验（必要时只增加窄纯函数接口，不迁移Store/重构认证），拒绝DB CHECK子集遗漏的损坏配置；以纯函数单测和PG18直接SQL损坏fixture验证invalid_configuration且目标网络0，原URL规范测试保持。
- [x] 2.3 在隔离PG18临时创建不继承/无BYPASSRLS的最小只读测试role，只grant所需配置列/归属和schema usage，不向probe传凭据；用真实授权/拒绝查询证明可读取snapshot但不能写、DDL或读身份/Session/Membership，测试清理无残留role，说明这不是生产role或Migration7。
- [x] 2.4 用独立连接/屏障证明SELECT快照与并发PATCH只读完整旧/新版，冻结后参数不变；PG18记录与单测断言URL/timeout/status/version不撕裂，网络联动的锁证明在7.4完成。

## 3. 地址策略与DNS协议

- [x] 3.1 固定并审查IANA特殊用途及分配表来源/日期/哈希，落地单一prefix允许/deny策略、IPv4/IPv6环境deny和已知嵌入格式拒绝；用首末/相邻、IsGlobalUnicast反例、metadata、mapped/NAT64/6to4/Teredo/ISATAP向量和有界fuzz验证，补策略更新/任意转换不可识别的说明，不运行时下载表。
- [x] 3.2 复用已固定x/net/dnsmessage实现仅固定IP TCP53的窄DNS adapter，验证framing/ID/question/RCODE/完整报文、绝对FQDN、无search/hosts/failover；用报文单测/fuzz覆盖截断/伪造长度/不匹配/无效压缩/解析错误，确认无新依赖升级并记录resolver通道规则。
- [x] 3.3 完成A/AAAA/CNAME链合并、两家族一致性、全section地址安全检查及只使用链终端答案，处理单家族NODATA/全空/NXDOMAIN/混合危险；用单元矩阵验证危险在最后/另一家族也整体拒绝、additional不作target、不支持DNAME失败，不能将LookupCNAME独立调用拼成证明。
- [x] 3.4 实施DNS总2秒、单16KiB/总64KiB、RR128、地址32、8跳/18交换与单DNS连接上限；用超限前后边界、慢连续alias和分配计数测试验证不截安全子集/不逐query重置预算/不重连，明确DNS失败与timeout分类。
- [x] 3.5 通过第6组隔离harness运行真实TCP DNS夹具矩阵，保存A/AAAA/CNAME query和进程socket证据；验证失败/空/混合/链/超限均零目标TCP，正常原生IPv4-only/IPv6-only和TTL0能到达允许fixture，证据与3.1–3.4单测分列。

## 4. IP绑定、TLS和有限HTTP

- [x] 4.1 实现每次独立transport的单目标dial闭包：核origin→重解析/策略→确定单IP literal tcp4/6→RemoteAddr核对，第二次dial先拒绝；用连接wrapper单测验证错误远端/替换origin零HTTP或TLS负载、无default resolver/happy-eyeballs/fallback，并记录wrapper证据限制。
- [x] 4.2 实现安全DialTLSContext显式握手与原域名ServerName/正常roots/hostname验证，TLS>=1.2、ALPN仅HTTP1、禁session cache/client cert/外部证书请求；用测试CA和真实TLS fixture证明正确SNI+IP绑定、CNAME不替换名、过期/错名/不可信失败，验证自定义dial下实际3秒及128KiB握手限制。
- [x] 4.3 实现GET无body、固定headers/原Host、Proxy=nil、单次RoundTrip、无keep-alive/HTTP2/升级/自动retry；用真实fixture验证301/302/307/308不解析或访问Location、expected302可成功、全部代理环境和凭据污染不影响连接/请求，代理CONNECT与接收计数均0，补header-only语义说明。
- [x] 4.4 实施32KiB header/64KiB累计明文stream/最多4个1xx、body0、不drain/解压及所有关闭路径；用真实慢/大header、1xx flood、101、无限/超大/chunked/gzip body证明上限和头部成功语义，记录TLS/parser read-ahead与应用body0的区别。
- [x] 4.5 真实DNS在校验P后切换Q，验证本次只连接P、没有第二次系统解析，后续独立调用拒绝Q；以resolver query、syscall目标和fixture负载证据证明TOCTOU防护及每次新连接重查，不用mock替代。

## 5. deadline、admission与安全分类

- [x] 5.1 实施admission起点的总T和各阶段min(上限,剩余)，连接deadline覆盖写/读、parent取消关闭、完成结果稳定；用T=1/30、DNS/TCP/TLS/header慢服务及取消竞态单测/race与真实网络测试证明无阶段叠加/孤儿连接，记录固定失败phase。
- [x] 5.2 实现单进程32/4/2/1并发和全局/Workspace/Project token速率、Monitor interval冷却的原子无排队gate；用单元/race验证完整scope分桶、拒绝无partial token泄漏/网络、换version/缩短interval不绕过、失败/取消也消耗速率，文档标明非分布式配额。
- [x] 5.3 将scope/cooldown记录总数限1024，Monitor last_start保留至少3600秒，不能逐出未过期/active记录；用容量满/短interval过期后改长interval/多租户慢任务/取消/并发错误验证无限流绕过或无界goroutine/FD/连接、所有终止释放active且另一租户在余量内能完成，记录不承诺严格公平。
- [x] 5.4 实施固定错误分类/terminal cause、未启动拒绝与执行结果区分、零自动retry/补偿；用DNS失败/blocked/TCP拒绝/TLS/timeout/cancel/协议/状态/limit矩阵及URL/path/DNS/原error/证书/header/body/panic sentinel测试验证result与普通日志不泄密、不引入高基数label，补丢失结果≠未发送的说明。

## 6. 独立Linux真实网络与出口harness

- [x] 6.1 新建独立runner/测试build tag与证据目录，不改第八阶段配置API审计白名单；固定Linux工具/镜像与构建元数据，先建无上游namespace/veth/DNS/HTTP/TLS fixture再启动无特权probe，测试controller通过pipe传snapshot不传DB/Cookie；用路由/namespace/进程能力记录验证无host网络/真实metadata路径。
- [x] 6.2 安装IPv4/6默认deny、精确resolver TCP53和case批准HTTP80/TLS443规则、内部/转换deny；配置memory128MiB/CPU1/pids128/nofile256等硬限；保存规则/路由/资源配置及子进程不能改规则的证明，确认DNS例外不放开target80/443。
- [x] 6.3 建立按进程/tuple审计的正向控制：allowed fixture真实接收>0；恶意测试probe绕过应用直拨危险fixture必须记录违规attempt并被防火墙阻断且无HTTP负载；保存child/audit各自exit、trace/packet/接收计数，验证未知trace/缺IPv6/缺权限fail closed，不能将连接失败视为零attempt。
- [x] 6.4 在该runner执行3.5及4.2–4.5/5.1–5.3的真实网络场景并逐项留证，严格区分应用拒绝时零目标TCP与绕过应用时有attempt但网络阻断；无公网实际探测、不为fixture放宽生产URL/IP策略，补可复验命令与证据解释。
- [x] 6.5 对自定义NAT64前缀/内部公网路由/目的NAT绕过建隔离反例，用外层出口规则仍阻断的计数证明最后防线；记录应用无法识别任意转换的限制与未来生产路由准入清单，不宣称测试拓扑证明未来生产出口。

## 7. 真实PG18与执行器联动

- [x] 7.1 将第2组PG18 reader输出通过受信controller交给隔离probe，保留fixture明确信息来源/构建SHA；以真实GET/TLS结果三元组/version断言验证“配置快照→实际单次执行→内存结果”闭环，不加入生产命令或持久化表。
- [x] 7.2 网络执行暂停在可观测屏障期间Owner PATCH URL/timeout/status到新版，再完成旧请求与后续新调用；用PG18数据与fixture计数验证旧结果始终旧version/旧判定/旧deadline，新snapshot完整新版且无当前健康覆盖，gate冷却测试用受控时钟/独立scope而非放宽生产限制。
- [x] 7.3 用真实PG18测试退出/移除/撤销/禁用/清理在snapshot前后的顺序，证明系统旧snapshot与新用户API失权是独立契约、无Cookie缓存；对原配置未知Commit丢确认运行已有故障回归，确认没有自动重放或网络补偿。
- [x] 7.4 在目标慢响应时用独立PG连接/pg_locks/屏障证明probe已释放DB连接且无业务长事务/行锁，Owner配置写/退出/撤销可完成；全套User→Session→Workspace→Membership→Project→Monitor现有正常/race集成保持，不能用sleep-only或Go race代替PG锁证据。

## 8. CI与可交付安全证据

- [x] 8.1 CI保留verify及PG18 integration/race、第八阶段网络审计/CGO=0双工具来源检查/NSCD拒绝回归，另加独立probe-network和egress证据步骤；以job依赖/权限/故意缺依赖失败及最终SHA实际运行验证新旧策略分离，失败也上传证据，更新对应runner说明。
- [x] 8.2 执行全量gofmt/vet/unit/race、Python审计回归、受保护PG18 normal/integration-race和第八阶段原配置API网络审计；记录准确命令/exit与Migration1–6/API文档未改校验，失败修复后只重跑相关检查及受影响回归，不将归档旧结果计作新通过。
- [x] 8.3 执行独立Linux真实网络矩阵和绕过应用的出口证明，分别汇总静态单元/真实网络/PG18/出口四类状态及原始证据；缺环境标未通过，核对duration/FD/内存/计数、安全分类与零泄漏，托管CI证据绑定最终实现SHA。
- [x] 8.4 更新内部执行/安全边界/资源预算/无激活入口说明及spec场景→测试→证据索引，声明结果仅头部观察、不代表公开服务已通过生产SSRF验收；交付时 `openspec validate add-monitoring-engine --strict --json`、`git diff --check` 通过，全库既有strict告警单列，不修改历史规格为消警。

## Workflow follow-up

- 当前只交付Propose，全部任务保持未勾选；等待独立技术评审和用户明确Apply授权。
- 本次不创建PR/提交部署/激活，不执行生产目标探测；后续实施也不得据A提案自行增加公开入口或自动调度。
- B若被产品选择，先重新规划激活API、Migration7、租约/分布式配额与结果生命周期，并重新独立评审；当前任务不授权B。
