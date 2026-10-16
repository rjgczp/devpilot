# Tasks

本清单仅规划未来Apply，当前全部未实施。用户要求先独立技术评审；design中的产品推荐若有变更，先同步全部规划，再等待明确Apply授权。CLI显示planning完成不等于获批实现。本次不运行本清单中的数据库命令、不修改业务代码、不创建PR。

## 1. Project schema 与历史迁移兼容

- [x] 1.1 新增事务型 `migrations/00005_create_projects.sql`，实现design D1的defaults、PK/FK/RESTRICT、文本/时间/version CHECK、复合UNIQUE与creator索引；用真实PG18约束测试验证UUIDv7、默认空描述/version1、错误外键/边界拒绝，并对1–4记录SHA256不变。
- [x] 1.2 同一v5内增加不可变id/workspace_id/creator/created_at的函数/trigger及精确Down；真实PG测试验证迁往合法其他Workspace仍失败、同值赋值允许、可变文本可更新，Down无函数残留。
- [x] 1.3 新增Project迁移测试，验证0→5、含认证/成员代际/邀请各状态历史的4→5、up/up；比较旧行字段与关系，断言不生成默认Project或改写membership_id。
- [x] 1.4 在真实v5临时副本的Up末尾注入失败，断言DDL/索引/trigger函数/版本记录原子回滚且v4数据不变；再验证未修改v5可成功Up以及守护库原生单步5→4→5只删除项目资源。
- [x] 1.5 将 `TestInvitationMigration` 相关动作固定up-to4/4→3，保留其真实v4失败测试；旧registration失败链复制1–5并将失败fixture/断言移到6；运行历史v1/2/3/4 migration测试证明未被latest语义替代。
- [x] 1.6 增加临时child复合FK/并发父引用测试，验证匹配元组成功、A Workspace+B Project失败，creator退出不影响Project、User/Workspace被引用删除RESTRICT；清理临时表后Down，不以CASCADE掩盖约束。
- [x] 1.7 同步 `docs/database-migrations.md` 的v5显式发布、备份、RESTRICT/trigger、Down/forward-repair与历史测试边界；核对API启动无DDL/readiness仍仅Ping，缺v5的项目访问safe500。

## 2. Project 输入与条件协议

