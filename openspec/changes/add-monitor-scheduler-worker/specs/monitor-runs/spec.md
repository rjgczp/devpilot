# Spec Delta

## Purpose

定义周期 HTTP Monitor 的持久运行谱系、未知执行与最小结果投影，确保跨租户隔离和配置/激活代次一致，并在崩溃恢复、暂停与有限存储容量下提供可核验的用户状态，不将一次结果解释为完整历史或可用率。

## ADDED Requirements

### Requirement: Every run preserves complete immutable execution lineage
每个run SHALL 绑定完整workspace_id/project_id/monitor_id、唯一run_id、configuration_version、activation_generation、scheduler_epoch及fencing_token。身份/执行版本 SHALL 不可改写；子引用 SHALL 使用三元组，不以裸Monitor ID授权或关联。

#### Scenario: Security - Mixed tenant identities cannot create or read a run
- **WHEN** 两Workspace成员组合真实但不匹配的wid/pid/mid或使用另一个run_id
- **THEN** 数据库复合引用拒绝混合写入，用户读取按实时身份与完整scope拒绝且不泄漏存在性

#### Scenario: Concurrency - Reauthorization cannot relabel an old observation
- **WHEN** v7/g7任务运行时配置改v8或同v7重新授权g8
- **THEN** 旧run只属于原v7/g7/epoch，不能被改写为新授权结果或覆盖其当前投影

### Requirement: Results contain only bounded private observations
完成结果 SHALL 只保存完整谱系、UTC事件时间、单调duration、可空200–599最终HTTP status、success及固定failure_code。SHALL 不保存URL任何部分、DNS答案、Header/Body、原始error、Cookie、证书或凭据；未启动拒绝不能伪造HTTP结果。

#### Scenario: Security - Sensitive sentinels never persist
- **WHEN** URL Path、DNS、TLS/HTTP输入或panic含秘密哨兵
- **THEN** 数据库run、常规日志、结果DTO和错误不含哨兵/原始网络详情，只有限定数字及固定分类

#### Scenario: Exception - Blocked before execution is not target downtime
- **WHEN** 无部署许可、配额耗尽、授权过期或配置预检失败
- **THEN** 返回固定blocked/expired或预检状态，不伪造HTTP0、timeout、目标失败或成功健康

### Requirement: Unknown delivery is terminal for retry and distinct from failure
已经started但无法确认结果的run SHALL 标记unknown，不自动重发该次GET，不退回leased。unknown SHALL 表示无法确定远端交付/结果，允许结果丢失；SHALL 不承诺HTTP端到端Exactly Once。下个正常周期须先证明旧物理执行权限已终止。

#### Scenario: Exception - Process dies after sending GET
- **WHEN** 接收端已收到GET而Worker在Result Commit前崩溃
- **THEN** run成为unknown，无同run重发，未获独立终止证明前其Monitor和共享物理槽继续占用

#### Scenario: Exception - Start acknowledgement is lost
- **WHEN** Start可能已提交但发起方未确认且进程重启
- **THEN** 不凭读回started重建发送信封，保守unknown；不能假定请求未发生

#### Scenario: Concurrency - Late results and later cycles
- **WHEN** unknown后旧Worker恢复上报，或资源已独立收回且下一interval到期
- **THEN** 迟到结果不改写unknown；新周期仅在有效授权/预算/旧权限不可恢复条件下以新run运行，不补发旧周期

### Requirement: Result commits are fenced and can be reconciled without networking
结果写入 SHALL 校验run完整scope、epoch、fence及固定result_commit_id，最多产生一个不可变终态。未知Result Commit SHALL 仅查询或幂等提交同一结果，不再次执行网络；只有匹配当前v/g/epoch的已确认结果可更新当前最近观察。

#### Scenario: Concurrency - Expired fence cannot overwrite a result
- **WHEN** 旧lease/fence或旧worker incarnation提交结果
- **THEN** 数据库拒绝其写入，不改变当前结果、下一due或资源账

#### Scenario: Exception - Successful commit loses acknowledgement
- **WHEN** 真实Result Commit成功但ACK被丢弃
- **THEN** 按同run/result_commit_id对账得到同一结果，重复提交不增加run/GET，不把未知提交直接当作回滚

#### Scenario: Concurrency - Pause or PATCH precedes result persistence
- **WHEN** 暂停或新配置先于旧run Result Commit提交
- **THEN** 有效旧run结果可留最小审计，但不更新新v/g/epoch当前观察；API不显示旧URL已健康

