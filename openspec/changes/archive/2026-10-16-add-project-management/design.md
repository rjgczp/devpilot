# Design

## Context

动机见 proposal.md。以下区分已观察基线与待实施推荐，所有 SQL 描述均为设计，尚未运行。

- 2026-10-16（Asia/Shanghai）开始时工作区干净，local main、origin/main 和 GitHub main 均为 `ce7e41c378d2add28697509f85725e0f31d92129`。Git fetch 因 `.git/FETCH_HEAD` 只读失败，ls-remote 因本机代理不可达失败；随后通过 GitHub 连接器实时解析 main commit 及 PR #6，确认远端一致、PR 已 merged，merge commit 为 `edcb73fb2d79196803ce12c988ca50ae13bb8a55`，没有假定陈旧远端引用是最新事实。
- 第六 Change 位于 `openspec/changes/archive/2026-10-16-add-workspace-invitations/`，29/29 tasks 已勾选，verification 记录四份 delta 同步、归档及历史测试；开始时 `openspec list --json` 无 active change。现有代码确实包含邀请接受/退出/重加及条件 DELETE。
- `internal/workspace/store.go` 的读取使用一个 identity CTE 快照；`member_delete.go`、`invitations.go` 的写操作为 READ COMMITTED，按 User SHARE → Session SHARE → Workspace NO KEY UPDATE → Membership UPDATE → Invitation UPDATE（需要时）保护，再以新语句检查有效期。邀请涉及多个 User 时按 UUID 排序。Session 清理使用 SKIP LOCKED。
- `internal/app/routes.go` 对 members/invitations namespace 绕过 ServeMux 清理，拒绝路径别名；不能仅给通用 workspace handler 增加字符串分支而让新路径落入 health fallback 或重定向。
- `internal/credentials/input.go` 的 ValidJSON 拒绝非法 UTF-8 和未配对 surrogate；全局认证 body limit 为 4096。新项目描述需要局部更大上限，不能增大全局认证限制。
- Migration 1/2/3/4 是既有只增不改链。`TestInvitationMigration` 当前使用 latest up 和单步 down 假设 v4 最高；`TestMigrationAndConstraints` 复制1–4并写临时失败v5。这两处需要明确修正版本边界，不能把旧v4测试实际变成v5测试。
- CI 为 `.github/workflows/go-ci.yml`：Go1.24.3、Goose v3.26.0、固定PG18 image digest、verify/integration两作业，普通 integration 与 integration-race 顺序执行。测试守护只接受 `devpilot_registration_test`，再次检查服务器为18且不允许 DATABASE_URL指向该库。
- 归档已记录全库 strict 的已知格式问题：user-authentication requirement[11]；workspace-access [7]/[8]；workspace-members [2]/[3]；workspace-persistence [1]/[8] 描述超过500字符。不得把局部strict通过宣称全库通过，也不擅自重写这些已审核条款。

## Goals / Non-Goals

**Goals:** 建立可信租户归属、数据库引用约束、当前成员权限和可检测并发冲突；使失权/撤销与写操作的先后语义可在真实PG18验证。

**Non-Goals:** 不实现 Monitor、Incident、Alert、AI、前端、项目级成员/RBAC、计费、缓存/队列、项目转移、删除/恢复、审计平台、创建幂等键或认证/Workspace重构。既有Cookie、邀请和成员契约不变。

## Decisions

### D1. 数据模型与身份

| 字段 | PostgreSQL 设计 | 含义/写入规则 |
| --- | --- | --- |
| id | uuid PK DEFAULT uuidv7() | DB生成UUIDv7；API不能指定或修改 |
| workspace_id | uuid NOT NULL FK workspaces(id) ON DELETE RESTRICT | 唯一租户归属，永久固定；没有项目迁移接口 |
| name | text NOT NULL | 1–100码点、≤400 UTF-8 bytes；不唯一 |
| description | text NOT NULL DEFAULT '' | 可省略的显示文本，0–2000码点、≤8000 UTF-8 bytes |
| created_by_user_id | uuid NOT NULL FK users(id) ON DELETE RESTRICT | 从受保护Session身份产生，只记录创建来源，不授予权限 |
| created_at | timestamptz NOT NULL DEFAULT statement_timestamp() | 创建时间 |
| updated_at | timestamptz NOT NULL DEFAULT statement_timestamp() | 初始等于created_at，每次成功PATCH更新 |
| version | bigint NOT NULL DEFAULT 1 CHECK(version > 0) | 每次成功PATCH加1；响应使用十进制字符串防止JS精度丢失 |

