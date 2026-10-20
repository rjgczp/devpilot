# Spec Delta

## MODIFIED Requirements

### Requirement: Dormant scheduling state never creates runnable work
原activation.next_due_at SHALL 保持数据库NULL保护，旧enabled/普通部署不得产生可运行任务。独立schedule SHALL 仅在新有限授权、完整v/g/epoch、可信fixture部署许可及资源预算均有效时形成due；API进程 SHALL 不依赖执行内核，生产执行入口保持不存在。

#### Scenario: Security - Large intent populations and restarts
- **WHEN** 大批Monitor仅有enabled意图、API重启或配置interval到期
- **THEN** 无目标DNS/TCP/HTTP、旧意图不生成run；activation.next_due非NULL仍拒绝，无每Monitor goroutine或重启逐周期补偿

#### Scenario: Exception - Unknown intent commit does not imply a probe
- **WHEN** 普通激活意图Commit未知、DB暂不可用或服务恢复
- **THEN** 不据此补发，独立执行授权和fixture许可缺失时保持blocked；已started请求遵守unknown且不重发

#### Scenario: Success - Explicit fixture authorization enables a periodic loop
- **WHEN** Owner重确认当前v/g、期限有效且独立隔离fixture部署通过许可
- **THEN** schedule可生成due，经Claim/Start/Result后推进下一周期；原activation字段不承载调度时间，生产仍硬关闭

### Requirement: Production execution requires a separately reviewed deployment boundary
真实生产执行 SHALL 继续关闭，直到另行批准生产入口并取得对应环境的独立出口/停止/节点生命周期及目标反滥用证明。A的多实例周期fixture成功、单元或旧CI SHALL 不等于生产许可；API内执行回退和环境变量解锁 SHALL 禁止。

#### Scenario: Security - Missing or unverifiable production isolation
- **WHEN** 缺生产拓扑、IPv4/6/resolver/内部公网/转换路由、最小身份、独立停止或同目标反滥用证明
- **THEN** 不开放互联网监控，生产验收标记未验证，不降低SSRF或增加解锁开关

#### Scenario: Security - Fixture success has limited scope
- **WHEN** 原76项安全测试及新周期/故障/网络拓扑验收通过
- **THEN** 仅证明对应测试SHA/拓扑，不自动启用旧Monitor、不宣称生产出口安全

### Requirement: Future worker introduction cannot activate stored intent automatically
Migration8 SHALL 在Worker入口前原子清全部旧enabled/授权版本、保留冷却，并令所有存量Activation含disabled的g恰好+1以失效旧表示tag。当前Owner重确认才可生成有限执行授权。旧DB写者/重启/迁移/配置保存 SHALL 不授予执行；fixture运行前还须全部HTTP实例完成新投影协议切换。

#### Scenario: Security - Later deployment with old enabled intent
- **WHEN** schema7存在enabled或旧API跨Migration8继续写enabled
- **THEN** 迁移前意图全部撤销；迁移后旧写也只有无执行许可的意图，零旧授权探测；新的Worker拒绝schema7和无协议1授权

#### Scenario: Exception - Upgrade fails after partially processing rows
- **WHEN** Migration8中途故障、任一g耗尽或trigger失败
- **THEN** DDL/回填/权限/Goose版本一起回滚，不留部分已获执行许可状态；Worker保持关闭

### Requirement: Acceptance separates planning database network and deployment evidence
验收 SHALL 分开记录文档/单元、真实PG18并发、race、受控Linux安全网络、分布式故障及生产出口状态。后续获授权A实现 SHALL 保留前十阶段回归，覆盖新增授权/status离线审计，不扩原精确目的白名单；计划和旧归档不是新实测证据。

#### Scenario: Concurrency - Real database proof for dormant activation
- **WHEN** 验收历史休眠意图及新有限授权
- **THEN** PG18独立连接/屏障/锁观测覆盖旧0→7/6→7并新增0→8/7→8、授权失权、v/g、quota/cooling、未知Commit及混跑升级；完整normal/race串行

#### Scenario: Security - Independent zero-network and kernel regression
- **WHEN** 运行Linux验收
- **THEN** 配置/activation/新授权/status端点仍零目标DNS/TCP/HTTP，构建来源及白名单不放宽；独立runner保留原76项并增加多Worker周期/物理kill/暂停阶段观测

#### Scenario: Exception - Missing infrastructure and planning-only status
- **WHEN** 只有Propose校验或缺PG18/Goose/IPv6/namespace/生产访问
- **THEN** 未执行项独立记录，显式安全job缺依赖失败而非skip；生产项未验证/关闭

## ADDED Requirements

### Requirement: Due work is bounded fair and uses database time
到期判定 SHALL 用等待后新数据库语句时间和完整授权版本，扫描/领取有界且跨Workspace轮转。重启/积压 SHALL 跳过错过周期，不逐次补偿；下一due不早于DB当前时间加interval及既有Monitor间隔限制。

#### Scenario: Concurrency - Large simultaneous tenant backlog
- **WHEN** 1000Workspace同时到期或24h积压恢复
- **THEN** keyset轮转、每tick最多32候选尝试、每W最多领取1次，不建立每Monitor常驻goroutine；有明确饥饿/资源观测

