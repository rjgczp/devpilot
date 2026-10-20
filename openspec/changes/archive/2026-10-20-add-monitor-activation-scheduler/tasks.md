# Tasks

本文件是**推荐方案A在未来独立设计评审通过、且收到新的明确Apply授权后**的实施清单。本次仅Propose，全部任务保持未勾选；规划工件完成不表示实现完成。B的claim/start/lease/fence/预算/运行记录/沙箱/生产网络部署不属于这些任务，见design第7–11节的另行评审输入。

## 1. 实施基线与授权边界

- [x] 1.1 在新Apply请求后核对独立设计评审和A产品决策记录（blocked含义、任意PATCH失效、v/g条件、100/60限额、意图寿命和未来重授权）；以明确授权及批准工件为验收依据，任何范围变化先更新规划。
- [x] 1.2 复核当时HEAD/main/origin/main、未提交内容和1–6哈希，保留用户改动；交付基线记录并确认无隐含B任务、依赖升级或生产执行入口。

## 2. Migration7与状态原子性

- [x] 2.1 编写Goose7的独立三元组activation表、全部字段/CHECK/FK/PK/索引及状态guard；用真实PG18约束测试验证混租户、空值/非法组合、scope/created_at不可变、g不回退、冷却不缩短和next_due永NULL。
- [x] 2.2 实现同一迁移事务内Monitor并发DML保护、历史disabled/g1回填和AFTER INSERT初始化；PG18 0→7/存量6→7及创建失败回滚证明每Monitor恰一状态、旧数据不变且零自动enable。
- [x] 2.3 实现配置version单调guard与AFTER UPDATE授权失效trigger，覆盖URL/name/所有字段/同值PATCH；真实PG证明v+1与disabled/g+1同Commit，旧二进制合法PATCH也失效，缺状态/g溢出/trigger故障整体回滚。
- [x] 2.4 添加7中途DDL故障、repeat up、隔离库7→6→0及应用库拒绝测试；串行修正历史up-to与next-version故障夹具边界（7移至8），验证1–6字节哈希不变并在数据库迁移文档说明发布与Down限制。

## 3. 状态读取、命令事务及持久化限额

- [x] 3.1 实现单语句Cookie身份/当前成员/完整scope关联的activation读取与精确私密投影；PG18直接Store和混scope测试证明401/404语义、Member可读及无URL/actor泄露。
- [x] 3.2 实现User→Session→Workspace→Membership→Project→Monitor→Activation保护及最终新语句的实际UPDATE门控；PG18独立连接覆盖每级等待后过期、失权两个顺序及直接Store绕Handler仍拒绝未授权DML。
- [x] 3.3 实现绑定当前v/g/body v的enable/pause及审计/g递增，不改变monitor或project version；真实两Owner同tag、同值命令、ABA、恢复和溢出测试验证最多一次消费与完整回滚。
- [x] 3.4 在相同Workspace保护内实现100 enabled计数、每Monitor持久化60秒enable冷却；两个实例并发第100/101项和同scope测试证明409/429，pause/PATCH可用且重启/配置变化不能清空冷却，记录索引与锁开销。
- [x] 3.5 注入真实回滚、Commit成功丢确认和响应丢失，验证无未确认成功/自动重试、旧条件恢复语义与GET对账；公开存储错误保持固定分类且不泄漏SQL/DSN。
- [x] 3.6 将配置PATCH、Project写、成员退出/移除/重入、邀请、Session撤销/清理和用户禁用交叉运行；用PG锁屏障/观测证明无新增反向锁或弱锁升级，文档记录A最终锁图和未知Commit边界。

## 4. HTTP协议与权限文档

- [x] 4.1 实现activation GET/PUT路由和严格1024字节双字段JSON、128字节强ETag解析；unit/contract覆盖重复键/非法Unicode/null/尾随JSON/版本语法/媒体/路径/方法/条件头，确保400/413/415/422/405准确。
- [x] 4.2 接入既有Cookie-only及精确Origin/CSRF协议、5秒请求/2秒DB预算、早拒绝drain、取消/panic安全响应；测试拒绝proof零DB，401/404/403先于428/412，429含Retry-After且错误无data/ETag/秘密。
- [x] 4.3 实现确认Commit后200的精确data与v/g强ETag、固定blocked/scheduler_unavailable、UTC及字符串版本；unit和真实HTTP/PG验证旧配置DTO/tag不变、状态无URL/health/actor/lease结果且no-store/nosniff/无Set-Cookie。
- [x] 4.4 更新OpenAPI及用户/运维说明，写明GET/PUT权限矩阵、完整错误码、enabled只是意图、任意PATCH撤销、响应丢失、100/60限额、最后审计局限和未来Worker重授权；用契约测试核对文档与路由/DTO一致。

## 5. 休眠与网络隔离验收

- [x] 5.1 添加API依赖图/入口contract与大量enabled/重启测试，验证API不导入执行内核、无生产Worker/定时扫描/执行开关/租约/run表，next_due非NULL不能保存且无积压队列。
- [x] 5.2 扩展第八阶段独立Linux零目标审计覆盖新GET/PUT及PATCH失效、未知Commit恢复等真实HTTP/PG路径；原精确白名单/来源守护不放宽，以正向违规控制和receiver/syscall证据证明正常流程零目标DNS/TCP/GET。
- [x] 5.3 保留第九阶段全部DNS/IP/SSRF/TLS/redirect/proxy/header-only/超时/取消/资源/fuzz及真实PG快照normal/race验收；适配schema7与历史schema6边界，独立runner/证据域不混用，缺Linux/IPv6/namespace依赖失败不伪报通过。
- [x] 5.4 添加敏感sentinel和最末审计长期操作测试，验证行数不随命令数增加、状态不保存URL/DNS/响应/raw error、外部DTO不公开actor；说明A恒关闭和生产出口仍未验证，不交付可开启Worker的模板开关。

## 6. 集成回归与可审查交付

- [x] 6.1 在CI中加入A专项和届时实施分支触发，保持verify/integration/execution-security证据分开；跑gofmt/vet、unit/race、全量PG18 integration/race及前九阶段回归，准确记录SHA/工具链/镜像/命令/exit/控制证据，失败依赖不可skip。
- [x] 6.2 执行本Change strict、全库strict及工件/主规格/代码一致性检查，分别记录新问题与四份历史七项长度WARNING，不修改无关规格消警；再次核对Migration1–6和没有生产入口/目标探测。
- [x] 6.3 完成按Requirement→Test→Evidence映射的实施验收记录和最终diff审查，逐项标注文档、PG18、normal/race、受控网络、部署验证状态；真实部署未访问则记未验证，保持A范围，等待独立代码/安全评审。

## Workflow follow-up

- 本次Propose完成后停止，等待独立设计评审及新的明确Apply授权；不创建PR、提交、部署或激活任何Monitor。
- B如获产品立项，另建Change解决design第7–11节中的生产出口、外部网络fence、活Owner、物理暂停语义、分布式预算/目标滥用及有界运行记录，不把研究直接执行。
- 合并、主规格同步、Archive和部署按后续明确授权分别处理；本轮不进行这些操作。