`UNIQUE(workspace_id,id)` 同时支持以 workspace_id 开头的列表访问和未来复合FK；不额外建立重复 `(workspace_id,id)` 普通索引。创建者FK另建 `projects_created_by_user_idx` 支持引用检查；PK支持UUID定位，但应用不能因此提供无租户查询入口。

同Workspace允许完全同名、大小写变体和Unicode规范等价名称；名称是显示值，不承担唯一性、幂等或授权。UUID已足够定位，不增加slug及其修改/冲突契约。

选择保留创建者来源，可帮助后续排查最初归属；不引用 workspace_members 或 membership_id。创建者退出/被移除/禁用/Session撤销后，项目与创建者字段均保留，当前Owner继续管理、当前Member继续读取；创建者本人失去成员关系后同样失去访问权。API不返回创建者，避免无业务需求地扩展用户身份披露。RESTRICT意味着即使未来创建者已无membership，硬删账户仍需另行设计保留策略；本轮没有账户删除接口，也不宣称这是完整审计日志。若省略创建者可简化账户删除，但会不可逆地丢失来源，因此推荐保留。

Workspace FK选RESTRICT而非CASCADE：未来删除Workspace必须先设计关联资源处置，不能通过删父行意外销毁项目；旧Workspace仍可在没有Project时按既有成员/邀请CASCADE约束运行。本轮没有Workspace删除API。

DB CHECK覆盖name长度、ASCII边缘空白和ASCII控制；description长度、禁止除HT/LF外ASCII控制；PG text本身拒绝NUL。Go验证额外拒绝所有Unicode Cc及name剩余Unicode边缘空白，与现有Workspace“应用较强、DB ASCII子集”边界一致，不承诺数据库以任意SQL完全执行Go Unicode分类。

追加轻量 `BEFORE UPDATE` trigger，仅用 `IS DISTINCT FROM` 拒绝 id、workspace_id、created_by_user_id、created_at 变化；无查询其他行或取业务锁，无生命周期状态机。它使直接SQL转移到另一个合法Workspace也失败，即使尚无子资源；仅API字段白名单无法提供这个DB不变量。version递增与updated_at更新由受支持写SQL完成，不声称特权SQL不能绕过授权或修改可变属性。trigger函数和trigger均属于v5，Down一并删除。

### D2. 权限与生命周期

| 操作 | Owner（当前Workspace） | Member（当前Workspace） | 有效身份非成员 | 无效身份 |
| --- | --- | --- | --- | --- |
| POST projects | 201 | 403 project_forbidden | 404 project_not_found | 401 |
| GET list/detail | 200 | 200 | 404 project_not_found | 401 |
| PATCH 已存在匹配项目 | 200，须当前If-Match | 403 project_forbidden | 404 project_not_found | 401 |
| DELETE /转移/归档 | 无接口 | 无接口 | 无接口 | 无接口 |

表中业务状态以结构合法、写请求CSRF通过为前提。item的不存在或跨Workspace组合均为404，优先于角色和条件检查，Member不能借PATCH检查其不可读资源。所有项目继承整个Workspace当前权限，不设项目Owner、成员表或角色。

允许Member创建可降低协作门槛，但在目前只有两种角色、没有项目额度/资源管理体验时会把租户资源写权限扩大到全部成员。推荐本轮Owner创建/修改，Member只读；未来角色调整独立评审，不用created_by给Member隐式“自己的项目可写”。

| 方案 | 收益 | 当前代价 | 推荐 |
| --- | --- | --- | --- |
| 永久删除 | 简单、释放存储 | 未来监控/事件引用、误删与恢复契约未定义 | 暂不提供 |
| 软删除 | 可隐藏及恢复 | 每个查询/FK/唯一性/恢复/保留策略复杂化 | 不引入 |
| 归档 | 有业务语义，可保留历史 | 还需定义是否允许Monitor运行、读写和恢复权限 | 等监控生命周期需求明确 |
| 暂无删除接口 | 最小可靠归属、可逆地推迟生命周期 | 误建项目只能改名/描述，会积累 | 本轮推荐 |

