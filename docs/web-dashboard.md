# 本地 Web Dashboard

已复用现有 Go API、Cookie/CSRF、Owner/Member、强 ETag 和 execution-status。
没有改 Go 后端、数据库迁移、权限或执行隔离。React + TypeScript + Vite，支持深浅色和手机布局。

## 一键启动

前置条件：Docker Compose、Go1.24.3+、Node24+、Python3、OpenSSL，以及本机 IPv4/IPv6 回环。
在仓库根目录运行：

```sh
./scripts/dev-dashboard.sh
```

打开 **http://127.0.0.1:5173**，不要替换成其他 hostname。首次启动自动安装固定
Goose3.26.0/锁定 npm 依赖，创建专用 PG18 本地开发库并应用现有迁移。
仅首次准备依赖需要外网，不访问任何 Monitor 目标。

新账号从登录页点「创建账号」，使用自己的邮箱和15–128字符密码注册，再登录。
不提供或保存默认账号/密码。依次创建 Workspace → Project → Monitor。

1. 创建监控，输入 URL、60秒起的间隔、1–30秒超时和预期状态码。
2. 点击「授权激活」，核对当前 URL/间隔，选择15分钟/1小时/24小时并明确确认。
3. 查看列表或点击监控名称查看权威状态；10秒自动刷新，切后台暂停轮询。
4. 「暂停」阻止新的检查；已在途/unknown不会因此伪装为停止或成功。
5. 编辑使用服务器 ETag；任何成功 PATCH 都撤销原授权。412时保留输入，明确重新加载后再提交。
6. 可切换 Workspace/Project、搜索/筛选、分页加载及深浅色。Member控件只读，后端仍最终鉴权。

**普通本地 API 不会启动 Worker，执行门仍关闭。** 授权后显示「授权有效 / 执行已阻断」
和「执行环境未开放」是实际状态，不是目标失败。真正的隔离三周期检查仍使用
[已验收9.1 runner](../tests/scheduler/README.md)，本轮不把任意用户 URL 接到测试执行器。
有真实 Run 的现有兼容 API 可显示 running/unknown/completed；新 unknown 优先于旧 success，
状态读取失败显示不可用，不回退旧成功记录。浏览器回归中的状态注入明确只是测试。

Ctrl-C 停止此脚本拥有的 API、Vite 和专用 PG，保留本地数据卷；再次启动可继续登录。
随机开发库密码仅位于 Git 忽略的0600 `.devpilot-dashboard.env`，不读取生产 `.env` 或外部 DATABASE_URL。
生成的二进制/日志在忽略目录 `.devpilot-dashboard/`。不要提交这些本地文件。

本地前端监听IPv4回环，API监听IPv6回环，同用5173端口：这是为遵守既有开发认证端口约束，
不重写 Origin、不放宽 CSRF/CORS。PG只映射127.0.0.1:55432。所有服务只供本机开发，未部署。

## 定向验证

```sh
npm ci --prefix web
npm run --prefix web build
npm run --prefix web test
# 保持一键脚本运行；首次安装浏览器，然后执行：
cd web && npx playwright install chromium && cd ..
npm run --prefix web test:e2e
```

浏览器用例包括真实 PG18 注册/登录/CRUD/授权/暂停/刷新/退出，以及独立的 unknown优先、
running/completed、Member只读、失效读取、跨Workspace迟到响应、401和412不重放。
不重跑调度/故障历史全矩阵。CI为独立 Web Dashboard 工作流。

## 范围与后续

Task9.1已按人类独立验收登记为完成，既有OpenSpec进度 **37/47**；8.3保持延期，全部原始失败证据保留。
本 Web MVP 是新的明确授权范围，不替代或勾选9.2–10.5，也不增加执行入口、Migration11或角色。第二轮历史只读函数的精确 EXECUTE 变更见历史说明。
原调度设计中「不做前端」是其原阶段范围，本轮用户已单独授权本 Dashboard。

检查历史已提供有界只读 API、状态筛选、分页及响应时间趋势，见 [历史说明](monitor-history.md)。

未实现删除、告警、复杂动画或生产执行开放。后续可交付保留在途物理终止语义的删除协议；它们不阻塞当前登录与 Monitor 管理 Demo。
