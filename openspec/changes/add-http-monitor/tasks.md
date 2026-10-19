# Tasks

2026-10-19用户确认全部MVP产品决策、独立设计评审通过并正式授权Apply及创建PR。每项测试和文档与相应实现同组落地；本轮OpenSpec文档验证不代表业务/迁移测试已运行。

## 1. Review gate and input foundation

- [x] 1.1 记录design末表全部产品选择的确认与独立技术评审结论；如有调整同步proposal/design/三份delta/tasks，验证`openspec validate add-http-monitor --strict`且确认用户已单独授权Apply，未通过则不进入实现。
- [x] 1.2 新增Monitor纯配置模型及strict JSON解析，复用现有原始Unicode验证协议，覆盖必填/缺省/PATCH至少一项、重复escaped key/null/未知不可变字段/整数token和int64界限；通过对应unit case，确认无客户端actor/role输入。
- [x] 1.3 实现name规则、GET/interval/timeout/status及合并配置校验，明确POST defaults和PATCH保留语义；unit验证边界、固定422原因与同值配置，README/API schema同步字段及“配置未执行”说明。
- [x] 1.4 选择并固定与Go1.24.3及现有x/text兼容的IDNA依赖/profile，保留非transitional/严格label/Bidi/round-trip；以module graph/toolchain审查和合法/非法A-label unit证明，不隐式latest或升级Go。

## 2. Offline URL policy and privacy

- [x] 2.1 实现design§3完整纯URL校验/规范化（原始标记、scheme/authority、DNS/punycode/特殊后缀、默认port、escaped path和双限长）；表驱动unit逐项覆盖示例及规范化幂等、path不清理和origin不变。
- [x] 2.2 补齐危险URL corpus：所有IPv4/IPv6及数值别写、公私网/mapped/zone/NAT64/转换字面量、metadata名、query/fragment空标记、userinfo/host编码、Unicode/尾随点/IDNA、控制/百分号及端口；unit和有界fuzz证明不panic/不放宽既定规则，测试只处理字符串不联网。
- [x] 2.3 建立可复验的零目标网络验证：审核Monitor依赖图无resolver/dial/HTTP client，并为Linux CI子进程设置可审计的出站拒绝/调用探针，只允许明确测试DB和本地测试HTTP连接，DNS及其他目标连接意图即失败；交付隔离配置/命令及zero-attempt结果，并以隔离环境人为违规DNS/连接正向对照证明探针会检测并使对应运行失败，保存命令/退出码；不能仅使用不可达超时或DefaultTransport替换充当证明。
- [x] 2.4 加入敏感URL path/host/query sentinel失败与日志单测；验证固定消息、列表无URL、无body/header/SQL/DSN/actor/资源ID泄漏，并在README/API文档说明单项URL可被当前Owner/Member读取、无秘密保险箱承诺及A/B安全边界。

## 3. Migration six and historical boundaries

- [x] 3.1 新增真实`00006_create_http_monitors.sql`，实现最小列/default/CHECK/PK/三元组UNIQUE/复合Project与creator RESTRICT FK、creator索引和不可变trigger/function；PG18 catalog/default/约束测试验证精确对象、无多余结果/状态表及无冗余索引，记录Migration1–5 SHA256不变。
- [x] 3.2 加入PG18固定归属和直接SQL保护测试：跨租户FK拒绝、同租户换Project亦不可变、等值赋值允许、必填NULL/长度/范围/时间/version非法拒绝、DB与应用验证子集差异；临时child三元组引用、父删除/子插入双顺序验证无孤儿，creator失权后数据保留。
- [x] 3.3 增加0→6、含完整旧数据5→6、重复up及原生6→5→6测试；新历史快照包含Project/成员generation/邀请/Session，核对旧值不变、无默认配置、guarded Down仅删除新对象，结果及限制更新`docs/database-migrations.md`。
- [x] 3.4 在临时副本真实6全部DDL后注入失败，验证table/index/trigger/function/applied6均回滚且旧数据保留；独立验证通用next-version失败7，证明缺URL/错误DB/应用DB/非PG18/缺Goose/不可达均fail不skip不fallback。
- [x] 3.5 固定`registration_test.go`及历史Workspace/邀请/Project迁移1–5的up-to/down边界，旧当前失败6移至7并复制真实1–6；真实v5失败仍测试5，完整历史migration测试正常通过且未删旧断言，schema suite串行。

## 4. SQL-authorized reads and writes

- [x] 4.1 实现Monitor scoped detail/list同语句快照门控，绑定可信digest→active User/Session→Membership→Project→Monitor，列表授权sentinel及limit+1/id ASC；真实PG18验证Owner/Member/空Project、401/404、Store直接调用、cursor仅范围且无unscoped existence lookup。
- [x] 4.2 实现有界事务POST、User SHARE→Session SHARE→Workspace NO KEY UPDATE→Membership UPDATE→Project UPDATE，保护后新SQL门控实际INSERT、creator从可信身份派生；PG18验证双父匹配和Owner-only原子创建，并检查零目标网络。
- [x] 4.3 实现PATCH追加Monitor UPDATE、完整三元组/version原子更新、合并验证、同值+1/非递减时间与bigint耗尽；PG18断言missing428/stale412、Member403、错scope404/invalid identity401优先及旧版本不变，无Project属性/version写入。
- [x] 4.4 加入真实注册/登录/邀请accept建立的Owner/Member隔离矩阵：outsider/双WorkspaceActor、wid/pid/mid替换和组合、失权Cookie/邀请无权、creator退出、其他租户不受影响；Store与HTTP预期一致，补充API权限/投影文档。
- [x] 4.5 实现确认Commit才成功、单次提交和故障分类，复用现有错误协议；真实Rollback后fault、Commit后丢ACK、pool/锁/DB timeout、schema5缺表、request cancel和live dependency cancel测试断言状态/次数/固定503或500，无retry/补偿/自动刷新tag。

