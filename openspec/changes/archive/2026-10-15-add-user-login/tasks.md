# Tasks

用户已正式批准Apply，包括清理Worker。验收值和错误语义以三份delta Specs及Design为准；完成状态仅依据实际测试/文档验收，不将规划校验冒充代码/数据库验收。

## 1. 密码验证与共享计算预算

- [x] 1.1 扩展 `internal/password` 严格 PHC 解析：1024-byte上限、argon2id/v19、唯一规范参数组合m=65536/t=3/p=1、固定段数/顺序和16/32-byte严格无padding Base64；表驱动覆盖错误算法/版本、零/符号/溢出/前导零/重复参数/过大成本、编码长度/换行/非规范尾位，证明非法PHC不调用Argon且不会panic。
- [x] 1.2 实现固定长度constant-time密码Verify和预生成dummy PHC，原始密码字节不变；真实Hash→Verify测试覆盖正确/错误密码、Unicode/空格、盐差异及dummy有效性，解析fuzz覆盖任意PHC且不执行昂贵计算；记录未来允许列表扩展需资源评审，不改users参数无关存储CHECK。
- [x] 1.3 建立每进程2槽无队列的共用密码预算，让注册Hash及登录real/dummy Verify经过同一预算，去掉注册独立容量且不双重获取；可控计算器测试混合同时调用数≤2、饱和错误、取消中真实返回才释放、异常最终释放及可复用；注册原有503 registration_busy/Retry-After不变，更新README共享资源说明。
- [x] 1.4 保留并适配Design第11节列出的注册全部contract/输入/原始Unicode/错误分类/取消/真实PG UNIQUE测试；补充混合Hash/real/dummy Verify计数及登录专属panic/坏PHC/token RNG/sessions缺表与创建失败后槽位复用，证明users/密码依赖健康时注册仍成功且不发认证产物；文档明确共享DB故障/容量竞争无公平或完全隔离保证，普通/race和PG18回归均通过。

## 2. Session migration、令牌及持久化

- [x] 2.1 新增原生Goose `00002_create_sessions.sql`、UUIDv7/FK级联/32-byte摘要唯一/时间CHECK及user/expiry/revoked索引，不改migration 1；同步修改集成setup/cleanup为已知完整迁移链，失败夹具使用真实1/2加坏版本3；PG18验证旧users保留升级、up/up、sessions单步down/up、完整disposable roundtrip和失败原子性；更新迁移/回滚说明。
- [x] 2.2 实现32-byte crypto/rand token、43字符规范Base64URL及SHA-256(decoded bytes)，只读当前模式下唯一Cookie；单元测试随机失败、长度/字符/尾位/padding/重复Cookie、其他传输不认证、摘要输入不能作原token，真实PG测试只持久化完整32-byte摘要且参数化值不改变SQL行为。
- [x] 2.3 扩展内部用户凭据读取并实现短READ COMMITTED创建事务：Verify后按id FOR SHARE重验active和hash快照，再插入并确认commit；PG18用锁/同步夹具验证状态/密码更新前后的顺序和无过时创建，验证重复登录独立行/不撤销其他会话，fault测试覆盖RNG/唯一摘要冲突/commit未知时不发Cookie且无自动重试；同步文档未知结果语义。
- [x] 2.4 实现数据库当前statement time、revoked为空与active JOIN的Session查询及幂等单token撤销；PG18测试到期相等/前后、重复读取不续期、disabled拒绝、独立token隔离、并发撤销/查询的快照语义、撤销后新查询失败和用户删除级联；文档注明直接SQL解禁恢复未撤销会话的限制及未来disable需事务revoke-all。
- [x] 2.5 为新仓储复用/扩展安全错误分类，未找到与连接/期限/缺表/internal分开，不把未知57014当超时；单元fault及PG18连接/statement timeout/锁等待/缺表测试验证503 service_unavailable、503 database_timeout、500 internal_error及不泄露原PgError/SQL/token，更新迁移故障表。
- [x] 2.6 在不启动清理Worker且过期/撤销行仍保留的PG18夹具上，验证默认expires_at-created_at恰好24h、并发/重复me不改行或Cookie、强制发送保留Cookie仍401、DB故障不回退Cookie/缓存身份；同组README/OpenAPI与contract测试一致声明默认24h/5m–24h范围、无滑动续期、Refresh Token或refresh路由，不以删除行证明逻辑失效。

