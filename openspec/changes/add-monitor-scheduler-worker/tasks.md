# Tasks

本清单仅供后续单独获授权的 Apply 执行，本次 Propose 不实施、不勾选。依赖顺序见各组；生产网络开放不属于任何任务的默认权限。所有PG生命周期套件仅对testdb守护通过的可销毁PG18串行运行。

## 1. 评审门禁与隔离执行边界

- [x] 1.1 记录A第一轮原则通过及两项修订的再次独立审核，并在代码前取得产品对生产硬关闭、Start Commit暂停/期限文案、24h上限、旧字段废弃/常量breaking change/全部旧tag失效/HTTP切换和预算的明确批准；以回执及新的Apply授权核验，不把修订授权当实施批准。
- [x] 1.2 固定批准工件、基线和Migration1–7 SHA256清单，核对HEAD/main/origin与最新工作区；验收输出差异说明，发现新代码或规格变化先更新方案。
- [x] 1.3 建立fixture-only Controller/Worker与Probe测试构建边界和API依赖静态契约；以普通发布产物无Probe入口、任意环境开关不能运行、API无执行包依赖的测试验证，并在运行文档说明。
- [x] 1.4 在受控Linux runner做PG18/IPv6/namespace/独立Supervisor工具预检；验证依赖缺失job非零失败、无上游路由，保存镜像/工具链来源；不能用旧测试namespace作生产许可。

## 2. Migration8、授权隔离与权限

- [x] 2.1 实现受审Migration8的独立schedule/run/budget/reservation/control对象、完整复合FK、状态/时间/单调fence/epoch约束及未物理释放run的部分唯一限制；真实PG18负向SQL覆盖跨租户/身份变更/非法状态/双活run，核对字段无URL/DNS/凭据。
- [x] 2.2 实现旧enabled原子撤销与全部存量Activation含disabled的g恰好+1一次/upgrade_revoked/actor=NULL，使表示协议升级前强tag全部失效，enabled不额外+2；冷却及原next_due=NULL/DELETE/TRUNCATE/revision guard保留；多W/混合状态/冷却的7→8、0→8、up/up快照证明重复up不再推进。
- [x] 2.3 实现g变化向下清schedule授权/next_due、推进epoch及延迟登记上限；用旧版本Store/SQL与旧角色作为非接流量DB写者，双顺序证明旧写只能撤销不能授予，新授权/配置变更整体回滚；不将旧HTTP接口接入fixture运行流量。
- [x] 2.4 实现非LOGIN函数owner、固定search_path及最小EXECUTE运行角色；真实LOGIN测试拒绝用户秘密/DDL/直接DML/PUBLIC/TEMP/伪造incarnation，旧角色无新授予函数；文档要求排空旧superuser连接。
- [x] 2.5 完成真实8中途故障、enabled及disabled任一g溢出/trigger失败的全事务回滚、无schema8安全失败及可销毁库8→7→8；验证无部分tag/许可切换，Down不恢复旧enabled/不缩冷却，历史next移9且旧schema边界固定，文档覆盖全部tag失效与回退协议。

## 3. 有限期Owner授权与离线API

- [x] 3.1 实现execution-authorization严格JSON/期限/profile/强If-Match及固定错误契约；单元与HTTP测试覆盖<=1024字节、重复/未知键、Unicode、body版本、428/412和401/404/403优先级，OpenAPI记录准确DTO。
- [x] 3.2 实现同事务最终Owner/Session授权、g+1、100/W及60s冷却与schedule有限授权；PG18每级锁后过期、两Owner同tag、失权/重新入Member、未知Commit和冷却不可重置测试通过。
- [x] 3.3 保持Activation字段名/string类型、权限/Owner意图及强ETag格式，明确废弃旧执行字段为not_reported/execution_status_required及成功头intent-v2；OpenAPI/客户端迁移/契约测试验证此BREAKING常量变化，schema7 fixture保留历史旧值，配置DTO/ETag及Cookie/CSRF/5s/2s/零目标审计不变。
- [x] 3.4 实现严格授权过期/显式续期及无active Owner阻止新Start；用PG数据库时间/角色变更双顺序验证，Session/logout不被错误当作Workspace后台凭据或自动续期理由。
- [x] 3.5 交付弃用/客户端切换及HTTP升级回退协议；以合同测试验证两个旧字段仅是迁移提示、status不支持/404/503只显示不可用，后台状态变化不改变Activation body/ETag/updated_at/g，不能以动态运行摘要破坏强tag；文档列closed-enum客户端breaking和回退全体tag失效条件。

## 4. 调度状态与父序领取