### Requirement: Execution status is a separate live authorized projection
GET `/api/v1/workspaces/{wid}/projects/{pid}/monitors/{mid}/execution-status` SHALL 是实际运行观察的唯一权威，以同语句Cookie实时身份/成员/完整scope允许Owner/Member读，区分授权、调度与attempt。Activation旧执行字段 SHALL 仅为not_reported/execution_status_required迁移提示；原配置DTO/ETag及Activation字段形状不加入run信息。

#### Scenario: Success - Minimal status distinguishes intent and actual execution
- **WHEN** desired_state=enabled但没有授权、部署出口关闭、已到期、执行中或结果未知
- **THEN** 有界响应分别显示effective_authorization、expires_at、调度投影、next_due、in_flight、固定blocked_reason、pause_semantics=start_commit及最多一个最近attempt/一个最近completed观察，不把enabled等同运行

#### Scenario: Security - Current identity and privacy remain mandatory
- **WHEN** Session失效、成员已移除、scope替换，或Member读状态
- **THEN** 分别401/404/合法200；不含URL/actor/lease_owner/出口标识/IP或预算hash，无目标网络；保留no-store/nosniff及5s/2s预算、固定依赖错误

#### Scenario: Concurrency - Unknown is not masked by an earlier success
- **WHEN** 最新attempt为unknown而之前有completed，或当前v/g已变
- **THEN** 当前投影不以旧success掩盖unknown；不匹配当前v/g/epoch的completed投影为null

#### Scenario: Success - Cross endpoint disabled waiting and blocked states
- **WHEN** 无在途的disabled、有效授权等待、due、缺授权或缺部署许可的blocked被两个接口读取
- **THEN** Activation只给真实desired_state/v/g及固定迁移提示；权威status分别给disabled、valid/authorized_waiting、due、blocked及准确authorization_required或scheduler_unavailable/egress_unavailable，不能用提示代替实际状态

#### Scenario: Concurrency - Running survives pause and configuration invalidation visibly
- **WHEN** fixture已Start后Pause或PATCH提交disabled/新v/g
- **THEN** Activation意图变更但不伪报执行blocked；权威status显示scheduling_state=disabled和旧谱系started/running、in_flight=true，current_observation不冒充新版本健康

#### Scenario: Concurrency - Unknown and authorization expiry remain separate observations
- **WHEN** 最近任务unknown、其物理权限被收回，或有限授权到期
- **THEN** status保留unknown且in_flight随终止证明true→false；过期单列authorization_state=expired/effective=false及authorization_expired阻断新Start，旧在途/unknown不被隐藏，Activation仍固定迁移提示且不因查询改变g

#### Scenario: Concurrency - Independent snapshots are not merged into a false state
- **WHEN** 两接口读取之间发生授权/配置变化，导致v/g不一致
- **THEN** 客户端刷新execution-status，不拼接旧Activation意图/当前运行；测试以PG屏障覆盖双顺序，并在无并发窗口验证矩阵，两个API保持零目标网络

#### Scenario: Security - Missing authority is not evidence of no execution
- **WHEN** 旧服务未提供权威status或状态读取失败
- **THEN** 客户端显示运行状态不可用，不把旧blocked常量、迁移提示、404或503解释为未运行/目标不健康；旧HTTP仍接流量则fixture执行门禁拒绝

### Requirement: Retention has enforceable count time and capacity bounds
run保留 SHALL 同时限制每Monitor100条终态/7天、每Workspace10000条、全库100000终态加最多32个活跃或隔离run。预算key总数 SHALL <=10000；部署最多登记10000个Monitor调度。新记录须原子预留容量，清理有界且不能删除仍有物理权限的run或有效冷却。

#### Scenario: Exception - Cleanup cannot keep up
- **WHEN** 数量上限或存储预算耗尽且无可安全清理记录
- **THEN** 拒绝新Claim/授权登记并显示retention_capacity，不突破硬cap，不影响pause/PATCH或丢弃quarantine账

#### Scenario: Concurrency - Bounded cleanup preserves references
- **WHEN** 清理器与Result/Start并发且删除最近观察
- **THEN** 按父序每事务最多100行/2s，原子清空或替换投影指针；无孤儿和反向锁，旧冷却/活跃记录不被TTL逐出

#### Scenario: Security - Disk growth is measured separately from row limits
- **WHEN** 长时间周期运行或模拟24h积压
- **THEN** 测量关系/索引/WAL/vacuum增长，运行数据预算1GiB、独立10GiB测试盘，空间80%拒绝新Claim；日志/WAL/备份有独立预算，不以行数宣称磁盘无风险
