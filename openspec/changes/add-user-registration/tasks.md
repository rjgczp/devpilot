# Tasks

以下为已授权的Apply任务：完成最后规格修订并通过严格校验后逐项实施/验证再勾选。需求依据为两份 delta specs；技术推荐和审核确认项见 design.md。按依赖顺序实施，每组同时交付自己的测试与文档。

## 1. 迁移基础与 users schema

- [x] 1.1 在开发/CI工具说明固定兼容Go基线的Goose CLI版本，并在go.mod/go.sum固定x/crypto；验证工具版本、原生CLI参数、`go mod verify`及编译，记录维护依据，不引入自定义迁移入口或隐式升级工具链。
- [x] 1.2 建立独立 PG18 集成测试 harness（integration tag、TEST_DATABASE_URL、固定测试库名和 current_database 二次确认、串行 schema 操作）；以拒绝开发库/缺配置/不可达库的测试证明任何变更前已校验，并交付 README 测试库准备与隔离说明。
- [x] 1.3 编写 `migrations/00001_create_users.sql` 原生Up/Down，落实UUIDv7、规范邮箱checks/具名唯一约束、active/disabled、验证和时间字段；password_hash仅NOT NULL及1–1024-byte约束，无算法/参数/PHC正则。真实PG18测试验证空值/空串/超长拒绝，参数升级的有效PHC无需schema修改。
- [x] 1.4 接入固定版本原生Goose CLI到开发/集成验证命令，复用其事务、版本记录和 `-timeout 30s`，显式 `-env=none` 及GOOSE_DBSTRING；在独立库验证status/up，不新增cmd/migrate、嵌入迁移包、自定义锁/确认开关或CLI脱敏框架。
- [x] 1.5 增加真实PG18迁移测试：同一CLI和SQL目录的up/up、disposable库up/down/up、坏SQL事务回滚和版本不误记；确认重复up保留行，清理仅限测试库，不测试或承诺未配置的CLI并发锁。
- [x] 1.6 交付 `docs/database-migrations.md`：原生命令/版本不可变/环境凭据、受限原始运维诊断、共享迁移串行与先up后API顺序；明确常规Down仅disposable库，共享/生产仅status/up，破坏性例外需单独授权/验证备份/恢复评审。核对无自动DDL或虚构CLI保护，测试库命令可复现。

## 2. 安全密码处理

- [x] 2.1 实现 `internal/password` 暂定Argon2id 64 MiB/t=3/p=1、16-byte随机盐、32-byte输出和v19 PHC；由模块/真实参数测试验证完整格式、参数、盐/输出长度、独立盐、同密码重算、不同密码不匹配及编码异常，确保写库无明文，不让DB承担PHC验证或增加登录API。
- [x] 2.2 支持受控随机源故障测试，确认失败不产生可用 hash 且不暴露输入；结合日志/错误断言和最小服务测试证明失败时不会调用存储。
- [x] 2.3 以真实参数运行并发1/2的资源基准，记录硬件、Go版本、样本数、p50/p95、allocations及进程峰值内存（含非Argon开销），据此形成性能目标和README记录；不宣称未实测SLO或128 MiB进程硬上限，超预算先复审资源/预算，任何降参另需明确审核。

## 3. PostgreSQL 用户存储

- [x] 3.1 实现 `internal/user` 的单条参数化 INSERT RETURNING 安全字段与最小 Store 边界，复用现有 pgxpool；真实 PG18 测试核对持久化字段、UUID、PHC、时间和返回投影，SQL 注入输入不得改变语句行为。
- [x] 3.2 实现users_email_key冲突409、连接不可用503 service_unavailable、DB子期限/确认statement timeout的503 database_timeout、缺users/其他内部错误500 internal_error；真PG错误与fault fake验证分类、未知57014不误判、PgError不透出及无自动重试/upsert。
- [x] 3.3 添加并发创建与旧账户保护集成测试：同邮箱两次插入仅一行，case/空白经规范化后冲突，不改已有 hash/status/验证/时间；在迁移文档描述原子插入与未知提交结果的重试语义。

