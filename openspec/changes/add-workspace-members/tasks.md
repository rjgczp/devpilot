# Tasks

2026-10-15：用户确认独立技术评审通过，正式批准Apply及完成后创建指向main的PR；四项产品决策全部接受。以下任务按实际实现和验证结果逐项勾选。用户已确认加入另做需目标同意的 Change，不创建POST/邀请/接受/重加/角色授予接口，不用生产SQL补足加入能力。

## 1. 评审定稿与实施边界

- [x] 1.1 取得独立技术评审结论和用户新的Apply指令，核对Design末尾四项产品决策（名单可见性、Owner自退、删除重试、应用事务保证）；通过记录已接受契约及同步所有规划文件验证，未完成前不编码。
- [x] 1.2 开始实施时重新核对main/origin/main、Git状态和适用指令，保留所有未提交文件，使用适合的checkout及`codex/add-workspace-members`分支；以基线SHA、工作区记录及迁移1/2/3哈希核对验证，不擅自reset或覆盖本规划。
- [x] 1.3 在README及OpenAPI先落定本轮两个端点、无POST/重加、Owner关系而非active Owner不变量、无数据库强制/RLS边界；以文档差异及接口清单核对验证，无新依赖或迁移。

## 2. 成员名单：同一快照授权与有界分页

- [x] 2.1 在internal/workspace增加仅接收Cookie摘要和Workspace选择的窄成员读取接口，使用同语句identity/member/Workspace授权及只含user_id/role/joined_at的投影；以真实PG18 Owner/Member成功、未知身份401、非成员/未知Workspace同404、空页200测试验证。
- [x] 2.2 复用或窄化现有limit/after及UUID校验实现用户UUID升序分页，默认50/最大100/limit+1，无游标存在性查询；以边界/重复键/非法编码单元测试及PG18多页、其他租户cursor、disabled关系无status测试验证。
- [x] 2.3 完成GET成员Handler及精确路由注册，复用Cookie、安全错误和5s/2s预算，禁止HEAD隐式成功；以HTTP状态/Allow/最小DTO/无Set-Cookie及跨租户替换单元/集成测试验证，不改变旧Workspace路由。
- [x] 2.4 同步OpenAPI的MemberDTO、分页、401/404及快照语义，并补README名单示例；以已有契约测试模式校验文档字段/响应和真实HTTP投影一致。

## 3. 成员写：实时身份、租户授权与Owner保护

- [x] 3.1 实现READ COMMITTED成员删除事务，按User FOR SHARE→Session FOR SHARE→Workspace FOR NO KEY UPDATE→Membership按user_id排序FOR UPDATE取得保护，且所有等待后使用新语句检查身份时间；以PG18逐层真实阻塞观察及静态SQL范围评审核对顺序，整段事务≤2s，不自动重试。
- [x] 3.2 实现Owner删普通Member、Member自退、Owner非final自退、禁止Member删他人与Owner删其他Owner；以PG18完整权限矩阵、disabled目标可删、actor/target跨租户复用及伪造身份测试验证，DELETE必须同时绑定workspace_id与target_user_id。
- [x] 3.3 在Workspace锁后新快照核对Owner关系数量，保证最后Owner自退409，已损坏零Owner状态允许写前500且不自动修复；以真实PG18 1/2 Owner及guarded损坏夹具验证，不新增计数器、触发器、role API或migration。
- [x] 3.4 实现Owner删除缺失关系的确认204和自退后重试404，禁止查询全局目标users或校验目标active；以重复删除、随机不存在用户UUID、其他租户用户UUID、退出后旧Cookie全Workspace读/写拒绝及auth/me仍有效的PG18测试验证。
- [x] 3.5 将锁顺序、所有writer义务、普通SQL边界和未知提交后的检查方式同步README/运维说明；以SQL路径清单及Design/Spec对照验证，没有可绕过协议的产品/后台/seed writer，不给出生产加入SQL。

## 4. 删除HTTP协议与安全故障

