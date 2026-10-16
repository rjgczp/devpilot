# Tasks

2026-10-15 用户确认独立技术评审通过，并正式授权Apply、条件DELETE、Migration4、事务协议及完成后创建main PR。本文件记录实际实施进度，只有指定验证完成才勾选。任务中所有数据库行为只针对既有守护认可的独立 PG18 可丢弃库，不连接真实应用数据库；完成后按本次明确授权创建PR，不合并/归档/部署。

## 1. 追加模型、迁移与同组验收

- [x] 1.1 新增事务性 `migrations/00004_create_workspace_invitations.sql`，为存量成员回填数据库生成的唯一非空membership_id并设default，保留复合PK/FK/role/created_at和旧索引；以填充v3→4、up/up保持同一代际、非法/重复代际约束PG18用例及Migration1/2/3字节哈希验证。
- [x] 1.2 在同一Migration建立邀请表、来源/目标/Workspace外键、状态/时间/非自邀CHECK、仅存储pending的部分唯一索引及D4列表/配额索引；以PG18直接SQL约束、历史与新pending共存、FK RESTRICT/Workspace CASCADE及查询计划验证，不把当前时间放入索引谓词。
- [x] 1.3 完成受保护库的0→4、填充3→4、4→3→4、单步Down保留v3数据和中途失败原子回滚测试；更新旧套件固定最高版本断言并保留真正v3覆盖，将临时失败Migration4 fixture移至最高版本之后；以完整迁移套件验证而非全局替换数字。
- [x] 1.4 同步迁移文档记录回填锁成本、显式status/up/status、无启动DDL、Down数据损失及停旧写者的部署/回退约束；以隔离库演练和文档审阅确认，不在应用库执行Down或发布。

## 2. 条件 DELETE 与旧请求安全

- [x] 2.1 改动DELETE前，以当前生产SQL和真实pgx屏障暂停D_old于Workspace获锁之前，受控移除M1并用隔离fixture建立M2，再恢复D_old，记录M2被误删的红测试证据；另记录已持Workspace锁时重加确实阻塞，不把fixture加入当成已交付邀请。
- [x] 2.2 名单加入membership_id并更新精确四字段DTO/PG18/分页测试，保留按user_id排序、Cookie/隐私/Workspace DTO契约；在OpenAPI/README同组文档说明如何由名单获取 `"membership:<id>:<role>"` item validator，以名单契约测试验证。
- [x] 2.3 DELETE增加单一强If-Match解析和权限后的428、条件412，最终删除同时约束Workspace/user/membership_id/role并要求恰好1条；以unit和PG18覆盖缺失/重复/弱/通配/列表/非规范tag、权限优先级、目标缺失、角色变化、最后Owner、精确错误消息/204 headers验证，无无条件兼容路径。
- [x] 2.4 更新既有DeleteMember调用、HTTP fixture和故障/并发断言，显式保留原关系validator；以旧M1延迟/重发/自退412且M2保留、DELETE先锁再重加、重复Owner移除204/412和两Owner退出双顺序验证，不在测试helper自动刷新到M2。
- [x] 2.5 同步OpenAPI/README的BREAKING迁移说明、旧客户端428、缺失目标412、名单新增字段及禁止自动刷新条件/混合部署/旧二进制回退；以API契约测试和文档示例与实际handler一致性审阅验证。

## 3. 邀请创建与 Owner 管理列表

- [x] 3.1 实现D5协调事务与新资源作用域：创建按UUID升序先锁Actor/目标Users，再Session→Workspace→Membership→Invitation，最后新语句重验身份/Owner/目标资格和时间；以unit决策及PG18 Owner失权/目标禁用双顺序、各级真实锁和不存在目标验证，禁止持Workspace后再取目标User锁。
- [x] 3.2 实现固定7天创建、统一422、单对pending409、局部lazy expiry和100有效pending配额；以PG18无预先成员关系、创建/创建两顺序、expired替换回滚、99→100/101竞争、时区/数据库边界和未知创建Commit不自动重试验证。
- [x] 3.3 实现Owner列表的当前身份/角色单语句快照、精确Owner投影及limit/after/status=pending|all；以unit/PG18空页、上下界/非法选择器、跨租户cursor、身份/角色变化和过期effective投影/GET零写验证。
- [x] 3.4 接入Workspace invitation集合路由/严格JSON/CSRF/方法/安全日志，禁止路径清理重定向到mutation；同组更新OpenAPI/README创建/列表的请求、成功/失败、Allow和隐私边界，以unit contract、真实HTTP/PG18、慢body和无Set-Cookie验证。

## 4. 个人收件箱、状态与拒绝/撤销

