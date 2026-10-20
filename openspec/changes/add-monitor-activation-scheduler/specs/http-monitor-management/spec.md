# http-monitor-management Delta

## MODIFIED Requirements

### Requirement: Monitors are configuration resources with inherited permissions
配置资源 SHALL 提供 Project 下 POST/list GET/detail GET/PATCH，独立activation子资源 SHALL 按monitor-activation契约提供GET/PUT意图管理。当前 Workspace Owner SHALL 创建和修改，Owner/Member SHALL 读取；创建者无额外权限。相同名称/URL SHALL 允许重复。配置资源 SHALL 不接受启停字段；系统 SHALL 不提供DELETE、归档、独立角色、跨租户列表或真实探测接口。

#### Scenario: Success - Owner creates and Member reads configuration
- **WHEN** 当前 Owner 创建相同名称和URL的两个配置，当前 Member 随后读取
- **THEN** 两个不同UUID的配置均可读，不产生网络探测、成员记录、任务、实际执行状态或结果数据，仅有默认disabled的意图子行

#### Scenario: Security - Creator leaves and readable Member cannot write
- **WHEN** 创建者退出/被移除/禁用，或当前普通Member修改可读配置
- **THEN** 配置保留且其他当前成员继续按角色访问；前创建者失权，Member写入返回403 monitor_forbidden

#### Scenario: Exception - Unsupported operations
- **WHEN** canonical collection/item接收DELETE/PUT/HEAD/OPTIONS，或访问未定义的运行/暂停/探测/跨租户聚合路径（规范activation子资源除外）
- **THEN** canonical方法失败405且Allow分别为GET, POST或GET, PATCH；HEAD无body、OPTIONS不授予CORS，未定义路径404且无副作用

### Requirement: Configuration management never performs target networking
Monitor创建、修改、读取和校验 SHALL 不解析目标DNS、不连接目标、不校验证书、不调用HTTP客户端、不发起HEAD/GET探测；数据库连接不属于目标网络。URL策略 SHALL 仅表示输入可接受，不能声称阻断所有SSRF。系统 SHALL 不接收代理、重定向策略或自定义Host/Header/凭据配置。

#### Scenario: Security - Syntactically valid hostile DNS name
- **WHEN** 合法格式的域名可能不存在或解析到私网/元数据IP
- **THEN** 本轮仍只按输入策略保存，既不查询也不标记“安全/可达/健康”，未来执行前必须重新拒绝不安全解析结果

#### Scenario: Success - No activation implied by persistence
- **WHEN** 任一配置被创建或修改
- **THEN** 创建仅初始化disabled意图；任何成功PATCH原子撤销已有授权并递增activation_generation；无任务、目标网络出站或结果，保存不自动启用，新版运行需Owner显式确认及独立执行门禁

#### Scenario: Security - Intent endpoints are also offline
- **WHEN** Owner启用/暂停或成员读取activation子资源
- **THEN** 只处理授权意图，不解析目标DNS或连接，不调用执行内核，不能把enabled响应当作安全或健康证明
