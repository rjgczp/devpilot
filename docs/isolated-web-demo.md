# 本地隔离 Web 演示

此入口将已审查 Dashboard、历史 API 与 Task9.1 的 PG18、双 Worker、可信 Supervisor 和无上游双栈 fixture 联通。它是**有界、可销毁演示**，不是公网监控部署，不改变 OpenSpec37/47、延期8.3及9.2–10.5。

## 启动和操作

准备 Docker/Linux 容器与原9.1所需 NET_ADMIN/IPv6能力、Node24+/npm、Python3、OpenSSL。首次构建会下载固定镜像/构建依赖；实际执行时仍无上游目标网络。

```
./scripts/dev-isolated-demo.sh
```

1. 等待“隔离演示已就绪”，打开 <http://127.0.0.1:5174>。原普通 Dashboard 的5173和独立开发库不受此入口操作。
2. 在本机读取终端指出的 `.devpilot-isolated-demo/credentials-<PID>.json`，使用临时 Owner 邮箱/随机密码登录。文件600，私有父目录700；仅可执行工具子目录755供受限容器遍历。目录/文件被git忽略，Vite禁止读取；密码不打印到证据、argv或环境。无需注册新的账号。
3. 选择已有 Workspace/Project，固定 Monitor 名为“本地演示 · 隔离 HTTPS fixture”。初始未授权、无Run。点击“授权激活”，查看固定目标，勾选确认，再提交现有900秒有限期 Owner 授权。没有自动授权或伪造会话。
4. 点击“历史”并停留页面：真实检查每60秒一个周期，状态轮询每10秒；新Run/Result会自动刷新演示历史及趋势。只展示实际最小结果，不注入投影行。默认做3个周期，随后关闭执行gate，保留真实API/记录供查看；整个演示最长15分钟。
5. 可使用原暂停接口停止新的批准；周期之间暂停将结束本次有界演示执行并保留查看，重新演示需重启脚本。已开始的检查仍可能完成。固定目标配置只读，创建、编辑、删除均禁用且服务端拒绝。授权/暂停与物理执行相竞时原门控优先；若fixture无法继续确认，演示保守停止，不扩展超时、不自动重发GET或冒充物理释放。它不是常驻调度服务。
6. Ctrl-C结束，自毁本次标签下容器/网络/测试库，清除临时登录文件。原始运行证据保留在终端所列目录。终止容器仅为演示拆除，不是物理释放证明。不保留演示业务数据作为生产记录。

## 固定目标和可信边界

- 唯一允许 URL 为 `https://a.fixture.example.com/fixture/private-path-SENTINEL`，是原9.1受控DNS/TLS接收器的虚构fixture名称。示例IP映射仅在该封闭拓扑内，不使用公网DNS或公网服务器。页面标识“本地演示 · 无公网出口 / 隔离演示”。
- 复用原TargetDirectory，目录仅绑定完整W/P/M及固定配置版本、URL和地址；原完整DNS验证、Target Permit、Fence、单次输入、拒绝重放、物理pidfd/reap/namespace退休及签名验收全部保留。两个Worker的既有LOGIN、EXECUTE、2秒事务/250ms锁预算不变。没有生产Go、迁移、基础角色或表列授权变更。
- 演示API复用原认证、Workspace/Project/Monitor处理器和本次私有PG18；只在可信协调器的loopback监听。外层限制仅允许登录/退出和固定Monitor的既有Owner授权/暂停写入，其他写操作403；正式鉴权、CSRF和ETag仍由原处理器验证。UI标记不是权限边界；直接调用API也不能创建/修改任意URL。
- PG/Worker/API控制容器仅在原internal双栈网络，**不发布端口、不加外联桥或上游路由**。Probe仍network-none数据平面、caps0、无DB/源码/Docker挂载；私钥仅在原可信宿主Authority。API无法调用执行管理路径。
- 浏览器入口仅127.0.0.1:5174。host侧::1网关通过已有宿主Docker管理权限的私有stdin/stdout调用协调器内固定loopback HTTP客户端；该客户端唯一目的地127.0.0.1:5174，不接受目标URL、主机或端口。Cookie/请求体不落日志。网关限4个同时进行的Docker转发、16KiB请求、2MiB响应；无Docker socket下放。它只桥接本地演示API，不是通用公网代理。
- 预置Owner密码经真实密码Hasher写入演示账号；用户真实登录后由原Cookie/CSRF/If-Match/有限期同意协议授权。没有新增数据库角色或权限；仅复用9.1既有临时角色和非生产协调器管理身份。

## 本轮定向验收

启动脚本只构建原9.1工具，不重跑历史全矩阵。`npm run --prefix web test:demo` 使用浏览器完成真实登录、核对无历史、直接拒绝任意URL创建/修改、勾选Owner授权，在同一历史页等待3个真实60秒周期、检查HTTP200/耗时/Run不重号/时间间隔/脱敏、趋势自动更新及暂停。原接收器逐Run核对仅1次GET，确认真实物理释放后才Commit最小Result；原始 cycles、receiver、规则与签名证据保留。没有mock或手工Result插入。

CI `Isolated Web demo` 在Linux同一SHA执行此用例，保存 `isolated-web-demo-evidence`。不宣称磁盘容量安全，不放宽旧门禁；历史125ms、IPv6与管理确认超时未知原因仍保留。仅独立PR，不合并、不部署、不开放生产出口。