- [x] 4.1 实现GET `/api/v1/invitations` 本人单语句live身份、精确Recipient投影、pending/all分页与effective expired/updated_at=expires_at；以unit/PG18不同租户/目标/第三人替换、未加入名称预览但detail/roster404、终态历史workspace_name=null且不能跟踪改名、分页快照与GET零写验证。
- [x] 4.2 实现目标decline及当前Owner revoke的空body接口、范围一致404、pending→declined/revoked、终态409和过期lazy持久化；以unit/PG18权限优先级、移除/降级/creator身份不作权限、终态重试、确认Commit和不存在/他人ID同形404验证。
- [x] 4.3 加入发起者失权/Session撤销/账户禁用后pending仍有效及其他Owner管理场景，目标自己的disabled/撤销仍401；以受控维护事务及PG18双顺序验证Workspace归属规则，不增加账户/角色管理endpoint。
- [x] 4.4 接入个人/item/action精确路由、POST/DELETE CSRF、空body单字节期限与取消，更新OpenAPI/README状态机、六项路径及所有固定失败消息；以unit/HTTP合同、HEAD/OPTIONS/Allow、RawPath/编码/额外段和日志sentinel测试验证。

## 5. 接受邀请和真实重新加入

- [x] 5.1 实现目标限定候选查询、User→Session→Workspace→Membership→Invitation锁和最终新语句门控，不要求目标已有成员才能取得Workspace保护；原子accepted+固定member INSERT，不覆盖冲突；以PG18可见性屏障、缺失关系保护、现有关系409、role注入和ownerless500验证。
- [x] 5.2 实现确认Commit后精确200接受DTO、旧accepted永不重放，以及接受后现有Workspace/member权限；同组更新OpenAPI/README显式同意/重加示例，以真实注册/登录→创建邀请→接受→列表/detail/roster→自退→新邀请重加端到端验证。
- [x] 5.3 对accept/accept、accept/revoke、accept/decline分别建立双方先获锁、首事务回滚和提交丢回执场景；以真实PG18连接、屏障和pg_blocking_pids断言最多一次加入/正确409/204/200，保留终态时间且无死锁/自动重试。
- [x] 5.4 将2.1的旧协议证据对应到真实邀请重加的最终回归，覆盖Owner移除/Member自退/延迟旧DELETE/重放M1、M2先提交和D_old先持锁的双向竞争；以精确membership_id及数据留存断言验证，禁止直接fixture插入代替最终同意流程。

## 6. 身份、过期及故障的交叉验收

- [x] 6.1 扩展既有真实pgx屏障，在User/Session/Workspace/Membership/Invitation等待后分别使Session及邀请过期；以PG时间、各边界401/409和门控后到期仍可提交验证，不用sleep猜测或事务开始时间代替最终statement time。
- [x] 6.2 对目标Session revoke/cleanup、账户disable、Owner移除/降级和创建/撤销的竞争各验证双方顺序；覆盖User→Session禁用规约、creator失权邀请仍有效、目标失效401及支持writer锁序；以真实PG18阻塞/无死锁和后续fresh读取验证。
- [x] 6.3 对Begin/所有保护阶段/状态更新/成员INSERT/Commit注入故障，并运行真实池/锁超时与缺schema；复用真实Commit后EOF及真实Rollback后EOF，断言两行原子结果、Commit至多1、无201/200/204、安全503/500、取消无响应和恢复后可继续，文档注明回执注入非物理断网。
- [x] 6.4 完成跨租户/接收者/Owner替换与响应/日志敏感sentinel矩阵，确认非Owner不可探测目标、422统一但成功有限可邀请性已承认；以unit、真实HTTP/PG18和原认证日志回归验证，无Cookie轮换/错误泄密。

## 7. CI、全量回归与证据交付

- [x] 7.1 更新既有Go CI分支触发（实际使用codex/add-workspace-invitations时），保留Go1.24.3、固定PG18 digest、Goose v3.26.0和两作业；以workflow审阅确认普通PG和integration-race串行、无应用DB回退、不把本地结果当托管CI结论。
- [x] 7.2 运行gofmt、git diff --check、go mod verify、go vet ./...、完整go test及go test -race；以实际输出验证注册/登录/登出/me/Workspace/名单/条件删除/最后Owner/health旧契约除已评审BREAKING外完整回归，依赖无需升级。
- [x] 7.3 仅在守护认可的PG18独立可丢弃库串行运行普通integration和integration-race，并负向验证缺URL/非18/缺Goose/不可达失败不skip；以实际环境版本、完整命令、退出码、1/2/3哈希和双顺序证据验证D8矩阵。
- [x] 7.4 对照四份delta逐Requirement建立成功/异常/安全/并发证据映射，复核Proposal/Design/OpenAPI/README/tasks一致并运行OpenSpec strict校验；交付明确局限、未实施公开防滥用和评审状态，不自动PR/合并/归档/部署。

## Workflow follow-up

- 独立技术评审和Apply已获用户明确批准；checkbox按实际验证更新。OpenSpec规划齐备本身不代表实施完成。
- 产品已确认：平台内UUID邀请、固定7天、Workspace归属及creator失权后有效、重复动作409、新邀请重加、100有效pending和有限可邀请性泄露。条件DELETE/validator、Migration4、锁协议已获独立技术评审及明确Apply授权，部署仍不在本次执行范围。
- 公开部署前另行确认网关限流、接收者跨Workspace频次/拒绝冷却、防注册滥用、脱敏监控、历史保留/容量与责任人；这些是发布门槛，不在本Change新增Redis/监控/通知。
- 本次用户明确授权完成后创建指向main的PR；不自动合并、归档或公开部署，等待独立代码审查。