## 4. 注册服务与 HTTP 合约

- [x] 4.1 实现有界严格 JSON 读取和 method/media 检查，覆盖4096边界/分块/尾随、多值、未知/重复键、null/类型/缺字段和原始UTF-8/Unicode转义（孤立/反序/缺损代理对、非hex/截短、合法代理对、有效U+FFFD）；不得只检查解码后utf8.ValidString；表驱动 handler 测试证明对应400/405/413/415、Allow头及 HEAD 无body，非法请求不调用 hasher/store。
- [x] 4.2 实现ASCII邮箱产品子集/归一化、密码15–128 code points/512 bytes及固定422原因码；覆盖邮箱/域标签长度、Unicode/空白密码等边界，确认密码不trim/截断、不去邮箱tag；API文档将其他邮箱格式列为MVP暂不支持而非普遍无效。
- [x] 4.3 实现廉价校验后获取2槽、无队列、5s处理/2s数据库预算和哈希前后取消检查；以占用计数/阻塞hasher验证非法请求不占槽、计算中取消仍占槽、第3个有效请求503/Retry-After、实际返回后槽位复用及成功/错误/异常最终释放，无后台逃逸/取消后INSERT；区分服务端数据库超时、连接不可用与客户端取消，后者不写HTTP503，并测试原始context取消及真实HTTP断连。
- [x] 4.4 实现201安全DTO、JSON错误/固定消息/安全头和全部状态分类、注册边界安全panic处理；测试active/null只表示未禁用且不宣称验证，拒绝客户端验证字段，不发token/cookie/mail；对超时/断连未知提交仅报无法确认，覆盖超时/取消/提交不确定的无重试及panic/PgError sentinel脱敏。
- [x] 4.5 在 `cmd/api` 组合注册路径与现有 health routes，继续共享连接池、slog和Runner；接线测试覆盖真实路径命中、健康端点原有文本契约、无schema也可启动、故障恢复与既有信号退出测试回归。
- [x] 4.6 同组交付OpenAPI/README示例和完整错误表：15–128码点/512 bytes密码、4096-byte JSON、ASCII兼容限制、重复409不覆盖旧账户、缺表500/连接503/DB超时503、未知提交；明确后续不得凭active信任邮箱、槽位不等于反滥用及公开部署前必须实现并评审注册限流/防滥用。用全状态合约测试和文档核对验证，不实现未来验证/限流业务。

## 5. 真实端到端验收与 CI

- [x] 5.1 以真实PG18、原生Goose/同一SQL目录、root router和真实Argon参数跑HTTP集成：201及active/null、大小写/并发201/409、旧密码/disabled不变、连接不可用503、锁等待导致DB期限503 database_timeout、缺表500及安全日志；确认数据、响应与两个Specs一致。
- [x] 5.2 扩展现有CI增加PG18测试服务和独立integration job，显式提供隔离库并运行 `go test -tags=integration ./... -count=1`；证明缺测试依赖会失败，现有普通/race job无需数据库仍能通过，README命令与CI一致。
- [x] 5.3 完成跨组最终检查：gofmt、`go vet ./...`、`go test ./... -count=1`、`go test -race ./... -count=1`、PG18 integration和OpenSpec严格校验；记录场景到测试映射与实际结果，确认没有扩展到排除功能且全部API/开发文档可复现。

## Workflow follow-up

- 最后修订和严格校验通过后按用户授权实施；完成后创建PR等待独立审查，不自动合并/归档。
- 公开部署前另行实现并评审注册限流与防滥用控制；本Change完成和每进程2个哈希槽均不能满足该前置条件。本任务表只交付记录/评审要求，不扩展限流实现范围。
- 实现验收完成后推送分支并创建PR等待独立审查；合并、同步主Specs与归档不在本轮授权内。
