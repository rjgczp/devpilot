# Spec Delta

## MODIFIED Requirements

### Requirement: Activation intent is disabled by default and distinct from execution
新建Monitor SHALL 默认disabled/g1、授权版本NULL，activation.next_due_at始终NULL。enabled SHALL 仅表示意图；旧execution_state/blocked_reason SHALL 废弃为固定not_reported/execution_status_required提示，不断言实际执行。execution-status是运行观察权威。Migration8 SHALL 撤销旧enabled并让所有存量g恰好+1以失效旧表示tag；执行仍需Owner限期授权及部署许可。

#### Scenario: Success - Existing and newly created monitors remain dormant
- **WHEN** 新Monitor创建、未获得新执行授权的存量升级，或普通API启动/重启
- **THEN** 每Monitor有且仅有一条完整scope意图；初始化/启动本身不启用、不发目标DNS/TCP/HTTP、不转interval为任务；原有fixture任务是否在途只由execution-status报告

#### Scenario: Success - Enabling records intent without claiming execution
- **WHEN** Owner成功设置enabled
- **THEN** 保存配置版本意图，固定返回not_reported/execution_status_required、next_due_at=NULL且无run/健康字段；不把该提示解释为被阻断/未运行，实际观察读取execution-status

### Requirement: Every configuration revision atomically revokes activation
任何成功配置PATCH SHALL 原子递增配置version、将意图disabled、清空授权版本并递增g，包含名称及同值PATCH。SHALL 同事务撤销独立执行授权、清next_due并推进scheduler_epoch，不改变已生效冷却或物理资源账。URL变化无例外；失败/回滚不改变状态，缺失状态或溢出使配置与全部失效整体回滚。

#### Scenario: Success - Conservative revocation for all fields
- **WHEN** Owner修改URL/name/interval/timeout/expected_status/method，或提交同值PATCH
- **THEN** 新配置版本与configuration_changed失效状态同一Commit可见；新版本需Owner显式重确认独立有限期执行授权；原run不得更新新v/g/epoch的当前观察

#### Scenario: Concurrency - Configuration and activation serialize
- **WHEN** enable/pause与v7到v8的合法PATCH竞争
- **THEN** PATCH先提交则旧v/g意图命令412；enable先提交则PATCH撤销它；PATCH先提交使旧pause412时当前已disabled，无跨表半提交

### Requirement: Intent ownership survives its author while audit remains bounded
已确认意图 SHALL 属Workspace，不因原Owner正常退出或Session撤销而追溯消失，前Owner后续访问按当前成员权限。最末意图审计 SHALL 仅保存当前g、固定原因、DB时间及可信actor，不保存URL/Session/网络详情或无界命令事件；不公开actor。独立执行授权须有期限，Claim/Start SHALL 验证至少一个当前active Owner；Session不是后台持续凭据。

#### Scenario: Success - Another Owner retains control
- **WHEN** 启用者退出但还有当前有效Owner
- **THEN** 意图保留，其他Owner可按新条件暂停；前Owner失去写权，重新加入Member也不能恢复旧请求

#### Scenario: Security - No active Owner never causes execution
- **WHEN** 异常禁用导致Workspace没有有效Owner
- **THEN** 新Claim/Start固定blocked且零新执行，不把owner行计数当作活Owner证明；已Start可按暂停弱语义结束，结果仍归原版本

#### Scenario: Success - Last-state audit has a fixed row bound
- **WHEN** 同一Monitor发生大量成功命令/配置失效
- **THEN** 只有一条状态行，覆盖最后原因/时间/actor，默认及配置失效actor为NULL；不产生事件历史或伪造完整审计承诺

### Requirement: Activation transport and response projections are bounded and private
PUT SHALL 只接受必填字符串desired_state/configuration_version的单JSON对象，最多1024原始字节，复用严格Unicode/重复键/媒体校验。请求 SHALL 复用精确Origin/CSRF、5秒请求和2秒DB预算、固定安全错误及no-store/nosniff；无Set-Cookie。成功 SHALL 只在确认Commit后返回200。

#### Scenario: Exception - Body path and method rejection
- **WHEN** body结构/未知重复键/null/多JSON/Unicode非法，字段枚举或版本范围非法，超限或媒体错误，query/GET body、路径别名或不支持方法
- **THEN** 分别400/422/413/415；路径别名404；其它方法405 Allow GET, PUT且HEAD无body；GET带If-Match或任何其它条件头400，PUT不接受Content-Encoding

#### Scenario: Security - CSRF and fixed failure behavior
- **WHEN** 精确同源proof不符，或DB依赖失败/确认超时/内部错误/客户端取消
- **THEN** proof失败403 csrf_rejected且零DB；依赖或确认超时503、内部/schema500；取消停止工作不继续响应，日志无Cookie/URL任何部分/raw error/SQL

#### Scenario: Success - Exact status representation
- **WHEN** schema8新API的Owner/Member GET或Owner确认PUT成功
- **THEN** data保持完整scope、当前configuration_version、desired_state、授权版本、g、next_due_at、next_enable_allowed_at、last_transition_kind、created_at/updated_at及两个弃用字段；其固定值为not_reported/execution_status_required，成功头DevPilot-Activation-Projection: intent-v2；版本/代次字符串、时间UTC RFC3339Nano，无URL/actor/lease/run/health，原配置DTO和activation强tag格式不变

