# Design

## Context

动机及基线见proposal。2026-10-19只读检查发现：Go1.24.3、pgx/v5、net/http；Migration5已提供projects UNIQUE(workspace_id,id)及身份不可变trigger；Project Store的实际写协议为User SHARE→Session SHARE→Workspace NO KEY UPDATE→actor Membership UPDATE→Project UPDATE，再用新statement的CTE同时门控身份、Owner、范围、版本及DML。Project read用同一语句快照，不依赖Handler预查。Session revoke为单行UPDATE，cleanup为FOR UPDATE SKIP LOCKED；member/invitation写在Workspace锁之后按user_id保护成员。无公开角色变更/禁用接口，本Change测试沿用受协调SQL fixture，不新增这些API。

相关实现：`internal/project/{input,handler,store}.go`、`internal/workspace/{member_delete,invitations}.go`、`internal/session/store.go`、`internal/app/routes.go`、`tests/integration/projects*.go`。现有router在ServeMux清理路径前截获所有Project namespace，必须在该截获之前分派Monitor namespace，不能仅向ServeMux加路由而让Project Handler吞掉Monitor路径。旧NewRoutes调用无Monitor Handler时仍需兼容。

CI已有Go格式/vet/unit/race和固定PG18镜像、Goosev3.26.0真实集成；`internal/testdb`要求独立库`devpilot_registration_test`且server_version_num在[180000,190000)，禁止回退DATABASE_URL。`registration_test.go`临时失败迁移目前为6，`projects_test.go`历史Migration5部分仍用无上界up，Apply必须固定历史边界并把当前“下一失败版本”移至7。Migration1–5不修改。

本设计的产品推荐构成完整可评审方案，尚未获得产品确认或独立技术评审批准；spec SHALL描述该候选方案被批准后的验收契约，不授权当前执行Apply。

## Goals / Non-Goals

**Goals:** 配置数据可被当前成员安全管理；固定双父归属和未来三元组引用；严格、无网络副作用的URL策略；当前版本更新及未知提交的可解释语义；保护现有API输入、Cookie/CSRF及成员代际协议。

**Non-Goals:** 不探测、不DNS、不调度、不新Worker、不结果/历史/latency/uptime/last_checked_at/incident_status、不通知/AI/前端/Redis/计费配额；不实现DELETE/归档/恢复/状态、Monitor或Project独立成员/RBAC；不执行迁移或修改业务代码。本轮配置校验不等于执行期SSRF安全。

## Decisions

### 1. 最小配置、默认值与生命周期

| 字段 | 类型/输入 | 默认值与约束 | API投影 |
| --- | --- | --- | --- |
| id | UUID，数据库生成 | uuidv7()，不可变 | 所有成功资源 |
| workspace_id/project_id | UUID，固定URL路径归属 | 复合FK，创建后不可变 | 所有成功资源 |
| name | POST必填string；PATCH可选 | ASCII SP/HT/CR/LF trim，1–100码点/≤400字节，无Cc/剩余Unicode边缘空白；保留大小写/组合字符 | 所有成功资源 |
| url | POST必填string；PATCH可选 | 1–2048字节、按§3规范化，输入与结果分别限长 | 仅POST/detail/PATCH成功 |
| method | 可选string | GET，只接受精确大写GET | 所有成功资源 |
| interval_seconds | 可选JSON整数，DB integer | 60；允许60/300/900/1800/3600 | 所有成功资源 |
| timeout_seconds | 可选JSON整数，DB integer | 10；1–30且小于interval | 所有成功资源 |
| expected_status | 可选JSON整数，DB integer | 200；单个200–599，包括未登记但格式有效的状态码 | 所有成功资源 |
| created_by_user_id | 可信身份派生UUID | User FK，不可变，非授权依据 | 不公开 |
| version | DB bigint | 1、正数，每次PATCH+1，包括同值写 | 正十进制string |
| created_at/updated_at | timestamptz | statement_timestamp()，初始相等；updated_at非递减 | UTC RFC3339Nano |

默认只适用于POST；PATCH省略保留现值，不允许null清空。整数token为0或可选负号后非零十进制数字，不接受-0、指数、小数、字符串或超int64；语法/类型400，合法整数的业务范围错误422。全部字段NOT NULL。没有description以免复制Project无关字段，没有以name/url去重的唯一键。

固定GET降低MVP行为面；显式保存method便于未来另行审核扩展且不暗示支持HEAD/POST。interval用离散集合比任意秒数容易解释和约束未来负载，默认60兼顾常见检查间隔；timeout默认10、上限30为未来单次操作预算意图，不等于本轮API5秒预算。expected_status用单码避免范围/表达式语法，未来探测不跟跳转时可直接检查3xx。与只允许200–299相比，这允许监控预期401/503等端点，但不声明其健康。