因此没有status、archived_at、deleted_at或后台清理。DELETE对规范collection/item返回405和精确Allow；不把删除决策藏进实现任务。项目没有总量配额：分页控制单次读取，不能当作防滥用或存储增长上界；是否需要业务配额另行评审。

### D3. API、输入和响应

| 方法 | 路径（统一前缀 /api/v1） | 请求 | 成功 |
| --- | --- | --- | --- |
| POST | /workspaces/{workspace_id}/projects | `{ "name":"…", "description":"…" }`，description可省略 | 201、Location为规范item路径、ETag、data |
| GET | /workspaces/{workspace_id}/projects | 仅limit/after | 200、data数组及pagination.next_after |
| GET | /workspaces/{workspace_id}/projects/{project_id} | 无query | 200、ETag、data |
| PATCH | /workspaces/{workspace_id}/projects/{project_id} | name/description至少一项及If-Match | 200、更新后的ETag、data |

本轮仅租户内列表。用户先GET workspaces再列所选Workspace的projects；不增加全账户聚合项目接口。

POST/PATCH均为严格单JSON object，字段大小写敏感，只允许name、description，不接受重复键、unknown字段、null、非字符串、空PATCH或多值/尾随JSON。POST必须name；PATCH缺省字段保留，description:""用于清空。路径而非JSON选择workspace，所有id/role/user/created_by/workspace_id/version/status/timestamp注入均400。复用ValidJSON的UTF-8/surrogate检查，合法补充字符计一个码点，组合字符不做规范化。

Project独立body上限**16384原始bytes**（包括escape与尾随空白），chunked同样计数；允许2000码点UTF-16 escape形式的描述以及名称余量，超过总上限413，即使解码后字段有效。仅单一application/json，允许唯一charset=utf-8（大小写不敏感），拒绝其他媒体参数、多个Content-Type和任何Content-Encoding，415。不得改credentials.MaxBodyBytes。

name复用Workspace语义：只trim边缘ASCII SP/HT/CR/LF，再校验1–100码点且≤400bytes；空值required，随后超长invalid_length，随后任意Cc或剩余Unicode边缘空白invalid_format。description不trim、不规范化，保留空串/空白/HT/LF，禁止其他Unicode Cc（包含CR、DEL、NUL），≤2000码点且≤8000bytes，超长优先invalid_length，其余invalid_format。字段错误422 validation_failed的fields只包含固定reason，不回显值。按name、description固定顺序检查；存在多个字段错误时可同时返回各自reason。DB CHECK只覆盖D1的明确子集。

列表沿用现有limit/after：limit默认50，规范十进制1–100，无符号/前导零；after是单个规范小写带连字符UUID。未知/重复/空参数400；POST/PATCH/detail不允许query。路径UUID不规范映射为无选择器，结构合法后经live身份判断：无效身份401，有效身份404。路径extra segments、trailing slash、点段、重复斜杠、任何RawPath编码别名一律404且不重定向/写入；路由必须在ServeMux清理前拦截项目namespace。

按PostgreSQL UUID **id ASC**，范围 `workspace_id = trusted scope AND id > after`，最多limit+1。next_after仅当额外一行存在时取最后返回id，否则null；空列表data:[]。cursor只是下界，不查询其存在性，不要求属于当前Workspace，外租户UUID作为下界也不披露资源。每页独立快照；UUIDv7不是提交序号，晚提交较小id可能漏于已走过页，更新不改变排序键。无跨页一致快照、total或UUID时间线承诺。

单资源data精确字段为 `id,workspace_id,name,description,version,created_at,updated_at`。version为正十进制字符串（例如"1"）；时间UTC RFC3339Nano固定序列化。列表元素相同，不加角色、创建者、成员、计数或ETag字段。POST/GET detail/PATCH强ETag为 `"project:<canonical-project-uuid>:<canonical-positive-version>"`，固定JSON封装/字段顺序/时间格式且无动态角色字段，保证同一version下资源表示稳定；不提供压缩/媒体变体，未来改变表示格式必须重审validator。列表不设单资源ETag，更新客户端需读取detail或持有原创建/更新回执的tag。