## ADDED Requirements

### Requirement: Deprecated execution fields have an explicit breaking compatibility protocol
Activation两个旧执行字段 SHALL 保留名称/string类型但只返回迁移提示，实际执行 SHALL 由execution-status权威报告。此const变化 SHALL 标记BREAKING并升级客户端/OpenAPI；所有存量g在Migration8恰好+1，冷却不缩短。运行变化 SHALL 不改变Activation表示/tag或消耗g；不以动态摘要破坏原强ETag。

#### Scenario: Security - No false blocked report while a fixture is running
- **WHEN** 同一Monitor在fixture running、blocked或unknown
- **THEN** 新Activation始终只报告not_reported/execution_status_required，实际状态读取execution-status；客户端不得把迁移提示或旧常量回退为未运行/被阻断

#### Scenario: Exception - Legacy closed enum client or absent authoritative endpoint
- **WHEN** 旧客户端仅接受blocked/scheduler_unavailable，或权威status不支持/404/503
- **THEN** 常量兼容失败必须显式迁移；新客户端显示运行状态不可用，不推测目标失败/健康；第十阶段原契约/归档不改写，schema7历史fixture仍验证旧值

#### Scenario: Concurrency - Runtime changes keep the intent validator stable
- **WHEN** v/g与Owner意图不变而任务等待、running、unknown、完成或授权自然到期
- **THEN** Activation表示及强ETag保持稳定，execution-status独立反映变化；不支持条件GET/304，不把两个不同快照v/g拼接为一致观察

#### Scenario: Security - Old HTTP projection blocks fixture rollout
- **WHEN** 仍有旧Activation HTTP实例接流量，或拟回退旧表示但尚有未终止物理执行
- **THEN** fixture部署gate保持closed；只有全部新HTTP协议就绪才允许fixture执行。旧API仅可作为非接流量DB写者验证无授予能力；回退须停止/撤销/确认终止，排空读写并再次推进全部g失效tag、保留冷却后切旧表示，不能在同强tag下切换协议

### Requirement: Execution authorization requires separate current Owner reconfirmation
PUT `/api/v1/workspaces/{wid}/projects/{pid}/monitors/{mid}/execution-authorization` SHALL 复用Cookie/CSRF及当前Owner最终SQL门控，要求单一强activation If-Match和configuration_version、consent_profile=periodic-http-get-v1、authorization_seconds整数60..86400。SHALL 原子消耗g、遵守原100/W与60s冷却，并生成绑定新v/g/epoch的有限授权；不自动续期。

#### Scenario: Success - Explicit finite authorization is separate from intent
- **WHEN** 当前Owner以最新v/g及严格<=1024字节单JSON对象确认
- **THEN** 确认Commit后200返回新v/g、数据库期限及effective/blocked字段，无URL；没有部署许可仍blocked且零目标网络，普通activation PUT不能代替此确认

#### Scenario: Security - Legacy writers cannot create or preserve execution permission
- **WHEN** 旧API在Migration8后enabled、同值enabled或pause，或与新授权竞争
- **THEN** 每次g变化清独立授权/next_due并推进epoch；旧角色无授予函数/新表写权限，新授权必须独立提交完整Owner门控，不能以默认新字段识别新协议

#### Scenario: Concurrency - Authorization expires without automatic renewal
- **WHEN** 到达数据库expires_at、原Session失效或当前Owner集合变化
- **THEN** 严格过期禁止新Claim/Start且effective=false，desired可以仍enabled；Session失效不追溯撤销Workspace授权，但没有active Owner不能新执行；续期须再次确认和消费g

#### Scenario: Exception - Conditional transport and uncertain commit
- **WHEN** 非法/缺少/旧tag、错误媒体/body/CSRF、Member写、身份/范围失效、期限越界或Commit未知
- **THEN** 沿用activation的400/428/412、413/415/422、403/401/404及优先级、5s/2s预算、固定503/500与隐私，不返回未确认data/ETag，不重放或补偿；授权status对账

### Requirement: Pause prevents new starts but does not promise to recall authorized traffic
Pause及配置撤销 SHALL 与Start在父资源锁下串行，暂停成功后不能再提交旧v/g/epoch的新Start。采用Start Commit线性化模型，已Start任务可能晚于暂停响应发包或完成；SHALL 明确在途/unknown及此承诺，不以数据库CAS宣称物理停止。

#### Scenario: Concurrency - Pause wins before Start
- **WHEN** Pause/PATCH先提交，Claim已成功或Start正在等待W/M锁
- **THEN** Start锁后新语句发现g/epoch/授权失效，run取消、零Probe发送，不凭锁前快照继续

#### Scenario: Concurrency - Start wins before pause response
- **WHEN** Start已提交但未连接、TLS完成未GET、GET已到远端或探测完未存结果
- **THEN** 尽力取消但允许在途旧任务晚发包/完成；旧结果不更新新v/g/epoch当前观察；暂停接口不等待并伪称绝对未发送

#### Scenario: Security - Stronger physical stop needs a separately verified fence
- **WHEN** 产品要求暂停成功后旧执行环境不能再发送数据
- **THEN** 必须独立撤销双栈现有流/permit、清连接并确认旧代次不可恢复后再成功；监督器不可达时不能报告强停止成功，未获该产品选择与部署证据前生产关闭