## 3. 配置、Cookie 与 CSRF

- [x] 3.1 在 `internal/config` 加AUTH_MODE默认production、必填AUTH_ORIGIN、TTL默认24h且5m–24h；监听前校验规范Origin（无userinfo/path/query/fragment/list等）、HTTPS production与HTTP回环development/显式IP回环listener同端口。表驱动和进程测试覆盖合法值/边界/非法值/缺失及固定安全诊断，更新`.env.example`、README启动配置和既有进程测试。
- [x] 3.2 实现固定production/development Cookie策略与TTL剩余值/UTC Expires，退出按相同scope/security属性删除；HTTP/TLS handler及Cookie jar测试检查名称、host-only、Path=/、Secure、HttpOnly、显式Lax、期限、不采信Host/Forwarded和模式隔离，文档说明HTTPS入口、本地Cookie无端口隔离及不允许任意属性覆盖。
- [x] 3.3 实现两个POST的Origin完整元组+唯一X-DevPilot-CSRF:1校验，Sec-Fetch-Site若存在仅唯一same-origin；测试缺/null/重复/multi-value/恶意suffix/同站不同域或端口/Referer-only/错误头/跨站fetch及OPTIONS，无效时不读凭据/Argon/DB/改Cookie；OpenAPI/README写明非浏览器也必须两个头、无CORS且me只读无需该头。

## 4. 登录 handler 与资源编排

- [x] 4.1 抽取窄范围严格凭据JSON/邮箱规则供注册与登录共用，保留注册15–128码点政策、登录1–128码点/512-byte；handler表驱动与原注册回归覆盖4096字节/媒体/重复或缺失键/原始Unicode/空白及未知注入字段，确认400/413/415/422廉价拒绝无lookup/计算且不改注册契约；OpenAPI写清两端密码政策区别。
- [x] 4.2 实现登录real/dummy选择与统一401 invalid_credentials：不存在、错误密码、disabled、坏PHC都恰好一遍approved-cost计算，dummy匹配不能成功；调用计数及真实密码测试验证相同body/headers、无Cookie/Session/账户改写、DB错误不能当不存在，文档明确登录防枚举与既有注册409的边界。
- [x] 4.3 加2个完整登录流程槽，user SELECT前获取并持有至session提交确认/失败；共享Argon饱和或流程满均503 authentication_busy/Retry-After，无等待队列。可控阻塞测试验证第三请求不查用户、创建等待仍占流程槽但释放Argon槽、取消/5s预算/每DB2s预算/异常释放，取消后无新DB/响应/Cookie，保持health可用。
- [x] 4.4 完成200 Public DTO及提交后Cookie写出、依赖错误与panic安全响应；fault测试覆盖commit未知/超时/请求取消、未写出panic的固定500、写出后中止、日志sentinel脱敏和无重试；交付OpenAPI login所有成功/错误状态与固定消息表，contract测试核对机器可读文档。

## 5. 当前身份、退出和路由接线

