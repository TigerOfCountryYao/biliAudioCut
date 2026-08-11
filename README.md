# BiliAudioCut

面向内部成员的商品采集与商品视频生产系统。成员提交京东商品链接后，由已绑定且获得用户授权的 Chrome 扩展采集可售 SKU、规格与允许的图片素材；系统随后生成可编辑的视频方案，并最终产出合集 MP4 与封面图。

项目不导出浏览器 Cookie、登录态、订单或账号资料；商品页面采集仅在用户已授权的浏览器会话中进行。

## 当前状态

当前已完成本地后端基础设施：

- Go API 的健康检查与 PostgreSQL 就绪检查；
- PostgreSQL、Goose SQL 迁移和 sqlc 类型安全查询生成；
- 初始管理员的本地 CLI 初始化；
- 用户与可撤销会话的数据模型。

产品采集扩展、Web 前端、登录接口、异步作业与视频渲染仍在开发中。完整方案见 [`docs/spec.md`](docs/spec.md) 和 [`docs/adr`](docs/adr)。

## 技术组成

- Go API：用户、权限、项目、采集校验与作业调度；
- PostgreSQL：业务事实、会话与作业的唯一事实源；
- Redis：后续用于扩展在线状态、进度广播与限流，不保存业务事实；
- Next.js：Web 前端；
- Chrome 扩展：在授权商品页采集数据；
- Node Renderer：后续通过 HyperFrames 与 FFmpeg 渲染视频。

## 本地开发

前置条件：Go 1.25+、Docker Desktop 和 [Task](https://taskfile.dev/)。首次使用还需让 Go 的二进制目录位于 `PATH`；Windows PowerShell 可执行：

```powershell
$goBin = Join-Path (go env GOPATH) 'bin'
$env:Path += ";$goBin"
```

启动开发数据库：

```powershell
docker compose -f deploy/compose.dev.yaml up -d
docker compose -f deploy/compose.dev.yaml ps
```

首次初始化工具、迁移和 sqlc 代码：

```powershell
task setup
```

启动 API：

```powershell
$env:DATABASE_URL = "postgres://product_video:local-dev-only@127.0.0.1:15432/product_video?sslmode=disable"
task dev
```

另开终端验证：

```powershell
Invoke-RestMethod http://localhost:8080/healthz
Invoke-RestMethod http://localhost:8080/readyz
```

## 常用命令

```powershell
task check                              # go vet + go test
task go:fmt                             # 格式化 Go 文件
task db:migration:new -- migration_name # 创建 Goose 迁移
task db:migrate:up                      # 应用迁移
task db:generate                        # 根据 SQL 生成 Go 查询代码
```

首次管理员只能通过本地命令创建，不提供公共注册接口：

```powershell
$env:DATABASE_URL = "postgres://product_video:local-dev-only@127.0.0.1:15432/product_video?sslmode=disable"
Push-Location backend
go run ./cmd/admin create --email "admin@example.com" --name "Admin"
Pop-Location
```

## 目录

```text
backend/
  cmd/api/       API 可执行入口
  cmd/admin/     本地管理员初始化 CLI
  internal/      按业务能力组织的 Go 模块
deploy/          Docker Compose 配置
docs/            规格与架构决策记录
extension/       Chrome 扩展（开发中）
frontend/        Web 前端（开发中）
renderer/        视频渲染服务（开发中）
```

不要提交 `.env`、本地采集产物或任何账号密钥。
