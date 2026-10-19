# HTTP Monitor Management Delta

## Purpose

定义 Project 下 HTTP Monitor 配置管理的可观察契约，包括继承权限、严格输入、URL 纯配置策略、分页、条件更新与隐私边界。保存配置不代表运行探测，也不以字符串校验替代未来 DNS、连接及出口网络层的 SSRF 防护。

## ADDED Requirements

### Requirement: Monitors are configuration resources with inherited permissions
系统 SHALL 只提供 Project 下 POST/list GET/detail GET/PATCH。当前 Workspace Owner SHALL 创建和修改，Owner/Member SHALL 读取；创建者无额外权限。相同名称/URL SHALL 允许重复。系统 SHALL 不提供 DELETE、归档、启停、独立角色、跨租户列表或探测接口。

#### Scenario: Success - Owner creates and Member reads configuration
- **WHEN** 当前 Owner 创建相同名称和URL的两个配置，当前 Member 随后读取
- **THEN** 两个不同UUID的配置均可读，不产生网络探测、成员记录、运行状态或结果数据

#### Scenario: Security - Creator leaves and readable Member cannot write
- **WHEN** 创建者退出/被移除/禁用，或当前普通Member修改可读配置
- **THEN** 配置保留且其他当前成员继续按角色访问；前创建者失权，Member写入返回403 monitor_forbidden

#### Scenario: Exception - Unsupported operations
- **WHEN** canonical collection/item接收DELETE/PUT/HEAD/OPTIONS，或访问运行/暂停/探测/跨租户聚合路径
- **THEN** canonical方法失败405且Allow分别为GET, POST或GET, PATCH；HEAD无body、OPTIONS不授予CORS，未定义路径404且无副作用

### Requirement: Monitor mutation bodies use strict bounded JSON
POST SHALL 必填字符串name/url，可选字符串method和整数interval_seconds/timeout_seconds/expected_status；PATCH SHALL 接受至少一个上述字段。单JSON对象 SHALL 最多16384原始字节，单一application/json可附UTF-8 charset，无Content-Encoding。null、重复/未知键、错误类型、非法Unicode、额外JSON SHALL 返回400。

#### Scenario: Success - Defaults and partial update
- **WHEN** POST仅提交name/url，或PATCH只提交name
- **THEN** POST采用GET/60/10/200默认值；PATCH保留所有缺省字段并校验合并后的完整配置，不把PATCH缺省恢复为创建默认值

#### Scenario: Exception - Structural and numeric grammar
- **WHEN** 输入缺少POST必填字段、空PATCH、数组/null、重复键（含转义后重名）、未知/不可变字段、无效UTF-8/孤立surrogate、尾随值，或数值为字符串/小数/指数/负零/超出有符号64位整数
- **THEN** 返回400 invalid_request，无持久化；整数token只接受0或可选负号后非零开头的十进制数字，范围错误由字段校验处理

#### Scenario: Exception - Representation and raw byte limits
- **WHEN** 原始body含chunked/转义/尾随空白后超过16384字节，媒体类型重复/不符或含Content-Encoding
- **THEN** 分别返回413 request_too_large或415 unsupported_media_type；慢body及早拒绝drain受5秒预算约束

### Requirement: Monitor configuration fields have deterministic validation
Name SHALL 只trim ASCII SP/HT/CR/LF边缘，要求1–100码点/400字节、无Cc或剩余Unicode边缘空白，不做Unicode归一化。Method SHALL 仅GET；interval SHALL 为60/300/900/1800/3600之一；timeout SHALL 为整数1–30且严格小于interval；expected_status SHALL 为单个整数200–599。字段无效 SHALL 返回422及固定原因。

#### Scenario: Success - Unicode names and exact status
- **WHEN** name含中文/补充平面字符及内部空格，method省略或为GET，预期状态为302或503
- **THEN** 保存规定的名称和单状态码意图，无状态范围、跟随跳转或执行成功含义

#### Scenario: Exception - Stable field reasons
- **WHEN** name trim后空/超长/格式非法，URL为空/超长/不符URL策略，method非GET，或整数不在允许范围
- **THEN** name/url按required、invalid_length、invalid_format顺序选原因，method为invalid_value、整数为invalid_value；如整数范围均有效但timeout不小于interval则timeout为must_be_less_than_interval，消息不含原值

#### Scenario: Concurrency - Cross field validation uses current locked state
- **WHEN** 不同Owner并发PATCH不同字段，或有一个字段被省略
- **THEN** 通过当前版本的完整配置检验后才修改，旧条件412不能被自动合并或刷新

### Requirement: Monitor URLs have a deterministic offline origin policy
URL SHALL 为ASCII绝对http/https层级URL，输入及规范结果最多2048字节，不trim。仅允许符合design规范的多标签DNS/punycode主机及对应默认端口；scheme/host小写、去单个尾随点/显式默认端口、空path变为/。所有IP字面量、zone、用户信息、query、fragment、空白/控制/反斜线及非法编码 SHALL 被拒绝。