不增加enabled/paused/desired_state：没有执行引擎时这些状态容易误导。所有记录均为“已保存配置”，不会自动生效；未来引擎上线必须另行确定用户激活意图与存量迁移，不得默认扫描并启动全部配置。相同name/url允许重复，支持不同频率/用途，也意味着POST响应丢失无法按URL可靠恢复请求身份；不做幂等键或配额系统。删除/归档需后续独立审核生命周期和未来引用处置。

### 2. API、权限、投影与错误

令base为`/api/v1/workspaces/{workspace_id}/projects/{project_id}/monitors`。

| 操作 | Owner | Member | 身份有效但无成员/范围不匹配 | 成功 |
| --- | --- | --- | --- | --- |
| POST base | 允许 | 真实Project下403 | 404 | 201，Location和ETag |
| GET base | 允许 | 允许 | 404 | 200摘要分页，无ETag |
| GET base/{monitor_id} | 允许 | 允许 | 404 | 200完整配置和ETag |
| PATCH base/{monitor_id} | 允许，须If-Match | 真实Monitor下403 | 404 | 200完整配置和新ETag |

身份无效/禁用/撤销/过期为401；authenticated但unknown/malformed wid/pid/mid和混合归属统一404 monitor_not_found。列表必须验证Project存在且在当前Workspace，合法空Project返回200/[]，不能将scope不存在变成空列表。Member对不存在/不匹配Monitor写仍为404，只有完整资源可读才403。

示例最小POST：`{"name":"官网","url":"https://example.com/health"}`。完整data字段严格为id,workspace_id,project_id,name,url,method,interval_seconds,timeout_seconds,expected_status,version,created_at,updated_at；列表元素去掉url，其余相同。列表`{"data":[],"pagination":{"next_after":null}}`，无total。单资源`{"data":{...}}`。不返回creator、成员信息、角色、状态、秘密或额外父资源。授权Member可读取单项URL；这不是秘密保险箱，path也可能含token，因此不在列表重复暴露。

POST Location完整嵌套路径；强ETag为`"monitor:<uuid>:<positive-version>"`。PATCH唯一If-Match≤96字节，UUID小写规范、version为规范十进制[1,9223372036854775807]；弱、多tag、wildcard、重复header、空值或project前缀均400。合法但另一Monitor tag在授权真实资源后412；无条件428，版本旧412。其他条件header一律400，无304语义。Monitor更改不修改Project版本或ETag；父重命名不使Monitor tag过期，不要求Project If-Match。

列表仅single limit/after，默认50、1–100、规范十进制和小写UUID，id ASC，最多limit+1候选。游标只是范围下界，不查询其身份；每页新快照，无完整遍历并发提交保证。非列表不接受非空query，不为GET引入body配置语义。

处理顺序与Project保持一致：路径/方法→写CSRF→Cookie格式→selector/条件header语法→写媒体/body/字段检查→Store当前身份→完整租户scope→Owner→缺/旧If-Match→合法合并配置/版本上界→DML/Commit。纯输入422可能先于DB授权；“授权优先于条件”专指业务门控428/412，不替无效结构赋予存在性语义。结构正确的已过期Session仍在实际SQL401。

body沿用16384原始字节上限；单JSON对象，拒绝未知、重复（含转义重名）、null、无效UTF-8/surrogate、额外JSON、空PATCH、POST缺name/url。单application/json，仅可有一个UTF-8 charset参数，任何Content-Encoding拒绝415。创建defaults与PATCH合并都需Store复验，不能只有Handler验证。

固定错误为`{"error":{"code":"...","message":"..."}}`，只有422附`fields`键和值，均为固定标识，无反射值。

| HTTP/code | 固定message |
| --- | --- |
| 400 invalid_request | Invalid monitor request. |
| 401 unauthenticated | Authentication required. |
| 403 csrf_rejected | Request origin could not be verified. |
| 403 monitor_forbidden | Monitor operation is not permitted. |
| 404 monitor_not_found | Monitor not found. |
| 405 method_not_allowed | Method not allowed. |
| 413 request_too_large | Request body too large. |
| 415 unsupported_media_type | Unsupported media type. |
| 422 validation_failed | Invalid monitor fields. |
| 428 precondition_required | Monitor precondition required. |
| 412 precondition_failed | Monitor precondition failed. |
| 503 database_timeout | Monitor operation could not be confirmed. |
| 503 service_unavailable | Monitor operation could not be confirmed. |
| 500 internal_error | Unable to complete monitor operation. |