- [x] 4.1 完成DELETE路由与严格无query/非空body拒绝，复用auth.ValidCSRF/CookieDigest且在数据库前校验来源，保证读体有界且5s/客户端取消可终止；以Origin/FetchMetadata完整矩阵、零DB调用、慢体/取消、非法路径/额外段/编码斜杠无变更重定向测试验证。
- [x] 4.2 固定所有新状态/错误message、204空body无JSON Content-Type/Location、no-store/nosniff及永不Set-Cookie；以单元HTTP和PG18契约测试验证，包括成员/owner权限403、非成员404、最后Owner409及POST始终405/Allow: GET且零DB调用。
- [x] 4.3 实现依赖/内部错误分类及有界回滚/连接丢弃，保留未知Commit不成功/不重试和取消不响应；以事务删除后确认回滚、底层commit成功但回执丢失、commit前回滚两种受控注入、pool/锁超时、缺schema、40P01/40001/未知57014及恢复测试验证。
- [x] 4.4 复用或扩展安全panic边界和仅固定类别日志；以UUID/email/token/digest/PHC/DSN/SQL/原始路径及敏感panic哨兵、并发成功/失败互不污染、旧请求不清新Cookie测试验证，无原始诊断泄露。
- [x] 4.5 同步OpenAPI/README中DELETE无JSON输入、方法/CSRF/错误表、幂等状态和self-exit404、无添加/重加限制；以契约测试及文档示例实际命中预期状态验证。

## 5. PostgreSQL18并发证据

- [x] 5.1 扩展现有真实pgx观察器，用独立连接/屏障和pg_blocking_pids验证两Owner自退双获锁顺序始终204/409、互删403/403，以及失败/回滚仍保留Owner；以最终数据库关系及无40P01/40001的受支持协议执行结果验证，不用sleep推断顺序。
- [x] 5.2 覆盖两个Owner重复删同一Member、Owner移除与Member自退双顺序、不同Workspace同时变更；以204/204、204/404或204/204及精确影响行/其他租户未变验证同租户串行和跨租户独立。
- [x] 5.3 以协调锁协议的guarded role-change/member-remove夹具覆盖actor降级/移除及target role改变在保护前/后的两种顺序；以404/403/409或先允许完成再变更的数据库提交顺序验证，夹具不作为公开角色管理/加入功能。
- [x] 5.4 覆盖Session撤销、用户禁用（User先于Session）、cleanup删除或SKIP LOCKED及Workspace创建/登录混合运行；以PG锁观察、双方顺序、最终身份结果与各操作预算验证无反向锁死或全局租户锁。
- [x] 5.5 在User/Session/Workspace/Membership每个可能等待点推进数据库时间，验证锁后到期401；暂停最终有效门控后跨越expiry仍可提交；以数据库时点及精确关系状态验证，不能用事务开始时间替代最终语句时间。
- [x] 5.6 覆盖名单快照前后revoke/disable/成员删除、跨页撤销及已提交删除但响应丢失；以真实SQL返回后受控延迟与新请求拒绝验证旧快照可完成、新请求无旧权限，并标注提交回执故障属于注入证据。
- [x] 5.7 将本组证据和原申请中延期的添加/重复加入/目标无效/重加场景映射到验收记录；以Requirement→实际测试名/证据路径表验证所有本轮四类场景有证据，延期项明确标识且不拿fixture插入冒充成功加入。

## 6. CI与全量集成验收

- [ ] 6.1 CI push触发加入`codex/add-workspace-members`并保留原main/PR规则、Go工具链、PG18 digest和Goose v3.26.0；以配置差异审阅及实际CI任务触发验证，集成/race套件串行共享专用库。
- [x] 6.2 运行gofmt、git diff --check、go vet、全量unit和race，回归注册/登录/me/logout/Session TTL/cleanup/健康与关闭/Workspace三个原API；以成功命令输出验证，不因本轮范围缩小删旧用例。
- [x] 6.3 在testdb守护的独立PG18数据库运行完整integration及integration-race、原迁移链/约束回归与缺URL/Goose/PG18硬失败检查；以实际服务器版本、命令输出、迁移1/2/3哈希和版本仍3验证，不读开发.env、不连接应用库、不跳过必需测试。
- [x] 6.4 运行openspec严格校验并审阅最终API/Spec/Design/README一致性，整理可复现验证记录；以全部成功/异常/安全/并发Requirement证据映射及未交付加入功能清单验证，明确本地结果与托管CI结果，不自动PR/部署/合并/归档。

## Workflow follow-up

- 独立技术评审及Apply授权已于2026-10-15收到；如实现揭示需改变契约，先提出并同步规划产物。
- 后续加入需单独规划目标有效Session同意、member固定角色、隐私发现、重复/过期/撤销/重加及旧DELETE竞态，不预设邀请实现或邮件依赖。
- 本轮已授权完成后创建PR；不合并、不归档、不公开部署，等待独立代码审查。