- [x] 5.1 实现GET me：仅有效Session+当前active返回Public；所有无效身份相同401 unauthenticated、无Set-Cookie/续期/验证邮箱推断；测试重复/坏/无Cookie、过期/撤销/disabled和DB故障分离，OpenAPI定义安全响应及未来Workspace需独立UUID权限检查的约束。
- [x] 5.2 实现CSRF先行的POST logout：无效Cookie幂等204清当前模式Cookie，有效token无论账户状态都撤销，DB失败500/503保留Cookie；测试重复/并发退出、其他Session不受影响、未知commit的幂等重试、204空body和安全头，文档说明Cookie清除不等于撤销证据、login/logout并发响应乱序需客户端串行化。
- [x] 5.3 在 `internal/app`/`cmd/api` 组合三个路径、同一pgxpool/密码预算/logger；测试每路径真实命中、精确405 Allow/HEAD无body/OPTIONS无CORS、无关404与health原契约不变，原启动/数据库故障恢复/SIGINT/SIGTERM测试加入有效认证配置且全量通过；README加入curl cookie-jar登录→me→退出示例。
- [x] 5.4 更新README发布前置条件：登录来源/账户预算、可信代理、跨实例、防撞库与硬锁DoS风险、429/恢复窗口/告警的后续验收，以及现有注册反滥用要求；文档/Spec核对确认资源槽不冒充限流，未实现控制不标记已通过，无Workspace/RBAC/前端/Redis等范围扩展。
- [x] 5.5 将入口认证仅为查询时点身份的语义同步至API/开发文档，人工对照新Workspace边界Requirement四类场景：未来业务须定义资源动作检查、权限变更/写提交协议及时钟到期检查时点；不得宣称普通事务或middleware保证整段操作权限。验收本轮无Workspace/RBAC表、接口或授权锁协议，不新增伪Workspace测试。

## 6. Session 清理生命周期

用户最终明确批准本轮实现清理Worker，本组两项任务按原设计实施，不迁移到独立Change。无Worker情况下的expiry/revoke测试2.6仍须保留。

- [x] 6.1 实现每15m一个≤1000行/2s的过期或撤销清理批次，候选锁SKIP LOCKED、同进程不重叠、失败下一周期再试且只记安全类别；可控时钟单元及PG18测试覆盖批量边界、只删eligible、锁跳过/多worker、DB故障、缺表及无立即重试，文档说明不承诺审计留存/清理吞吐与失败不改变认证有效性。
- [x] 6.2 通过已有Runner.Workers接入清理，取消/等待/关池共用原总退出期限；生命周期测试覆盖运行中清理取消、worker先于关池退出、超时不会额外等待且health契约不变，记录可复现的验证命令。

## 7. 跨模块 PostgreSQL 验收与 CI

- [x] 7.1 完成真实PG18/真实Argon/根路由/HTTP与TLS Cookie jar端到端验收：注册→登录→me→退出→旧token401、多Session隔离、未验证active、expiry/disabled、状态/密码竞态、共享资源及撤销并发、连接/超时/缺表与恢复；断言用户数据不被登录修改、数据库无明文token、Cookie/日志/响应契约与Specs一致。
- [x] 7.2 扩展CI push纳入`codex/add-user-login`，保留main/PR和PG18 digest/Goose固定版本；gofmt涵盖cmd/internal/tests，明确运行unit/race和integration tag及顺序integration-race。验证隔离库保护与缺TEST_DATABASE_URL/Goose/错误PG版本会失败不skip、不回退开发DSN；开发文档命令与CI一致。
- [x] 7.3 在真实参数混合Hash/Verify下测并发1/2，记录硬件/Go版本/样本/p50/p95/allocs/峰值RSS并与历史基线比较，新增verification场景→测试/操作规约映射；gofmt、vet、unit/race、PG18 integration/integration-race和OpenSpec strict全部通过后记录原始结果，公开限流前置条件仍明确未交付，不为指标降安全参数。

## Workflow follow-up

- 新的Apply请求后实施；合并、同步主Specs和归档需后续工作，不通过任务勾选伪装已经完成。
- 公开部署前另行实施并评审登录限流、防暴力破解/撞库以及注册反滥用；本Change不部署共享/生产环境。
- Apply已获批准，包括清理Worker；交付PR后等待用户代码审查，不自动合并、归档或部署。