支持方法精确Allow为collection `GET, POST`、item `GET, PATCH`；HEAD/OPTIONS/PUT/DELETE均405，HEAD无body、OPTIONS无CORS。GET忽略body且不消费无界内容，但同样限制传输读/drain时间；不得以GET触发任何写。

### D4. 条件更新，避免丢失更新

PATCH要求恰好一个强If-Match，语法为上述Project tag；拒绝重复header、多tag、weak tag、星号、空tag、非规范UUID/版本或超出bigint范围（400 invalid_request）。header有界（单值≤96bytes），不扫描无界tag；缺失先记录为missing，等身份/作用域/权限检查后428 precondition_required。语法正确但携带另一个Project UUID，或旧version，均412 precondition_failed；不返回当前版本或ETag。GET/POST出现If-Match，或任一项目端点出现If-None-Match/If-Unmodified-Since/If-Modified-Since/If-Range，返回400，本轮不提供条件GET或替代写条件。

作用域匹配后，UPDATE必须同时限制workspace_id、project_id、expected_version，且重新包含live身份和Owner条件；成功恰好一行，version=version+1，updated_at来自最终数据库语句。两个Owner带同一version更新，即使修改不同字段也至多一个成功，另一个412；不自动合并、不在服务端刷新If-Match。合法同值PATCH仍消耗一次version，返回200，防止重放被冒充“已成功”。`updated_at=GREATEST(created_at,old.updated_at,statement_timestamp())` 避免数据库时钟回拨违反非递减CHECK；时间可相等，version才是冲突依据。bigint到最大值不回绕，安全500且不写。

无条件last-write-wins会覆盖旧编辑；只锁行会串行两个写但仍接受旧意图；仅updated_at条件受时钟/精度影响。版本+If-Match代价较小且与已有条件成员删除习惯一致。重放成功但丢回执的PATCH会因旧version得到412，即便后来有人改回原值也不发生ABA；客户端应重新GET比较，不能自动取新tag覆盖。

### D5. 租户验证、RLS与未来引用

读取一个SQL快照同时建立live identity（Session digest→active User、revoked_at NULL、expires_at > statement_timestamp）、Workspace membership与`p.workspace_id = requested_workspace AND p.id = requested_project`。identity左连接保留根：没有live身份401；有身份但scope不可见404；只有scope存在才返回project或空列表。列表不能因零projects/越过末页而混淆401/404。

不设仅接受project_id的公开/内部资源读取或修改Store入口；Store收到digest、wid、pid，不能收到handler宣称的user_id/role。HTTP入口的CSRF和格式检查只约束协议，不能授予数据权限。只在Handler前置查询membership会出现检查后移除、Session撤销或把`UPDATE ... WHERE id=$1`写错的窗口；数据库作用域断言必须存在于真正的SELECT/INSERT/UPDATE内，写的保护与门控见D6。

更换pid、wid或二者（包括用户同时是两个Workspace成员）都不能使错误组合命中；非成员、未知Workspace、未知Project、不匹配组合、UUID格式错误统一404 project_not_found，无无租户存在查询，也不返回外租户ETag/名称。权限不足403只用于已经可读的当前Workspace/Project。错误统一不承诺完全恒时或消除所有延迟侧信道。

继续应用层授权：当前pgx共享连接池/数据库角色没有租户连接上下文；加入RLS需要SET LOCAL、池隔离、migration/owner bypass、任务身份等全套协议与回归，超出本轮。SQL范围、FK、immutable trigger和必测隔离矩阵足以建立当前受支持路径的边界；不宣称任意SQL都具备用户级授权。未来多入口写者或直连数据服务出现时需专门评审RLS。

未来monitors必须有NOT NULL workspace_id、project_id及 `FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)`，不能只对project_id加FK。未来Incident/Alert同理：指向monitor时还需monitor的租户复合键/FK；如同时保存project_id，要用能同时包含workspace/project/monitor的复合引用，不能让两个独立有效外键拼出跨Project关系。本轮只在受保护测试中建临时子表验证跨租户复合FK，不提前创建未来生产表。

### D6. 写事务与实际锁协议

本轮选择**每Workspace写串行**，先实现与现有成员/邀请协议一致的正确性，再根据压力决定未来是否缩小Project写锁。READ COMMITTED事务整体最多2s，所有锁持有到Commit/Rollback：