name/url原因优先required→invalid_length→invalid_format；method与整数值不合策略为invalid_value，均在有效范围内但跨字段不满足时timeout给must_be_less_than_interval（当前区间已使这种关系冗余，仍作为明确不变量）。unsupported method exactAllow collection为GET, POST、item为GET, PATCH；HEAD无body、OPTIONS无CORS，额外动作404。

所有响应no-store/nosniff，JSON（HEAD不写body），不Set-Cookie、不扩展Cookie/Session、不引入CORS。POST/PATCH复用精确AUTH_ORIGIN、唯一X-DevPilot-CSRF:1、可选唯一same-origin Sec-Fetch-Site，证明失败在身份/DB前403；无Host/Referer兜底。路径RawPath/清理别名/尾随段/双斜线/点段直接404不重定向，Monitor namespace先于Project分派，以真实server测试覆盖ServeMux清理行为。全请求5秒和read/整transaction≤2秒，包含pool/lock等待，慢body与早拒绝drain亦需有界。

预写panic固定500，已写panic abort，不记录panic值；请求取消停止后续业务/响应，依赖自行报告取消而请求仍live按500，避免空200。连接503、确认deadline503、缺schema/死锁/不明错误500（沿用user.Classify）；未知Commit按可证明的类别503/500，不承诺回滚。成功元数据仅确认Commit后设置。

### 3. URL输入策略（本轮A：无DNS、无目标网络）

这是一组刻意收窄的产品输入规则，不是公网可达性证明，也不建立DNS/IP allowlist。解析器不是策略；Go net/url解析后还须检查原始标记、authority与转义，避免空query/fragment被解析后丢失。规范化需幂等，禁止校验一份字符串却存另一份语义不同的字符串。