#### Scenario: Success - Canonical domain URL without network lookup
- **WHEN** 保存HTTP://EXAMPLE.COM.:80或https://example.com/a%2fb
- **THEN** 分别规范为http://example.com/和https://example.com/a%2Fb，path大小写/分隔/点段语义保留；不查询DNS、不确认域名存在或可达

#### Scenario: Security - Dangerous and alternate IP forms
- **WHEN** URL含127.0.0.1、10.0.0.1、169.254.169.254、100.100.100.200、0.0.0.0、广播/组播、公网IP、IPv6、IPv4-mapped IPv6、NAT64/6to4字面量、zone、十进制整数/八进制/十六进制/缩写IP
- **THEN** 均422拒绝，无目标解析/连接；不把拒绝字面量解释为对域名解析结果的保证

#### Scenario: Security - Authority ambiguity and reserved names
- **WHEN** URL含userinfo（包括空userinfo）、%编码host、Unicode原始host、无效punycode、空/超长/下划线label、多个尾随点、单标签、非字母且非有效punycode末标签、localhost或以.localhost/.local/.internal/.home.arpa/.invalid/.test/.example/.onion结尾
- **THEN** 均422拒绝，IDN只允许经严格校验且round-trip一致的ASCII A-label，拒绝策略不依赖DNS和本机search domain

#### Scenario: Exception - Ports query fragment and path encoding
- **WHEN** http用443或https用80、端口有前导零/为空/非默认，出现任何?或#（包括空query/fragment）、反斜线、裸非ASCII、非法%转义或path中编码C0/DEL/反斜线
- **THEN** 均422；合法RFC3986路径保留转义字节且仅将百分号十六进制改为大写，不解码重解释为authority，不清理点段或合并斜线

### Requirement: Configuration management never performs target networking
Monitor创建、修改、读取和校验 SHALL 不解析目标DNS、不连接目标、不校验证书、不调用HTTP客户端、不发起HEAD/GET探测；数据库连接不属于目标网络。URL策略 SHALL 仅表示输入可接受，不能声称阻断所有SSRF。系统 SHALL 不接收代理、重定向策略或自定义Host/Header/凭据配置。

#### Scenario: Security - Syntactically valid hostile DNS name
- **WHEN** 合法格式的域名可能不存在或解析到私网/元数据IP
- **THEN** 本轮仍只按输入策略保存，既不查询也不标记“安全/可达/健康”，未来执行前必须重新拒绝不安全解析结果

#### Scenario: Success - No activation implied by persistence
- **WHEN** 任一配置被创建或修改
- **THEN** 没有enabled/paused/desired_state、任务、网络出站或结果；未来引擎需独立评审激活契约，不能据本次保存自动激活全部历史配置

### Requirement: Monitor reads bind live identity and complete resource scope
每次读取 SHALL 在同一数据库语句快照校验active User、未撤销且expires_at>statement_timestamp的Session、当前Membership及匹配的Workspace/Project/Monitor归属。身份无效 SHALL 401；非成员、未知/非规范UUID或混合资源ID SHALL 统一404 monitor_not_found，不作不带租户的存在查询。

#### Scenario: Success - Authorized empty collection differs from denied scope
- **WHEN** 当前Owner/Member读取其真实Project的空列表，或请求不存在/外国Project
- **THEN** 前者200且data:[]，后者404；身份无效优先401而非空成功

#### Scenario: Security - Substitutions at every nesting level
- **WHEN** outsider、双Workspace成员或已移除成员替换wid/pid/mid，组合Workspace A+Project B或Project A+Monitor B，或仅持有邀请/旧Cookie
- **THEN** 不可访问/不匹配范围同为404，无URL、名称、ETag、创建者或存在性提示；禁用/撤销/过期身份为401

#### Scenario: Concurrency - Fresh snapshot loses authority
- **WHEN** 移除/退出/撤销/禁用先于新读取快照提交，或在早先已授权快照之后提交
- **THEN** 新读取按401/404失权，早先快照可完成，后续请求不能复用旧权限

### Requirement: Monitor lists expose bounded minimal projections
列表 SHALL 仅接受单个可选limit（规范十进制1–100，默认50）和after（小写连字符UUID），按id ASC最多limit+1候选。响应 SHALL 为data及pagination.next_after，仅有额外候选时取最后返回ID，否则null。列表元素 SHALL 仅含id/workspace_id/project_id/name/method/interval_seconds/timeout_seconds/expected_status/version/created_at/updated_at，不含url。

#### Scenario: Success - Pagination and URL minimization
- **WHEN** 一个Project跨页、为空或cursor超过末尾
- **THEN** 返回稳定ID排序的有界配置摘要，空页[]/null、无total、无URL且无单资源ETag header