- [x] 4.1 实现正交状态机、数据库时间、单调epoch/fence及固定stop/blocked分类；单元/race覆盖全部转移/溢出/时钟后跳与异常，文档给状态表，不将unknown编码为HTTP失败。
- [x] 4.2 实现D6无锁候选→候选Owner User SHARE→W NO KEY UPDATE SKIP LOCKED，不得先W后User；每候选独立RC事务、W跳过/候选失效全回滚释放User再重选；PG18两Worker竞争证明最多一有效fence，限制64候选W/8个W内候选/32次尝试/每W一领取。
- [x] 4.3 实现Membership→P→M→Activation→Schedule→Run→Budgets→Reservation完整父序及D6逐语句协议；受限VOLATILE函数在全部等待后新SQL/SPI快照重查active/Owner/membership_id/scope/v/g/epoch，并一次clock_timestamp取gate_now门控DML，不能误用外层调用开始时间；PG函数内部真实阻塞跨期限/超时全回滚测试通过，不用锁前CTE或持W补User，用户API协议不变。
- [x] 4.4 实现Claim ACK未知对账、未Start lease过期终止及未来周期重排；真实提交后丢确认与回滚两对照，证明不创建同周期替代run、不补发旧周期。
- [x] 4.5 用真实PG18两Owner U1/U2、两Worker独立连接/屏障验证同或不同候选同时Claim，hint后Owner禁用/退出/降级/移除重入的双顺序；pg_locks/pg_blocking_pids及事务时间线证明失效U1全回滚后新事务才能选U2、W skip立即释放User、无Run/Activation先W，不能用sleep-only/无死锁代替。
- [x] 4.6 用PG18独立用户PATCH连接制造Worker1持U1/W、U2用户PATCH持U2/Session等W、Worker2候选U2争用；锁观测证明W2 SKIP后释放User且W1不持W补U2，PATCH可继续，最终fence/授权/配置符合提交顺序；保存锁图及两个顺序证据。

## 5. 集群槽、令牌与同目标预算

- [x] 5.1 实现global/W/P/M持久原子槽预订与固定顺序锁；PG18多实例竞争最后槽/取消/跨租户证明32/4/2/1上限，禁止无锁count，加入单元/race及预算文档。
- [x] 5.2 实现global8/s、W1/s、P每2s及Monitor持久not_before、整数token和有界key容量；真实并发与时间边界覆盖新v/g/interval/重启不缩短间隔、失败不退token、容量满安全拒绝。
- [x] 5.3 实现控制面HMAC origin/IP预算和fixture目标目录；跨W同origin/别名同IP实测1/2槽及60s/1s限速，确认不保存Path/原始DNS/IP、不向用户泄漏hash/其它租户；密钥轮换保留旧预算1h测试通过。
- [x] 5.4 实现quarantine与独立物理终止回执绑定的槽释放，回执只改资源不伪造网络结果；PG与真实僵尸Probe测试证明lease过期不返槽、重连旧incarnation无新权限，补充安全优先导致blocked的运维说明。

## 6. Start、无凭据Probe与独立出口监督

- [x] 6.1 实现Start事务重新校验v/g/epoch/lease/Owner/期限/stop与fixture许可，取完整冻结快照且Commit确认前不下发；PG屏障覆盖Claim/Pause及Start/PATCH双顺序、Start ACK丢失不重发，记录无DB事务跨网络。
- [x] 6.2 实现单run有界匿名pipe/单次信封及私有内核桥；真实进程测试证明URL不在argv/env/log、重连/重启不恢复发送能力、Probe无DB/用户/云凭据或管理socket，保留128MiB/CPU1/PID128/FD256等资源约束并文档化。
- [x] 6.3 在原完整DNS验证与literal拨号之间实现只收紧权限的target-permit钩子；单元/race与独立网络测试证明失败/断连/ACK未知零dial、等待计入原timeout、无弱化HTTP/DNS实现，原76场景全部通过。
- [x] 6.4 实现每run namespace/veth、双栈默认deny、精确Resolver与受控80/443、不可延长的短期permit；接收端/防火墙/路由证据验证规则先于Probe、恶意bypass被独立阻断、Probe不能改规则，记录仅fixture适用。
- [x] 6.5 实现独立Supervisor紧停/现有流切断/进程终止回执、漂移拒绝及namespace重建代次；真实TLS连接后切断、Supervisor失联、规则丢失和旧节点重连测试证明新执行关闭且旧槽不盲目回收，交付停止/恢复手册。

## 7. 结果、unknown与最小状态投影