#### Scenario: Exception - Clock and authorization boundaries
- **WHEN** 数据库期限已到、时钟后跳或配置间隔变化
- **THEN** 严格过期禁止新Start，持久not_before不缩短；大幅时钟异常关闭新执行，不使用事务开始时间冻结授权

### Requirement: Claims serialize complete parent scope before child leases
Claim SHALL 按候选无锁读取→Owner User SHARE→Workspace NO KEY UPDATE SKIP LOCKED→Membership→Project→Monitor→Activation→Schedule→Run→Budgets→Reservation执行，每候选独立READ COMMITTED事务。所有保护等待后 SHALL 用新SQL语句重查Owner/成员代际/scope/v/g/epoch/数据库期限并门控实际DML。后台身份不使用用户Cookie。

#### Scenario: Concurrency - Two workers race with user writes
- **WHEN** 两Worker领取同due，同时Owner Pause/PATCH或Project更新
- **THEN** User SHARE必须早于Workspace SKIP LOCKED，后者跳过即回滚释放User；同scope最多一个有效fence，所有等待后新语句门控身份/Owner/代际/scope/v/g/epoch/期限，候选不是授权

#### Scenario: Security - Active Owner witness disappears
- **WHEN** 后台选中的Owner在取锁前退出、降级或禁用
- **THEN** Claim/Start锁后新SQL验证失败即回滚全部行锁与新Run/预订，下一轮无锁重读并新事务选人；不能持Workspace补锁另一User、不能同事务换候选或用锁前CTE授权

#### Scenario: Concurrency - Two Owners witness competing worker claims
- **WHEN** 两Owner U1/U2作为同或不同候选、两个Worker用真实PG18独立连接同时Claim同scope
- **THEN** 屏障及pg_locks/pg_blocking_pids证明各自先User后W、W跳过即释放所有锁、没有先Run/Activation后W，最终至多一个有效fence；失效U1必须全回滚后新事务选U2

#### Scenario: Concurrency - User PATCH waits without a reverse User dependency
- **WHEN** Worker1持U1 SHARE及W，U2的用户PATCH持U2/Session等待W，Worker2候选U2同时尝试Claim
- **THEN** Worker2在W SKIP后结束事务释放User锁，Worker1不得持W后补锁U2；真实PG18锁图证明无反向依赖，PATCH随后可完成，不能只以无死锁或sleep断言锁序

#### Scenario: Exception - Protective waits are bounded and revalidated
- **WHEN** User/Membership/父资源/预算/control发生等待、表锁阻塞、候选代际改变、超时/deadlock/取消
- **THEN** 等待受250ms/事务2s预算，SKIP不声称免表锁等待；等完须用新SQL取新快照和等待后的DB当前时间。若封装于受限函数须VOLATILE并在最终SQL/SPI命令一次clock_timestamp取gate_now，不误用外层调用开始时的statement_timestamp；失败全回滚，Commit未知只对账原run_id

### Requirement: Claim start and result have separate confirmed commit gates
Claim Commit SHALL 只授租约，Start Commit SHALL 重查完整授权/lease/fence/stop与许可后授单次发送能力，Result Commit SHALL 独立保存结果。网络I/O SHALL 不持业务事务、行锁或数据库连接；任一未确认Start不得下发Probe输入。

#### Scenario: Exception - Unknown claim commit
- **WHEN** Claim真实提交但ACK丢失
- **THEN** 只以预定run_id和incarnation对账，不创建替代同周期run；确认有效leased才可Start，否则不发送

#### Scenario: Exception - Unknown start or result commit
- **WHEN** Start ACK丢失，或Result ACK丢失
- **THEN** 前者不重建/重发Probe而保守unknown；后者只按固定result_commit_id对账/幂等同结果，不重做GET

#### Scenario: Concurrency - Network runs after database resources are released
- **WHEN** DNS/TCP/TLS/HTTP被fixture卡住
- **THEN** 池连接与pg_stat_activity/pg_locks实测证明无该Probe持有DB连接/业务事务，API仍可Pause/PATCH

### Requirement: Leases fencing and physical execution are separate lifecycles
Lease SHALL 使用DB时间、固定owner/incarnation和单调fence；Start后不得重新租给别人重发同run。heartbeat只更新有效租约，不能延长物理硬截止；过期started SHALL unknown并保留物理槽直到独立终止回执。失效run fence/回收代次不能写结果或重获许可；schedule代次变化只允许有效旧run留原谱系审计，不更新当前观察。

#### Scenario: Concurrency - Expired unstarted lease is reclaimable
- **WHEN** leased尚未Start且lease过期
- **THEN** 原run expired、旧fence无效，确认无Probe/permit才可释放；未来正常周期用新run，不把旧run改回待发送

#### Scenario: Concurrency - Expired started lease is not a proof of termination
- **WHEN** 心跳断开但旧Worker仍可能发包
- **THEN** 拒绝旧写并quarantine，未确认切断旧物理权限前禁止返还槽或同Monitor下一run；重连也不能恢复旧能力

#### Scenario: Exception - Fence or epoch exhaustion
- **WHEN** 正bigint无法继续递增
- **THEN** 整事务失败且不回绕，不生成许可/任务或部分资源预订
