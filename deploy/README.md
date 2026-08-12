# NAS 部署

此目录的生产 Compose 栈包含 PostgreSQL、数据库迁移、Go API、Next.js 网页、以及内部 Nginx 网关。数据库不暴露到 NAS 网络；网关默认监听 `18089`，用于内网验收。

## 内网验收

在 NAS 的项目目录复制环境变量文件并生成密码：

```sh
cp deploy/.env.example deploy/.env
```

将 `POSTGRES_PASSWORD` 与 `DATABASE_URL` 中对应密码替换为同一个高强度随机值，然后启动：

```sh
docker compose --env-file deploy/.env -f deploy/compose.production.yaml up --build -d
docker compose --env-file deploy/.env -f deploy/compose.production.yaml ps
```

浏览器访问 `http://192.168.10.4:18089`，并使用下载的扩展包完成授权与采集验证。

首次部署还需要创建唯一的管理员账号；该命令会在终端交互式要求输入并确认密码：

```sh
docker compose --profile admin --env-file deploy/.env -f deploy/compose.production.yaml run --rm admin create --email "admin@example.com" --name "Admin"
```

## 切换到正式域名

Cloudflare 和 NAS 的 HTTPS 反代配置完成后，修改 `deploy/.env`：

```dotenv
PUBLIC_ORIGIN=https://your-domain.example
COOKIE_SECURE=true
```

随后重新构建并启动。扩展内置 API 地址和 host permission，因此切换域名后必须从网页重新下载并安装新的扩展包。系统 Nginx/Cloudflare 应将该域名转发至 `http://127.0.0.1:18089`，并支持 WebSocket 升级请求。

## 常用运维命令

```sh
docker compose --env-file deploy/.env -f deploy/compose.production.yaml logs -f
docker compose --env-file deploy/.env -f deploy/compose.production.yaml down
```