- [x] 7.1 实现完整谱系/原固定网络failure_code及独立stop_reason最小结果持久化；敏感sentinel与SQL字段检查覆盖URL/DNS/Header/Body/raw error/凭据不入库，预检/许可拒绝不伪造目标失败。
- [x] 7.2 实现run fence门控与当前v/g/epoch投影门控分离、Result固定commit_id幂等对账；PG真实丢ACK/旧fence/暂停后旧结果/重新授权同version场景证明只留旧审计、不覆盖当前观察、不重做网络。
<!-- 7.2 legacy network convergence: user-authorized BREAKING fixture protocol; acceptance tracked separately in verification.md, no new task or 7.3 authority. -->
- [x] 7.3 实现started→unknown及迟到结果拒绝；按产品已接受范围，缺少可信且可区分的持久许可拒绝证据时保守unknown，不推断canceled，精确canceled分类后续另审；强杀/分区/Commit未知实测证明unknown不退leased、不补发，物理回收后下个正常周期可以继续。
<!-- 2026-10-28: Product explicitly accepts conservative unknown scope at a1fc65a; 7.3 accepted, 32/47. Precise canceled provenance is deferred, not implemented. Historical IPv620s timeout remains unresolved. See evidence/task73-final-review/product-acceptance.json. This batch authorizes only 7.4, not 7.5. -->
- [x] 7.4 实现唯一权威execution-status实时Owner/Member读取、同语句意图/授权/调度/attempt正交投影及最近记录各最多1条；HTTP/PG测试覆盖失权/混合scope、unknown不被旧success掩盖、版本不符null、无URL/actor/lease详情/固定错误/预算，OpenAPI及客户端明确不能从Activation迁移提示推断运行。
- [x] 7.5 按D11.3做真实PG18/HTTP跨接口矩阵：disabled、authorized_waiting、due、running、blocked、unknown、配置失效、期限过期及completed；无并发窗口验证固定迁移对与权威观察不矛盾，屏障覆盖两读间v/g变化须刷新、Pause/PATCH后旧在途仍可见、unknown物理回收前后in_flight变化；保持零目标审计。

## 8. 保留上限与磁盘预算

- [ ] 8.1 实现M100条/7天、W10000、全库100000终态+32活跃、schedule/预算key各10000的原子容量账；并发插入/终态转换/无清理空间测试证明硬cap与retention_capacity，pause/PATCH不受阻，更新容量文档。
- [ ] 8.2 实现父序keyset清理每事务<=100行/2s及投影指针原子维护；PG并发验证不删active/quarantine/冷却key、不反向锁、无悬空引用；schedule登记回收不得重置仍有run的fence。
- [ ] 8.3 测量周期负载下表/索引/WAL/vacuum与日志增长，验证1GiB运行预算/10GiB测试盘及80%拒绝新Claim；保留实测预算和运维清理文档，不能把行数cap当物理磁盘保证。

## 9. 多实例周期与故障集成验收

- [ ] 9.1 交付可复验tests/scheduler/run-docker.sh及README，创建独立PG控制网、两个以上Worker、可信Supervisor和无上游双栈fixture网；以真实Owner授权和60s interval连续至少三个周期验收最小结果/下一due，不连接真实公网目标。
- [ ] 9.2 在七个暂停/PATCH阶段建立事务/网络屏障，保存PG锁及独立接收端GET时间线；验证1–3阻止Start、4–7准确承认在途窗口、旧结果隔离，禁止用sleep-only或CAS断言物理停止。
- [ ] 9.3 真实注入Claim前崩溃/三种Commit丢ACK、GET后SIGKILL、DB分区/重连、租约过期僵尸/节点重建；观测同run无自动再发送、unknown可丢结果、无终止证明槽不返回，恢复后只有正常新周期。
- [ ] 9.4 运行24h级虚拟时间积压和1000W同时到期/同目标压力测试，另保留真实时间周期；证明无逐interval补偿/每Monitor goroutine，记录p95、公平轮转、资源/磁盘界限而不宣称无限租户严格公平。
- [ ] 9.5 保存原76项+新增网络/权限故障测试的独立receiver/规则/路由/trace/cgroup证据及正负控制；故意绕过child0/audit1必须被检测，正常目标真正收到GET，生产出口明确未验证。

## 10. CI及完整回归交付

- [ ] 10.1 扩展CI push分支及独立scheduler-security job、guarded PG18 normal/race串行步骤；在同一实现SHA验证缺依赖失败、原verify/integration/execution-security继续成功，Artifact绑定工具/镜像/命令/exit。
- [ ] 10.2 运行原配置/activation并覆盖新授权/status的Linux零目标审计，保留精确白名单/CGO来源/NSCD反例与校准；以目标DNS/TCP/HTTP零尝试证明API边界，不增加Probe目的豁免。
- [ ] 10.3 执行gofmt、go vet、unit/race、PG18完整integration及integration-race、execution snapshot normal/race与Migration1–7哈希复核；仅对实际通过项登记，失败证据与修复留存，不把历史CI当本SHA通过。
- [ ] 10.4 建立每个Delta场景→实际test→证据路径的映射、严格OpenSpec校验和全库历史告警对比；交付独立技术/安全审查包及生产关闭、未验证能力清单，等待后续明确授权，不自动合并/部署/Archive。
- [ ] 10.5 集成验收客户端先升级→排空旧HTTP/写者→Migration8退役全部tag→逐实例intent-v2/status就绪→fixture放行及反向回退；旧HTTP仍接流量或有未确认物理执行时门禁必须拒绝；旧API非接流量DB写者仍验证无授予。保存upgrade/rollback/disabled溢出失败证据，绝不宣称旧const值兼容或开放生产。

## Workflow follow-up

- 本次Propose工件完成后停止；等待独立设计评审和产品对范围/暂停/部署条件确认，再由新请求授权Apply。
- 后续实现PR、合并、规格同步、Archive和部署各按届时明确授权处理；本轮不创建实现PR，不打开生产执行。
