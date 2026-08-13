# NAS 部署

生产 Compose 栈包含 PostgreSQL、数据库迁移、Go API、Next.js 网页和内部 Nginx 网关。数据库不对 NAS 网络暴露；网关默认监听 `18089`。

业务代码不在 NAS 上构建。开发电脑手动构建 `linux/amd64` 的 backend、web 镜像并推送到 GHCR，NAS 只负责拉取和运行镜像。PostgreSQL、Nginx 和 cloudflared 继续使用运行时基础镜像，数据库卷不会因业务镜像更新而改变。

## 手动发布业务镜像

首次发布前，在 GitHub 创建具有 `write:packages` 权限的访问令牌，然后交互式登录。令牌只输入 Docker，不要写入仓库或环境变量文件：

```sh
task release:login
```

确保准备发布的代码已经提交并通过测试，然后使用不可变版本号构建和推送。`release` 同时更新 `stable` 标签：

```sh
task release VERSION=2026.08.13-1 PUBLIC_ORIGIN=https://biliaudiocut.tftocd.com
```

第一次推送后，可在 GitHub Packages 中把两个镜像设为公开，使 NAS 无需保存 GitHub 令牌即可拉取。若保持私有，则在 NAS 上另用只有 `read:packages` 权限的令牌执行一次 `docker login ghcr.io`。

## 内网验收

在 NAS 的项目目录复制环境变量文件并生成密码：

```sh
cp deploy/.env.example deploy/.env
```

把 `POSTGRES_PASSWORD` 和 `DATABASE_URL` 中相应密码改为同一个高强度随机值，然后拉取并启动：

```sh
docker compose --env-file deploy/.env -f deploy/compose.production.yaml pull
docker compose --env-file deploy/.env -f deploy/compose.production.yaml up -d
docker compose --env-file deploy/.env -f deploy/compose.production.yaml ps
```

浏览器访问 `http://192.168.10.4:18089`，并使用网页下载的扩展包完成授权与采集验收。

首次部署还需创建唯一的管理员账号；该命令会在终端交互式要求输入并确认密码：

```sh
docker compose --profile admin --env-file deploy/.env -f deploy/compose.production.yaml run --rm admin create --email "admin@example.com" --name "Admin"
```

创建后续普通成员账号（同样会要求输入并确认密码）使用：

```sh
docker compose --profile admin --env-file deploy/.env -f deploy/compose.production.yaml run --rm admin create-user --email "member@example.com" --name "Member"
```

## 通过 Cloudflare Tunnel 公开访问

Cloudflare Tunnel 不需要公网 IP、路由器端口映射或 NAS 证书。它会由 NAS 主动向 Cloudflare 建立出站连接，并把一个固定的 HTTPS 子域名转发到本项目的网关。

1. 在 Cloudflare Dashboard 的 **Networking → Tunnels** 创建远程托管 Tunnel。
2. 在该 Tunnel 添加 Public Hostname：
   - 主机名：`biliaudiocut.tftocd.com`
   - 服务：`http://127.0.0.1:18089`
3. 从 Docker 安装命令中仅复制 Token 值，写入 NAS 的 `deploy/.env`：

```dotenv
PUBLIC_ORIGIN=https://biliaudiocut.tftocd.com
COOKIE_SECURE=true
CLOUDFLARED_TUNNEL_TOKEN=从Cloudflare页面复制的完整Token
```

4. 启用 Tunnel profile：

```sh
docker compose --profile tunnel --env-file deploy/.env -f deploy/compose.production.yaml pull
docker compose --profile tunnel --env-file deploy/.env -f deploy/compose.production.yaml up -d
```

Tunnel 容器使用 NAS 的 host network，因此 Cloudflare 控制台里的 `127.0.0.1:18089` 指向 NAS 上的网关。切换公开域名后，必须重新从网页下载并安装扩展包，因为扩展内置 API 地址和 host permission。Tunnel Token 等同于连接凭据，只能保存在 NAS 的 `deploy/.env`，不得提交或发送到聊天。

## 常用运维命令

```sh
docker compose --profile tunnel --env-file deploy/.env -f deploy/compose.production.yaml pull
docker compose --profile tunnel --env-file deploy/.env -f deploy/compose.production.yaml up -d
docker compose --env-file deploy/.env -f deploy/compose.production.yaml logs -f
docker compose --profile tunnel --env-file deploy/.env -f deploy/compose.production.yaml logs -f cloudflared
docker compose --env-file deploy/.env -f deploy/compose.production.yaml down
```

如需回滚，把 `deploy/.env` 的 `APP_VERSION` 改成之前发布的不可变版本号，再执行 `pull` 和 `up -d`。确认恢复后再决定是否重新指向 `stable`。