## 5. Deterministic PG18 concurrency evidence

- [x] 5.1 使用不同Owner、独立连接和DB等待观测/屏障，覆盖同tag同值/不同字段、ABA、溢出、PATCH响应丢失旧tag重放；普通及race集成最多一个消费原版本，结果412且无丢失更新。
- [x] 5.2 按写先/变更先覆盖Monitor POST/PATCH对Session撤销/cleanup、账户禁用、Owner退出/移除、role变化/退出重入Member；受协调fixture不新增公开接口，真实PG18断言401/404/403及旧member-generation保护仍有效。
- [x] 5.3 分别阻塞User/Session/Workspace/Membership/Project/Monitor保护锁，确认真实等待后推进数据库expiry，最终新statement拒绝401；单独控制final gate后到期允许提交，记录pg_locks/pg_blocking_pids或等价屏障证据，禁止sleep-only证明。
- [x] 5.4 验证Project PATCH与Monitor创建/修改两个顺序、FK隐式父键/creator锁、邀请/member交互、独立Workspace进展、共享身份合法争用；用真实锁观测排除受支持协议反向锁/升级，不改变Project ETag，并覆盖deadlock安全分类无自动重放。
- [x] 5.5 验证新旧read snapshot两种授权时序及跨页变化/低cursor晚Commit；普通PG18及race结果保留“每页当前授权、无遍历快照承诺”，未来同快照config+version契约记入开发文档且不添加调度器。

## 6. HTTP integration and API compatibility

- [x] 6.1 实现Monitor Handler并在`internal/app/routes.go`的Project截获之前分派，在server注入Store，旧可选Handler调用保持兼容；真实server路由测试覆盖四操作、精确Allow、HEAD/OPTIONS、RawPath/点段/双斜线/尾随/extra action无redirect，旧Project/Workspace/health路径回归。
- [x] 6.2 接入Cookie-only/CSRF、strict body/media/selector/condition header与预算，transport unit及PG HTTP覆盖伪造Origin/重复proof/替代凭据、body raw上限/Unicode/整数/空PATCH、slow/chunked与早拒绝drain≤预算，无拒绝请求DB/目标网络副作用。
- [x] 6.3 实现最小单项/列表DTO、强monitor ETag、Location和安全headers；contract/PG HTTP tests精确断言URL仅单项、version字符串、UTC时间、全部固定error/fields、无Set-Cookie/失败成功元数据、If-Match语法与428/412；同步OpenAPI准确类型、响应和示例。
- [x] 6.4 覆盖panic前写500/后写abort、取消后无响应及无解释dependency cancel固定500，检查URL/凭据/SQL sentinel日志不泄露；端到端创建/修改/读取均用§2.3探针证明零目标DNS/连接，README明确无运行/启停/删除及未来SSRF契约。

## 7. Whole-system acceptance and review handoff

- [x] 7.1 CI加入`codex/add-http-monitor`触发和可审计零目标网络验收步骤，保留固定PG18镜像/Goosev3.26.0及既有普通/race命令；检查workflow中schema测试串行、仅独立测试DB且无skip/fallback，记录实际运行结果。
- [x] 7.2 运行格式检查、`go vet ./...`、unit及unit-race、全PG18 integration及顺序integration-race；前七阶段所有API/迁移/并发回归通过，不能把URL输入unit、Go race或planned case当执行期SSRF证明。
- [x] 7.3 完成verification记录/spec-to-test映射，区分本地与hosted CI结果、真实锁/commit/迁移哈希证据、未实现B阶段验收；`openspec validate add-http-monitor --strict`通过，既有主规格长度警告单独报告，不借机改主规格。
- [ ] 7.4 独立技术评审验证URL规范化/IDNA、无网络证据、SQL全归属、FK锁与等待expiry、未知Commit和迁移边界；修复评审发现并仅重跑受影响检查，交付可供用户审核的实现及验收记录，创建指向main的PR，记录提交SHA/CI状态，等待独立PR代码评审，不自动合并或归档。

## Workflow follow-up

- 用户已授权Apply及创建PR；不自动合并、归档、部署或激活监控。
- 后续实现结束后，按用户届时授权创建PR、完成独立PR Review，合并后才按单独归档请求执行Archive及主规格同步。本轮不执行这些动作。
- 未来探测引擎Change必须单独设计激活/存量处理、config_version在途语义，并实现/验收DNS/IP/绑定dial/TLS SNI/redirect/proxy/出口限制和资源预算；这些不是当前实现清单中的“已完成”或“跳过”测试。


实施与验收进度：30/31。7.4中PR创建和交接已完成，独立PR代码评审及其可能发现的修复依用户要求保留未完成；不将本地自检或托管CI等同独立代码评审。


2026-10-19第一轮独立PR审查：`d6914e8` 的网络审计失败作为阻塞项处理，当前不批准合并。本轮下载原Artifact并逐条记录六次NSCD调用，以进程trace和仅ParseConfig的libc标记对照确认来源；保留AF_UNIX全面拒绝，统一纯Go审计构建并加入双工具元数据门控及7项回归。全量本地Go/race及PG18 integration/integration-race重跑通过，证据见verification的review1章节；最新Push/PR CI按明确SHA实际采集。7.4继续未完成，等待第二轮独立代码审查，不能以实现者自检或CI替代。