#### Scenario: Exception - Query grammar
- **WHEN** limit/after重复或为空、未知key、编码错误、非规范数字/UUID，或POST/PATCH/detail带任何非空query
- **THEN** 返回400 invalid_request，不允许offset/filter/sort；创建JSON中的url query亦按URL策略独立拒绝

#### Scenario: Security - Cursor has no authority
- **WHEN** after来自其他Project/Workspace或不存在
- **THEN** 仅作已授权范围的下界，不查询cursor存在性、不泄露或纳入该资源

#### Scenario: Concurrency - No traversal snapshot promise
- **WHEN** 页面之间创建/修改/成员变化或低于cursor的UUID晚提交
- **THEN** 每页重新授权，无跨页快照或覆盖所有并发提交保证

### Requirement: Single monitor responses carry a versioned configuration
POST/detail/PATCH SHALL 使用data，包含列表全部字段及规范url，不含创建者、状态或结果。Version SHALL 为正十进制字符串，时间为UTC RFC3339Nano。确认POST SHALL 为201及完整规范Location，detail/PATCH为200；三者均附强ETag "monitor:<id>:<version>"。Monitor变更 SHALL 不更新Project version/ETag。

#### Scenario: Success - Exact configuration response
- **WHEN** 创建提交version1或PATCH提交version2
- **THEN** 返回准确字段/数字类型、version为"1"/"2"且ETag匹配，仅POST含Location；读取响应不声称监控已开始

#### Scenario: Security - Authorized URL and secret minimization
- **WHEN** 读取单项配置或列表、失败、日志/诊断
- **THEN** 仅授权单项成功含URL；列表不含URL，错误/日志不含URL任何部分、body、Cookie、原SQL/DSN/创建者，path亦可能含token，不能宣称禁止query消除秘密

### Requirement: Monitor updates require a strong current precondition
PATCH SHALL 要求唯一强If-Match monitor tag，最大96字节、规范UUID及正bigint版本。Malformed/repeated/multiple/weak/wildcard/empty SHALL 400；完成身份/完整scope/Owner门控后缺少条件428，资源或版本不匹配412。同值更新 SHALL 递增version一次，非递减updated_at；溢出500不修改。

#### Scenario: Exception - Precondition grammar and forbidden conditional headers
- **WHEN** PATCH条件非法/重复/超长/越界，GET/POST含If-Match，或任何请求含If-None-Match/If-Modified-Since/If-Unmodified-Since/If-Range
- **THEN** 返回400且无条件读取语义；合法但指向另一Monitor的tag仅在实际资源授权后412

#### Scenario: Security - Business denial precedes version outcome
- **WHEN** 语法有效的请求身份失效、scope不匹配、Member写入且条件缺失/过期/正确
- **THEN** 分别401/404/403先于428/412，不泄露当前tag；合法Owner对真实资源缺条件428、旧条件412

#### Scenario: Concurrency - Two Owners and response loss
- **WHEN** 两个不同Owner以同一tag修改（包括不同字段/同值），或已提交但响应丢失后重放原tag
- **THEN** 最多一个消费该版本，后者412；不自动合并/刷新条件，ABA不能恢复旧版本

### Requirement: Monitor transport preserves existing security and bounded failures
POST/PATCH SHALL 复用Cookie-only认证及精确同源CSRF协议；请求预算5秒，读/整写事务最多2秒。所有响应 SHALL no-store/nosniff且无Set-Cookie。错误 SHALL 为固定安全JSON；依赖失败503、确认超时503、内部/模式错误500；取消 SHALL 停止后续工作和响应，无自动业务重试。

#### Scenario: Security - CSRF and path isolation
- **WHEN** Origin/自定义proof缺失/null/重复/不匹配、Sec-Fetch-Site不是单个same-origin，或路径有RawPath/点段/双斜线/尾随段
- **THEN** proof失败403 csrf_rejected在身份/DB前；路径别名404无重定向/写入，不接受Host/Referer/Forwarded或body/query/Authorization作为替代权限

#### Scenario: Exception - Error envelope and fixed messages
- **WHEN** 可写响应出现业务/依赖失败
- **THEN** 返回{"error":{"code":...,"message":...}}，仅422增加固定fields；完整状态/固定英文消息遵循design错误表，不回显参数，不发出成功data/Location/ETag

#### Scenario: Exception - Cancellation and panic
- **WHEN** 客户端取消、依赖在live请求中无解释地报告取消，或pre/post-write panic
- **THEN** 客户端取消无后续响应，无法解释的依赖取消固定500，写前panic安全500、写后abort，不泄露panic值或静默生成空200

#### Scenario: Concurrency - Slow and rejected bodies
- **WHEN** 合法请求与慢body、拒绝proof/路径/方法的请求并发
- **THEN** 读取和早拒绝drain均有界、请求状态互不串用，无未经授权DB工作或目标网络操作