- [x] 2.1 新增 `internal/project/` 的输入/DTO/错误定义，复用ValidJSON但使用局部16384-byte上限；单测覆盖POST/PATCH字段presence、unknown/重复键、null/类型/空PATCH、多JSON、UTF-8/surrogate与escape/字节界限，不改全局4096认证限制。
- [x] 2.2 实现name/description规则及固定field reasons，单测验证码点/UTF-8边界、ASCII边缘trim、Unicode空白/Cc、HT/LF允许及CR拒绝、空串清空/缺省保留、SQL/markup值不执行、不规范化。
- [x] 2.3 实现规范UUID、limit/after和有界强If-Match解析；单测覆盖≤96bytes、单tag/重复/weak/*/非规范/超bigint/缺失、有效他项目tag、GET/POST错误条件header及未支持的其他条件header。
- [x] 2.4 为四个API同步OpenAPI请求、文本/byte限制、version字符串与固定DTO、tag、错误/Allow/分页规则；用文档契约测试确认已写条款与输入测试一致，不扩大现有认证/成员模型。

## 3. 当前身份与租户范围读取

- [x] 3.1 实现Project Store的detail/list，实际SQL同快照绑定digest、active User、Session状态/数据库时间、当前membership和wid/pid；真实PG验证Owner/Member、空Workspace/空末页、invalid Session401与无scope404区分。
- [x] 3.2 实现id ASC/limit+1分页，真实PG测试多页/默认值/边界/未知cursor及外租户cursor、不泄露total；受控插入和重命名测试记录fresh-page/晚提交语义，不宣称完整遍历所有并发写。
- [x] 3.3 以两个Workspace及Owner/Member/outsider/同时加入两租户用户完成SQL/HTTP隔离矩阵：替换wid、pid、组合和伪造身份；断言一致404、无外部DTO/ETag、pending邀请无读取权。
- [x] 3.4 增加退出/条件移除、Session撤销/清理、账户禁用后的fresh读取拒绝和已有快照可完成测试；通过真实邀请接受/重加证明只按当前关系恢复读取，creator退出不删除Project。
- [x] 3.5 文档写明先Workspace列表再Project列表、cursor仅下界、允许重名及creator不授予访问；对照最小响应契约测试确认不暴露creator/role/Session。

## 4. 原子创建与条件修改

- [x] 4.1 实现READ COMMITTED创建事务：User SHARE→Session SHARE→Workspace NO KEY UPDATE→actor Membership UPDATE，再新命令做live Owner/expiry门控与INSERT SELECT；真实PG验证Owner201、Member403、失权404、live身份401及Commit前外连接不可见。
- [x] 4.2 实现条件PATCH，在上述顺序后锁scoped Project，再新门控DML按wid/pid/version匹配、递增version并更新数据库时间；Store与HTTP测试证明401→404→403→428→412优先级、immutable字段不变及部分更新。
- [x] 4.3 使用两个不同Owner和独立PG连接验证同版本竞争（同字段、异字段、同值）、原tag重放、A→B→A、最大version；至多一个200，其余412，溢出500且状态不变，不刷新条件/自动合并。
- [x] 4.4 对create/PATCH逐级User/Session/Workspace/Membership及PATCH Project锁等待安排Session到期；用PG时钟/屏障/pg_blocking_pids证明门控前401、门控后到期可提交，不把transaction/旧statement时间当fresh时间。
- [x] 4.5 真实PG双向排列Project写与revoke/disable/cleanup/Owner退出/成员条件移除/受协调角色变更/邀请接受；验证保护先后、M1退出后重加Member不能恢复旧Owner写、原DELETE代际保护仍通过且无锁序环。
- [x] 4.6 验证同Workspace不同Project写等待、另一Workspace可独立完成及读快照不被行锁阻塞；并发测试必须记录实际等待而非仅sleep推断，受支持维护夹具不得冒充新角色API。
- [x] 4.7 用真实Rollback后故障/真实Commit后丢回执注入覆盖create/PATCH，断言请求Commit≤1、无二次写/补偿、确认回滚与未知结果分别取证；显式POST重复允许多项目，旧tag PATCH重试412；同步API/迁移文档的未知结果与手动恢复说明。

## 5. HTTP 集成、安全边界与故障

- [x] 5.1 接入独立Project handler及最小routes/server组装，保持现有NewRoutes调用兼容；路由单测验证四操作可达、health/auth旧行为不变、清理前拒绝所有项目编码/重复斜杠/点段/尾斜杠别名且无mutation redirect。
- [x] 5.2 实现方法→CSRF→Cookie→输入→DB顺序、原环境Cookie与same-origin proof；unit/PG HTTP测试覆盖重复Origin/header、Fetch Metadata、伪造Host/Referer、缺/错/重复Cookie、精确Allow、HEAD/OPTIONS/DELETE和零数据库副作用。
- [x] 5.3 实现固定DTO/消息/headers和条件成功回执；HTTP契约测试逐字段验证7字段、version字符串、UTC格式、Project ETag/Location、无错误ETag/Location、所有响应无Set-Cookie，更新现有认证OpenAPI白名单而非移除旧断言。
- [x] 5.4 实现5s处理/2s整个DB事务预算及有界body/drain/取消；真实TCP慢chunk/早拒绝/GET body与断连测试、无ReadDeadline transport测试确认不会隐式200、无读goroutine/连接/事务泄漏，取消不继续业务/响应。
- [x] 5.5 增加pool/lock/query/Commit超时、不可达、缺schema、死锁/未知故障、panic与恢复的unit及真实PG故障覆盖；固定500/503与name/description/id/token/digest/DSN/SQL/If-Match sentinel测试确认日志/错误脱敏，无未确认201/200。
- [x] 5.6 完成注册→登录→Workspace→邀请接受→Owner项目创建→Member读取→Owner条件更新→退出/撤销失权的端到端测试；同步README/OpenAPI的权限、无删除/归档/转移、重名/无slug、创建者保留和创建非幂等说明并核对$ref有效。

## 6. PostgreSQL 18、CI 与整体验收

- [x] 6.1 更新CI push白名单加入codex/add-project-management，保留原Go/Goose/PG18 digest及普通/race integration串行步骤；审阅workflow差异，验证主分支/PR触发与contents:read保持，禁止为通过而删除历史测试。
- [x] 6.2 对缺URL/错库/与应用库冲突/不可达/非18/缺Goose运行负向依赖检查，确认fail不skip、不读取开发.env或回退DATABASE_URL；记录实际服务器版本及固定工具版本，不并发运行两个Schema套件。
- [x] 6.3 运行gofmt、git diff --check、go mod verify、go vet、全量unit与unit-race；记录真实退出码，确认go.mod/go.sum及Migration1–4无意外改动，Project之外只有已计划接入/文档/测试变更。
- [x] 6.4 在受保护真实PG18库串行运行全量 `go test -tags=integration ./... -count=1 -timeout=120s` 和 `go test -race -tags=integration ./tests/integration -count=1 -timeout=120s`；确认前六阶段全部回归及Project并发/迁移用例实际执行，必要timeout调整必须有实测依据。
- [ ] 6.5 运行单Change strict、主Specs和全库strict验证，分别记录结果与既有长度warning，不擅自重写旧主Specs以变绿；交付verification及逐Requirement→unit/PG/HTTP/concurrency测试映射，区分本地证据与实际托管CI。

## Workflow follow-up

- 本次Propose交付后等待用户独立技术评审；产品推荐确认及新的Apply授权前不执行上述任务。
- PR创建、合并、规格同步、归档及部署均不属于本次授权；后续仅在相应授权与验收完成后执行。