1. 用digest只路由读取uid（不授权）。Actor `users FOR SHARE`；Session按token_hash和同uid `FOR SHARE`，拒绝active/撤销状态失败。需要多User的未来操作必须先按UUID排序保护全部User，不能在Workspace锁后补User锁。
2. 在含当前actor membership候选条件的Workspace选择语句对w `FOR NO KEY UPDATE`。候选条件不能授权：等待期间membership可能变化；记录workspaceLocked并继续最后门控。不存在/未命中也不能直接掩盖后续身份到期。
3. 对存在的actor membership按user_id `FOR UPDATE`，完整取尽结果；member缺失不插入，不锁创建者的历史membership。只有candidate Owner才锁目标Project，且始终以wid+pid选取 `FOR UPDATE`，避免非成员锁外租户Project。Member item存在性可在最终范围读取中判断，不能借锁查询外部存在性。
4. **全部保护性等待之后另发新SQL命令**，以fresh statement snapshot/time构造live身份、membership、scoped Project与decision。顺序为身份401 → Workspace不可读404 → item不存在/组合不匹配404 → Member写403 → 缺条件428 → tag不匹配412 → version溢出500 →写。POST无item/条件阶段。
5. 创建用带上述门控的 `INSERT ... SELECT ... RETURNING`；更新用带门控及wid/pid/version的UPDATE RETURNING。建立新Project需要的Workspace/creator FK行已经受保护，无新的授权锁排在最终过期检查之后。UPDATE后恰好一行否则安全失败。行和门控状态在同事务内受保护；不得在Handler读完角色后执行无范围DML。
6. 检查取消、调用一次Commit，确认成功才发送data/ETag/Location。没有更后级资源要锁：Project操作不锁Invitation；现有Invitation操作不锁Project。未来同时操作两者必须重新规定后缀顺序，不能形成相反依赖。