1. 原始输入有效ASCII、1–2048字节，无trim，无ASCII空白/控制/DEL和反斜线；原始非ASCII拒绝，国际化路径可按[RFC3986](https://www.rfc-editor.org/rfc/rfc3986.html)百分号编码提交。必须绝对hierarchical `http://`或`https://`（输入scheme不区分大小写），有authority，无opaque、相对引用、network-path或额外scheme解释。
2. 不允许任何userinfo（包括空`@`）；原始`?`或`#`任何位置拒绝，包括`/?`、`/#`空标记。路径内`%3F`、`%23`为路径字节可保留，不能重解析为query/fragment。拒绝所有自定义Host/Header/认证/代理/redirect字段。禁query消除常见token容器，但path和host仍可能敏感，不尝试通过关键词识别全部秘密。
3. Authority仅DNS域名和可选port；禁止`[]`、冒号IP/zone、host百分号编码、user界定符与非法authority字符。先剥离并验证精确port：只许http的`:80`、https的`:443`，或省略；拒绝空port、前导零、符号、其他数字及scheme错配，规范存储去掉显式默认port。未来仍需限制实际目的端口，当前默认端口限制降低未来任意端口扫描面，不消除80/443滥用。
4. Host转ASCII小写，只允许去掉**一个**结尾`.`；去后仍有结尾点则拒绝。去后总长度≤253字节、至少两个label，各1–63字节，仅LDH、不能以`-`起止。ASCII中label第三/四位双连字符仅有效`xn--` A-label可用；末label必须≥2个ASCII字母，或有效punycode A-label。不使用Public Suffix List/域名注册查询，不要求域名真实存在。
5. Unicode U-label不自动映射；用户须给punycode。`xn--`必须经非transitional IDNA严格DNS/label/hyphen/Bidi及有效码点校验、ToUnicode后ToASCII round-trip回同一个lowercase A-label。使用经固定版本审查的`golang.org/x/net/idna` profile（ValidateLabels、StrictDomainName、CheckHyphens、BidiRule、VerifyDNSLength及非transitional处理），不手写punycode；不得接受替代字符或silent mapping修复畸形A-label。Apply选与Go1.24.3及既有x/text兼容的版本并固定go.mod/go.sum，不升Go。相似字形仍可能误导，人类可读展示/IDN增强另行审核，本轮返回ASCII存储形式。
6. DNS规范化后拒绝`localhost`、其子域，以及`.local`、`.internal`、`.home.arpa`、`.invalid`、`.test`、`.example`、`.onion`各本身/子域；按label边界匹配，非简单字串contains。单label先前已被拒绝。这能拒绝`metadata.google.internal`等常见元数据名，但绝不声称列尽所有云元数据别名；攻击者自有域名可指向任意IP。
7. **所有IP字面量均拒绝，包括公网IP。** 严格netip识别IPv4/IPv6/mapped/zone后拒绝，再由上述多label+末label规则拒绝`2130706433`、`127.1`、`0177.0.0.1`、`0x7f000001`、`0x7f.0.0.1`等非规范数值形式，不让解析失败的IP回落为可用DNS名。IPv6、NAT64、6to4、mapped literal和zone亦无法通过authority语法；不以netip.IsGlobalUnicast作为IP安全允许依据。
8. 路径空则`/`；非空必须以`/`开始且每个未转义字符仅RFC3986 pchar或`/`（unreserved、sub-delims、冒号、@），`%`后必有两hex。转义字节禁止C0(00–1F)、DEL(7F)、反斜线(5C)，允许%20、UTF-8编码字节及编码分隔符作为不透明路径。仅将转义hex大写；不decode-unreserved、不二次解码、不去点段、不合并`//`、不改path大小写，避免改变服务器路径语义。保留escaped path语义而不是把Go的解码Path直接串回。最终序列化重新解析同origin且再次校验/限长，重复规范化结果相同。

| 输入示例（仅文本，绝不请求） | 结果 |
| --- | --- |
| HTTP://EXAMPLE.COM.:80 | http://example.com/ |
| https://example.com/a%2fb | https://example.com/a%2Fb |
| https://xn--bcher-kva.de/ | 严格A-label通过后保留ASCII |
| https://bücher.de/ | 拒绝原始Unicode |
| https://example.com/a/../b | 保留点段，不清理 |
| https://example.com/?、https://example.com/# | 拒绝，即使标记后为空 |
| https://user:secret@example.com/、https://@example.com/ | 拒绝userinfo，不回显secret |
| http://169.254.169.254/、https://8.8.8.8/ | 拒绝所有IP |
| http://[::ffff:127.0.0.1]/、http://[fe80::1%25eth0]/ | 拒绝mapped/zone |
| http://[64:ff9b::a9fe:a9fe]/ | 拒绝NAT64字面量 |
| http://example.com:8080/、https://example.com:80/ | 拒绝非对应默认端口 |
| https://example.com/%0d%0a、https://example.com/%5c | 拒绝编码控制/反斜线 |
| https://127.0.0.1.example.com/ | 可符合DNS语法；不说明其解析结果安全 |

完整URL只在DB和授权单项成功投影中存在；列表、errors、logs、tracing labels/metrics不得含其全部或任何片段，包括host/path。不得记录raw body/header/SQL/driver errors/DSN；只记录固定operation/reason/status等有限枚举，不加actor/资源ID/name/秘密。DB自身受控备份/运维权限仍应按配置数据管理；本Change不发明secret加密存储或声明URL永不含秘密。

### 4. SSRF威胁模型与执行期契约（未来B，不实现）

攻击者可以是合法Owner，保存公共格式域名却令DNS返回内部地址；也可利用rebinding、CNAME、混合A/AAAA、连接时重新解析、redirect、环境代理、IPv4-in-IPv6或出口路由访问元数据。读权限用户亦可能经配置看到敏感path。安全资产包括本机/容器/集群/内网服务、云凭据和同/异租户数据；仅检查字符串不会约束网络路由或未来DNS。

[OWASP SSRF指导](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html)区分输入与网络控制；本设计选择配置期不做DNS，避免产生用户驱动的网络副作用。下面是**下一探测引擎Change的必备验收条件**，本轮不创建实现占位函数或运行网络实验：

- 每次解析和每次新连接前验证目的host/port，受控resolver取完整A/AAAA并限制解析时间/答案数/CNAME深度；任一不可接受或不可分类答案则整体拒绝，不只挑一条公网结果。DNS失效/空结果失败关闭，配置期通过不获豁免。
- 按经过审核的[IANA IPv4](https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry.xhtml)和[IPv6特殊用途](https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry.xhtml)快照、运行环境路由和云元数据清单实施公网目的策略，拒绝loopback/private/link-local/multicast/unspecified、保留/文档/benchmark/CGNAT/broadcast、元数据及环境内部网段。IPv4-mapped先Unmap再分类；NAT64知名/本地及部署自定义前缀、6to4/Teredo等转换地址默认拒绝，未来若支持须识别嵌入目标和实际转换路由，不能只查显式RFC1918。`IsGlobalUnicast`包含私有地址，`IsPrivate`也不覆盖全部特殊用途，不能单独作为判据。[Go netip文档](https://pkg.go.dev/net/netip)说明这些分类函数的范围。
- 防TOCTOU：将通过验证的IP绑定到dial目的地址，禁止默认Transport在校验后再次按host解析。所有重拨、fallback和池中新连接都经过同样检查，实际远端验证为已审核目标；保留逻辑Host为规范域名，并使TLS ServerName/SNI及证书验证仍校验原域名，绝不InsecureSkipVerify或为IP绕过证书名。复用连接的目标/版本策略需在该Change明确测试。
- 默认禁止跟随任何redirect（含同源），因此本轮expected_status可配置3xx。若未来审核允许跳转，每一跳重新验证scheme、host、port、DNS/IP及连接绑定，限制跳数且不跨origin传凭据。
- 不使用ProxyFromEnvironment，不受HTTP_PROXY/HTTPS_PROXY/ALL_PROXY/NO_PROXY或用户代理影响；不接收自定义Host/header、Cookie、Authorization。服务固定安全请求头/无凭据，限制响应header/body字节、压缩、总耗时和连接并发；公网80/443本身也可能被滥用，需要引擎阶段的操作预算及滥用控制。
- 在探测进程网络出口阻断内部/metadata/特殊用途目的，限制DNS和默认端口，禁止内部路由/不受控转换/代理绕过应用策略。应用DNS/IP检查与出口防护共同验收，不能以单元测试替代实际部署边界。
- 每次任务携带完整wid/pid/mid及config_version、同快照配置；更新后旧在途任务的取消/结果标记、激活及调度一致性由后续Change定义，结果至少标记实际使用版本，不把旧配置结果当新配置结果。

当前验收只证明A及“没有目标网络”；B必须用受控DNS/连接/TLS/代理/重定向/出口故障测试，在该阶段独立完成。文档研究访问官方资料与业务目标探测不同，本轮不请求上述示例或任何用户配置地址。

### 5. Migration6结构与数据库约束边界

规划新增`00006_create_http_monitors.sql`（当前不创建此文件）。字段见§1；普通Goose事务内创建table、必要索引、`http_monitors_immutable_identity`函数和BEFORE UPDATE trigger。无扩展、RLS或其他生产业务表。

- PK(id)，DEFAULT uuidv7()；双父复合`FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id) ON DELETE RESTRICT`，creator FK users(id) RESTRICT，均默认非延迟约束。复合FK已证明Workspace存在，无需额外冗余Workspace FK。
- UNIQUE(workspace_id,project_id,id)：同时满足未来子表三元组引用、当前列表范围、Project引用检查访问路径；不再建重复(wid,pid,id)或(wid,pid)列表索引。creator-reference B-tree用于User引用检查。总计PK、三元组unique、creator索引；无url/name唯一、无频率/状态/created_at推测性索引。
- 全部NOT NULL；name DB CHECK为1–100码点/≤400字节、ASCII trim不变、无可存ASCII控制(01–1F/7F)，Go附加Unicode分类。url DB CHECK为1–2048字节、仅可打印ASCII且不含空白、以`http://`/`https://`开头的基本形状；不把SQL regex当完整RFC/IDNA解析器。完整应用策略在sole supported writer强制，普通SQL仍能插入DB子集允许但应用禁止的URL，不能宣称DB完整防SSRF。
- method='GET'；interval IN(60,300,900,1800,3600)，timeout BETWEEN1 AND30 AND timeout<interval，expected_status BETWEEN200 AND599；version>0；updated_at>=created_at。
- immutable trigger仅比较OLD/NEW id,workspace_id,project_id,created_by_user_id,created_at，实际改变raise23514；不查询别表或反向加锁。支持业务更新仅通过Monitor Store，不能加入id-only属性写者。普通特权SQL能改配置/version，DB约束不替代Cookie授权或保证任意SQL自动递增版本。
- 不引用Membership：创建者失权不销毁配置；未来引用用完整三元组FK而非独立monitor_id。临时child测试引用可证明未来可用，不在本轮建results/Incident/Alert。未来child记录版本的语义与FK到当前不断变化version不同，不预先建版本历史表。

### 6. 事务边界、锁兼容和并发语义

Read：一条SQL（可CTE/LATERAL）返回live identity sentinel、current membership/Project scope和有界Monitor数据；缺identity401，缺scope404，空列表仍有授权sentinel。detail按wid,pid,mid三元组，不先取mid再相信其父ID。查询使用digest参数绑定，不接受调用者UserID或缓存角色。

Write：一个READ COMMITTED事务、整事务≤2秒（继承≤5秒请求），全部值参数化。初始无锁Session→user_id读取仅用于定位User锁，不能授权。随后按下表取得保护，不锁不存在的Monitor；任何没有成功取得的父保护不能在最终门控中当作有效。

| 阶段 | 锁 | 作用/兼容性 |
| --- | --- | --- |
| User | FOR SHARE | 阻止status/hash/删除等更新；KEY SHARE不能阻止非key状态变更 |
| Session | FOR SHARE且绑定digest+user | 阻止撤销/有效期修改/清理删除；cleanup先删则401，否则SKIP LOCKED |
| Workspace | FOR NO KEY UPDATE | 复用现有同Workspace写串行门；与FK KEY SHARE兼容，不能改为先SHARE再升级 |
| actor Membership | FOR UPDATE | 在Workspace保护后绑定当前membership/role；多个成员按user_id排序；新模块仅需要actor |
| 匹配Project | FOR UPDATE | POST和PATCH均用相同父保护；沿用既有Project writer模式，避免弱锁后升级 |
| 匹配Monitor | PATCH FOR UPDATE | 始终完整wid/pid/mid限定，创建无现存行，无反向父锁 |

只有当前Owner候选且scope存在时才继续资源锁，但最终必须重新SQL门控，不能以候选角色判定替代DML授权。所有保护性等待完成后，**单独新的数据库语句**重新验证active User、digest绑定User、unrevoked Session、expires_at>statement_timestamp()、当前Owner、Project双父和Monitor三元组；INSERT从此受保护查询派生creator/父IDs，UPDATE还带当前版本谓词并验证实际结果。缺identity优先401；scope404→role403→missing tag428→stale412→配置合并校验/overflow→DML。最终语句不再有未保护的业务父行锁等待，唯一键/FK正常不会新增跨事务反向等待；若维护/异常DDL阻塞仍由预算和故障安全处理。

更新时间`GREATEST(created_at,updated_at,statement_timestamp())`，同值也version+1，bigint最大值预先500且无修改。不用timestamp当版本。受支持Project PATCH在相同Workspace锁之前遵守相同User/Session顺序，之后锁Project但不触Monitor；Monitor路径不更新Project属性，因此父名称并发只串行，不消耗Monitor版本。两Owner同tag因Workspace/Monitor串行，第二条新语句看最新version后412。

锁分析依据[PG18行锁冲突表](https://www.postgresql.org/docs/18/explicit-locking.html)：NO KEY UPDATE与KEY SHARE相容，UPDATE与全部行锁冲突；SHARE保护身份的非key属性。Monitor INSERT的复合Project FK和creator FK会要求父键保护，但本事务此前已有Project UPDATE及creator SHARE，自有锁不互相阻塞。其他遵循协议的同Workspace事务尚未越过Workspace门，不会形成一个持子锁等父、一个持父等子的环。独立Workspace有共享User/Session读锁，非全局串行；共享User更新可能合法争用。

特殊路径审查：revoke只更新Session不再请求User/Workspace；cleanup跳锁不在随后请求父；member remove/exit及invitation路径在Workspace前保护相关User/Session，Workspace后才锁成员，目标User若多行按稳定ID获取。Monitor仅锁自己的User SHARE不再请求其他User，可等待但不构造反向边。禁用/角色SQL测试必须遵循现有User在前、Workspace→Membership在后的协调协议；任意外部SQL先锁Project后锁Workspace可造成死锁，本轮不承诺约束所有特权维护写者。

不能机械宣称“有顺序就无死锁”：PK/FK/index、SQL计划和未来子写者仍需真实PG18观察；测试用pg_blocking_pids/pg_locks或已存在事务屏障确认等待、双顺序断言，无sleep-only证明。若异常deadlock/serialization/schema错误，safe500，无自动业务重试；调整锁协议需回评审，不能为性能跳过Workspace保护。

时间语义：每级保护等待后最终新statement_timestamp门控到期，不能用事务起点now()或等待前statement的旧时间；final gate前到期拒绝，gate后自然到期可Commit。锁协调身份/角色状态，不冻结时间。

Commit单次，无自动retry/补偿，只有确认后返回成功。真实回滚后fault与真实Commit后丢确认分别验证旧/新状态。客户端取消不再写或响应，不保证未提交；live请求收到依赖unexplained cancellation是500。POST重复可能两个同URL记录，GET/list只提供观察不能唯一关联未知请求；PATCH重试保留旧tag，若已提交则412，不替用户刷新条件。未来调度读取整行快照+version，不在本轮占用锁/后台读取或承诺在途任务取消。

### 7. 测试与CI验收矩阵（全部待Apply执行）

| 层 | 必需覆盖与证据 |
| --- | --- |
| Go unit/input | strict JSON/raw字节上限/重复escaped key/null/Unicode/整数语法、默认/合并、name边界、强tag/selector/error exact DTO |
| URL纯函数 | scheme/userinfo/query/fragment/host/ports/path规则、所有IP变体/zone/NAT64、LDH/IDNA/尾随点/特殊名、长度界限、规范幂等和保留escaped path；可用fuzz验证不panic/不跨origin，禁止网络resolver |
| HTTP transport | Cookie/CSRF与全部路由方法、RawPath/ServeMux清理、slow/chunked和早拒绝drain预算、headers、panic/cancellation、URL/secret sentinel脱敏 |
| No target network | 代码依赖审核：Monitor仅依赖DB、纯解析/IDNA；不注入resolver/dialer/http client；单元与HTTP/PG端到端在监测目标DNS/dial/transport调用的测试边界和限制出站的测试环境中断言零调用，DB/本地入站测试连接明确隔离。测试域名只作文本，无外部HTTP验证 |
| PG18授权 | 真实注册/登录、邀请accept加入Member、Owner创建/Member读、Member403、outsider与双Workspace混合wid/pid/mid/cursor404、撤销/禁用/过期401、移除/退出404、邀请不授访问；Store直接调用同样失权 |
| PG18完整性 | UUIDv7/default/NOT NULL/CHECK/不可变等值与变更、同Project/不同Project固定归属、复合FK混合拒绝、RESTRICT、creator离开持久、临时三元组child FK、精确索引/列集合 |
| PG18并发 | 两不同Owner同tag/disjoint fields/equal values、ABA/overflow、Project PATCH和Monitor POST/PATCH双顺序、跨租户独立进展；revoke/disable/cleanup/remove/exit/rolechange均写先/变更先；User/Session/Workspace/Membership/Project/Monitor各锁等待过期、final gate后到期允许 |
| PG18 faults | pool/DB/lock deadline、client cancel、不明dependency cancel、panic/缺schema5、deadlock safe分类；真实rollback后fault和真实commit丢ACK，Commit次数≤1及无重试/补偿 |
| Migration | 0→6、存量5→6精确旧表快照（包括Project、成员generation和邀请历史）、up/up、真实6全部DDL后失败、native6→5、再up、守护down-to0、schema/version/index/function/trigger无残留、1–5 SHA256不变 |
| Historical regression | 固定1/2/3/4/5迁移边界、next failure7包含真实1–6；注册/密码预算/登录/退出/session cleanup/health/runtime/workspace/成员generation/邀请/Project全部回归，不取消旧断言 |

禁止把Go race等同数据库并发正确性；normal与race集成必须串行共享测试库，schema suite不能t.Parallel。显式integration缺URL/PG18/Goosev3.26.0、误指应用库或不可达直接fail，无Skip和应用DB fallback。rolechange/multi-owner/disable等受控fixture应标明无新增公开API；正常Member建立优先真实邀请流程。

CI延续现有：`gofmt -l cmd internal tests`、`go vet ./...`、`go test ./... -count=1`、`go test -race ./... -count=1`、`go test -tags=integration ./... -count=1 -timeout=120s`、随后`go test -race -tags=integration ./tests/integration -count=1 -timeout=120s`。如新锁测试使现有限时不足，凭实际耗时审核上调，不跳过场景。添加codex/add-http-monitor push触发，PR main照旧；保留当前固定PG18镜像/工具链，不顺带更新基础依赖。目标出站限制应允许测试DB连接及本地HTTP测试入口，不能用禁网掩盖企图出站，应有调用探针/可审计限制计数。具体可移植测试隔离机制在Apply开始落实，任何“不会联网”结论须同时有结构审核和运行证据，不能以不可达超时当零调用。

验收记录映射每项spec到实际case，记录依赖/命令/退出状态、锁顺序与DB状态、迁移哈希；只在运行后填证据。真实探测DNS rebinding/TLS/IP绑定等B项留作下一Change必验，不记为本轮通过或跳过测试。

## Risks / Trade-offs

- [域名格式合法仍可解析到内网] → 本轮零目标网络；未来引擎必须完成§4后才可运行，配置无“SSRF安全”标志。
- [只许默认端口/无query/IP限制了内网及带令牌端点] → 接受MVP可用性取舍，需要这些能力时单独评审secret、出口、授权/反滥用，不能静默放宽。
- [无enabled/删除使产品只能保存配置] → 明确文案与API投影，未来激活/生命周期独立Change，禁止自动启用历史数据。
- [URL path可能含secret] → 仅授权单项投影，不进列表/错误/日志；不声称所有token已过滤或加密。
- [同Workspace写串行限制吞吐] → 沿用现有锁协议，MVP一致性优先；需性能重构时单独评审全局授权/成员交互，而非局部弱化锁。
- [配置数量无配额、默认端口也能滥用] → 本轮不实现quota；未来公开探测发布须配套预算、出口限制和反滥用，不声称当前字段上限是完整限流方案。
- [IDNA依赖/URL解析器差异] → 固定兼容版本、严格round-trip和原始语法验证、规范幂等测试，拒绝未知形式；future engine使用同一规范origin。
- [未知Commit与重复创建] → 不做exactly-once承诺，保留旧PATCH条件，显示“无法确认”固定消息，不能据错误做补偿删除。
- [未来外部SQL逆序/隐式锁] → 当前实际语义分析+PG18验证及维护顺序约束，不承诺任意SQL无死锁。

## Migration Plan

本轮不执行以下步骤。批准Apply并完成测试/独立评审后：备份和规模/锁影响评估→单一发布步骤status/up/status到6→部署支持配置管理的新API→验证Owner/Member/ETag及前七阶段冒烟。启动不自动DDL，readiness连接成功不证明schema6已就绪；schema5 Monitor请求safe500。无旧数据回填，无旧协议切换，保留邀请阶段禁止旧无条件DELETE二进制的规则。

应用回退保留schema6与Monitor配置，停止新Monitor写入口后前向修复；共享/生产不down/down-to/redo/reset。仅受testdb守护确认可销毁PG18库的native6→5：先移除测试child依赖，再DROP http_monitors及其函数，保留所有旧表/数据；不加CASCADE掩盖依赖，再up不会恢复已删配置。迁移schema测试串行，真实6失败在临时副本DDL结束后、Goose事务Commit之前注入，生产文件不带故障。

## Review Decisions Requiring Product Confirmation

以下为本文已选定的候选方案，不是留白；用户若调整，需要连同proposal/spec/tasks一致修订并再次评审，不能在Apply自行扩大范围。

| 待确认 | 推荐方案 | 取舍 |
| --- | --- | --- |
| 生命周期 | 只保存，无enabled/paused/desired_state、无删除/归档，未来显式激活 | 避免虚假运行含义，暂不能清理配置 |
| 检查形式 | 仅GET；单个200–599状态码，默认200 | 不支持HEAD、范围、body断言 |
| 间隔/超时 | interval枚举60/300/900/1800/3600默认60；timeout1–30默认10且<interval | 有意收窄未来频率/等待预算 |
| URL功能 | 仅DNS HTTP(S)、对应默认端口；禁query/userinfo/fragment/IP/自定义头/代理；punycode可用、原始Unicode host拒绝 | 不支持内网IP/非标端口或query token端点 |
| 重名/重复URL及恢复 | 均允许，不承诺POST幂等 | 响应丢失可能重复，未来幂等另行设计 |
| URL可见范围 | Owner/Member单项可读URL，列表不含URL | 权限继承一致并减少无必要暴露 |
| 管理权限 | 当前Owner写、Owner/Member读，全部继承Workspace | 无Monitor独立角色或终身creator权限 |

独立技术评审重点：URL原始语法/IDNA与规范化测试、无目标网络证据、实际FK与行锁相容、每级等待后expiry、Store内DML授权、未知Commit与迁移历史边界。评审完成前停留Propose。


## Propose Validation Record

2026-10-19仅执行规划校验：`openspec status --change add-http-monitor`为4/4规划artifacts完成；`openspec validate add-http-monitor --strict --json`通过且无issue；`openspec validate --all --json`为13/13通过。全库strict仍因四份既有主规格的七个>500字符WARNING失败（user-authentication 1、workspace-access 2、workspace-members 2、workspace-persistence 2），不属于本次新增规格错误，未修改主规格以消除历史警告。

Git范围检查只有`openspec/changes/add-http-monitor/`未跟踪规划文件，tracked/staged diff为空；Migration1–5逐字节与HEAD一致。没有运行Go/PG18/race业务验收，没有执行migration/DNS/监控HTTP请求，没有创建PR或进入Apply。本节不是业务verification；所有tasks保持未完成，等待独立评审。


## Apply Authorization

2026-10-19用户确认上表MVP选择、独立设计评审通过并授权Apply与创建指向main的PR。上文Propose边界及记录是历史阶段记录。新增验收：隔离环境的网络探针必须有违规DNS/连接正向对照，检测到即失败，再证明真实配置端点零目标尝试；排除批准的PG和本地HTTP通信，保存脚本/命令/退出结果。不得自动合并、归档、公开部署或激活。