User/Session采用SHARE而非KEY SHARE，因为后者不能阻止status/revoked_at的非键更新。Workspace NO KEY UPDATE与现有Member/Invitation写锁互斥，也允许FK的KEY SHARE；同Workspace两个项目写会串行，但不同Workspace不共享全局锁，纯读不被这些行锁阻塞。锁等待完成不意味着原SQL命令的时间或所有join快照已刷新，因此必须用下一条命令做最终门控。这个推导基于[PG18行锁冲突矩阵](https://www.postgresql.org/docs/18/explicit-locking.html#LOCKING-ROWS)和[READ COMMITTED每命令快照](https://www.postgresql.org/docs/18/transaction-iso.html#XACT-READ-COMMITTED)。

撤销/禁用先提交，写操作对应保护检查或最终门控后401；Project写先取得User/Session保护，则对应身份变更等待其事务结束。退出/移除/降级先取得Workspace锁并提交，则后来的Project门控404/403；Project先锁Workspace则可先提交，失权动作随后完成。角色改变仅为遵循协议的测试/维护夹具，不新增产品接口。移除后接受新邀请形成新member关系：旧Project写必须按当前角色决定，不能沿用旧Owner；未持锁的早请求不保证保留原权限。

Session在任何User/Session/Workspace/Membership/Project等待中到期，都由最后新语句的数据库时间拒绝401；门控后到期仍可提交。锁保护状态，不冻结时钟。Session清理先删则401；业务先持Session SHARE则cleanup SKIP LOCKED跳过，随后正常清理。事务不锁历史creator账户（创建时就是actor），创建者后续禁用不应阻碍另一个Owner修改。

读取允许在撤销/移除提交前取得的快照完成；提交后的新读必须失权。不会声称取消已获得合法快照的响应，也不会把in-flight成功当作后续访问仍有效。

### D7. 错误、期限、提交不确定性与日志

JSON失败封装为 `{"error":{"code":"…","message":"固定英文消息","fields":{"name":"reason"}}}`，fields仅用于422。所有项目错误不附加资源ID、etag或用户输入。

| HTTP | code | 固定message |
| --- | --- | --- |
| 400 | invalid_request | Invalid project request. |
| 401 | unauthenticated | Authentication required. |
| 403 | csrf_rejected | Request origin could not be verified. |
| 403 | project_forbidden | Project operation is not permitted. |
| 404 | project_not_found | Project not found. |
| 405 | method_not_allowed | Method not allowed. |
| 412 | precondition_failed | Project precondition failed. |
| 413 | request_too_large | Request body too large. |
| 415 | unsupported_media_type | Unsupported media type. |
| 422 | validation_failed | Invalid project fields. |
| 428 | precondition_required | Project precondition required. |
| 503 | database_timeout | Project operation could not be confirmed. |
| 503 | service_unavailable | Project operation could not be confirmed. |
| 500 | internal_error | Unable to complete project operation. |

乐观并发冲突是412，不另造409；本轮没有名称唯一或生命周期业务冲突。规范路径外的路由404不检查资源，不承诺与业务404同一JSON；绝不重定向写别名。

协议顺序：路径形状→方法405→写CSRF403→Cookie语法401→query/header/media/body/字段400/413/415/422→DB live身份/作用域/权限/条件。缺失If-Match不是协议语法错误，应延后至授权后的428。语法有效但已撤销的Cookie可能先遇输入400，只有合法输入进入DB后才承诺live身份401；不拿解析错误探测DB存在性。

复用选定环境Cookie（production __Host-devpilot_session / development devpilot_session），canonical secret与完整digest，不接受body/header user_id或Authorization替代。POST/PATCH必须单Origin匹配AUTH_ORIGIN、单X-DevPilot-CSRF:1，存在Sec-Fetch-Site必须单same-origin；GET只读，无CORS、无Referer/Host fallback。任何项目响应（含401/故障）都禁止Set-Cookie。

全请求5s、DB read或整个tx2s（包含pool、锁、statement、Commit）且受父deadline限制。复用既有邀请读body的deadline/取消处理模式，限制慢chunk、早拒绝后net/http drain、无ReadDeadline transport的Close解除；不启动不可回收读goroutine。若读deadline取消net/http请求可能隐式发送200，须按既有模式abort连接；不得伪装成功。GET无需消费body，但早拒绝和drain也必须有界。

连接不可用503 service_unavailable，已确认DB deadline503 database_timeout，处理/body预算到期503 service_unavailable，schema缺失/约束异常/死锁/无法分类故障500 internal_error。未知Commit连接丢失503，其他未知故障安全500；超时仍503 database_timeout。写者从不自动重试、补偿或重新Commit。确认rollback才可判定未写；Commit后失回执/HTTP断开可能已写，不能返回成功或声称回滚。客户端已取消则传播取消、停止后续业务/响应，释放或丢弃tx连接；已提交结果无法通过取消撤回。

POST无幂等键：丢响应后可在fresh list中观察项目，但重名/并发/分页使名称不能可靠关联原请求；手动重试可产生第二个Project。禁止自动按name去重、自动重放或承诺exactly-once。PATCH持原tag显式重试最多只能有一次对应版本成功，412后重新读并由用户决定；不是全局幂等结果存储。

所有响应no-store/nosniff，JSON Content-Type；只有确认POST有Location，成功单资源有ETag，错误无二者。HEAD无body。panic值不外泄，写前safe500，写后abort。日志只写固定operation/reason/status，不写原始error、SQL、DSN、headers/body、name/description、workspace/project/user/member/invitation ID、token/digest/PHC/email，也不把If-Match放进日志。

### D8. 结构与最小改动

新增 `internal/project/` 的独立handler/input/store与窄Repository/Database接口；Store自己完成授权，复用既有session digest、auth.CookieName、auth.ValidCSRF、credentials.ValidJSON及user错误分类。项目errors/message map独立，避免把新语义塞入Workspace错误集。只在路由/组装处接入，并给项目namespace增加清理前精确拒绝。保持旧NewRoutes调用方兼容，可明确增加可选project handler，不借机改造所有认证/Workspace路径。OpenAPI在Apply阶段才更新，包含精确DTO、条件tag、错误顺序和Allow；现有auth路由白名单测试须增加四操作，不能删旧约束。

## Risks / Trade-offs

- [Workspace锁扩大竞争] → 全部租户写串行，预算2s；PG测试证明同租户等待、不同租户不受其锁阻塞，未来有压力证据再拆锁，不在本轮重构Member/Invitation协议。
- [无删除/配额，项目可持续增长] → 只由Owner写、列表有界，记录容量与误建体验代价；公开发布的限流/容量准备仍独立评审，不声称已实现。
- [创建者RESTRICT及不可变trigger增加schema责任] → 无用户删除API；Down精确移除trigger/function，存量邀请/成员保持；未来账户硬删和资源转移需独立Change。
- [应用层授权可被未来错误SQL绕过] → 无无scope Store入口，强制多租户真实PG回归与复合FK，新增数据入口评审RLS；不对特权直连SQL作用户隔离承诺。
- [强ETag依赖稳定表示] → 明确单一JSON格式、无压缩变体，version对每个受支持更改递增；未来投影或中间件变化需validator兼容性评审。
- [默认UUIDv7不等于提交排序] → 文档注明keyset分页是新快照、cursor仅下界，不能作为一次扫描覆盖所有并发创建的保证。
- [全库既有strict格式警告] → 单Change严格验证与全库结果分开报告，不为“全绿”改变已审核主Specs。

## Migration Plan

仅设计计划，不在Propose执行以下步骤。

1. Apply获批后新增Goose事务 `00005_create_projects.sql`：建projects及D1全部NOT NULL/default/CHECK/FK/PK/复合UNIQUE/creator索引，再建immutable函数/trigger。updated_at CHECK≥created_at；不使用NO TRANSACTION或CONCURRENTLY，不backfill旧表、不创建项目默认记录、不修改1–4。
2. 测试从空库0→5；有用户、Session（active/revoked）、Workspace、不同role/membership_id及各邀请状态的4→5，比较旧字段/计数/关系/secret hash等保持。up/up后新旧行、版本、触发器均不变。
3. 在临时migration副本中向真实v5 Up末尾（所有DDL/trigger后、Down前）插入失败SQL，从v4运行，断言projects/索引/trigger函数/应用v5记录均未留下，旧v4数据逐项不变；不能只测试另一个合成migration。再对未修改真实v5 Up成功。
4. 只在既有testdb守护可丢弃PG18库做原生单步down 5→4：先删projects（自动删除其trigger），再DROP该trigger函数；保留所有旧表和membership_id/邀请history，不CASCADE跨越历史。仅用于测试的future-child FK表要先清理，不能用CASCADE掩盖依赖。再Up回5；Project数据已被Down删除，不承诺恢复。
5. 修正历史测试：`TestInvitationMigration` 的v4动作固定up-to4，Down确实4→3，复制1–4的真实v4故障测试保留；`TestMigrationAndConstraints`复制真实1–5，把下一失败fixture移至6并更新断言。保留v1/2/3/4的原生单步Down/故障路径，latest helper升级至5。Schema变更套件不能t.Parallel，也不能普通/race同时使用同库。
6. 部署由唯一受控步骤：审查备份/锁与规模→status/up/status到5→部署新二进制→四项目API及前六阶段冒烟。API启动无DDL、readiness只Ping；DB为4时项目请求安全500，不能把ready200视为schema已就绪。
7. 回退API保留v5及数据，停项目写并前向修复；共享/生产不down/down-to/redo/reset，不回到v6前的无条件Member DELETE版本。本Change不授权部署、迁移或PR。

## PostgreSQL 18 verification and CI plan

所有条目均为待执行验收，不能引用前六阶段证据宣称本轮已通过。

| 验收组 | 必须的实际证据 |
| --- | --- |
| 模型/迁移 | 0→5、populated4→5、up/up、真实v5中途失败回滚、单步5→4→5、1–4 SHA256、FK/NOT NULL/CHECK/version/UUIDv7/immutable trigger、临时child跨租户FK拒绝、creator退出/账户引用RESTRICT、Workspace RESTRICT |
| 功能与协议 | 真实注册/登录→Workspace→邀请接受→Owner创建→Member列表/详情→Owner PATCH；精确DTO/tag/Location/Allow/错误消息；Unicode/逃逸/16384bytes/重复键/header/空PATCH/媒体/慢body及真实TCP取消 |
| 隔离矩阵 | A/B两个Workspace、Owner/Member/outsider/多租户同一用户；替换wid、pid、组合、cursor、body身份；pending invitation无Project读取权；非成员/未知/格式错误同404，无意外cookie/ETag泄露 |
| 即时失权 | Member退出/被移除、Owner在保留另一Owner后退出、撤销当前Session、账户禁用后fresh read/write拒绝；新邀请重加仅恢复member读，不恢复旧Owner写；creator退出不删除项目 |
| 双向锁序 | Project create/PATCH与revoke/disable/cleanup/条件移除/自退/角色维护/邀请接受分别先取得保护；用独立连接、屏障和pg_blocking_pids证实等待，避免sleep猜测，不把角色fixture称为产品API |
| 时钟边界 | create等待User/Session/Workspace/Membership、PATCH另加Project，每一级到期都新gate401；gate后到期允许提交；实际数据库时间观察，不靠Go时钟替代 |
| 并发版本 | 两个不同Owner、同版本同/不同字段更新至多一200、另一412；原tag重放412；同值PATCH递增；A→B→A不能接受旧tag；最大bigint拒绝不绕回；分页期间创建/改名及晚提交不夸大遍历保证 |
| 租户独立性 | 一Workspace持写锁时另一Workspace真实Project操作可完成，读可在既定快照完成；邀请/成员写与Project写无锁序环路 |
| 故障 | 真实事务先Rollback再注入错误与先Commit再丢回执两种；每请求Commit≤1，无自动再写；pool/lock/query/commit超时、缺schema、取消、panic、恢复；以sentinel捕获响应和日志脱敏，无success headers/Set-Cookie |
| 完整回归 | health/shutdown/config、注册/password、login/logout/me/cleanup、Workspace、名单/条件移除/最后Owner、邀请全生命周期/配额/退出重加/旧DELETE代际竞态全部保留 |

复用 `internal/testdb`：无TEST_DATABASE_URL、错误库名、与DATABASE_URL冲突、不可达、非18或缺固定Goose都失败不skip、不回退应用连接。不读取开发.env、不操作开发数据卷。Schema操作串行；稳定schema内部并发使用独立连接，并断言cleanup不泄露连接/锁。

Apply更新现有CI push白名单加入 `codex/add-project-management`，保留main/此前分支及PR→main触发；保留Go1.24.3、Goosev3.26.0、现有PG18 digest，verify与integration作业结构及permissions:contents:read。执行 `gofmt`、`git diff --check`、`go mod verify`、`go vet ./...`、`go test ./... -count=1`、`go test -race ./... -count=1`，继而 `go test -tags=integration ./... -count=1 -timeout=120s` 和 `go test -race -tags=integration ./tests/integration -count=1 -timeout=120s` 串行。若完整并发用例实测超出现有120s，再基于证据统一调整文档/CI，不先删除或缩小旧测试以过关。保留PG版本、命令、退出码、Requirement→测试映射；本地与托管CI结果分开报告。

## Product recommendations awaiting review

用户明确要求提出决策供评审，故本设计选择一套完整推荐供独立审核，并未将歧义留给实现者：①Owner创建/修改、Member只读；②允许重名、无slug；③无删除/归档/status；④固定workspace，不转移；⑤保留仅内部可见created_by、用户及Workspace删除RESTRICT；⑥name沿用Workspace长度，description2000码点/8000bytes、空串清空，项目body16KiB；⑦强制条件PATCH、version字符串、每次成功含同值写都递增；⑧只提供Workspace内列表，无项目配额或创建幂等键。

这些选择都已反映在delta/tasks。评审若改变其中任一项，需先同步proposal/design/delta/tasks，再获新的Apply授权；本次文档完成后等待独立技术评审。

## Propose validation record

2026-10-16实际仅执行规划/只读检查：`openspec status` 为4/4 artifacts complete；`openspec validate add-project-management --strict` 退出0。三份delta共17个Requirement、79个Scenario（包含modified块保留的8个既有场景）；每个Requirement均有Success/Exception/Security/Concurrency分类，34个实施任务全部未勾选。新增Requirement描述均≤500字符；modified块保留原场景，仅扩展已评审schema边界及项目场景。

`openspec validate --specs --json` 为10通过/0失败；`openspec validate --all --strict --json` 为7通过/4失败（含本Change，共11项），失败仅为Context列出的四份既有主规格长度warning。`git diff --check`通过，Git仅有本Change新目录，无tracked diff；未执行Go/PG/迁移/托管CI，也未更改主Specs或创建PR。这些是规划格式与范围检查，不代表实现或安全测试已通过。
