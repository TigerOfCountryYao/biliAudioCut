# 商品视频生产系统规格（v0.1）

状态：已确认的第一版架构与行为规格。本文不包含实现代码或具体库选择。

## 1. 目标与非目标

### 目标

- 内部成员在网页粘贴京东链接，由已绑定 Chrome 扩展自动采集可售 SKU、规格和允许的商品素材。
- 用户选择 SKU、横竖画幅和一套视觉模板；系统独立生成三套可编辑的卡片计划。
- 用户确认方案后，系统以真实 TTS 音频时长驱动卡片播放，异步生成一个合集 MP4 与独立封面图。
- 项目、采集事实、AI 调用状态和产物可追踪、可恢复、可按保留期清理。

### 非目标

- 不绕过网站访问控制，不导出 Chrome Cookie、登录态、订单或账户数据。
- 不做公共注册、拖拽式视频编辑器、逐字字幕、应用商店自动安装或多机高并发渲染。
- 不以 Redis 作为事实源，不以预估文案时长决定卡片时长。

## 2. 部署与网络

系统部署在飞牛 FN EVO 4 NAS 的 Docker 环境中。N150 与 8GB 内存按单并发视频渲染规划。

```text
Internet
  -> 免费 DDNS 域名
  -> 路由器：仅 80/443
  -> NAS 反向代理：TLS / Let’s Encrypt
  -> Docker 内网：Next.js | Go API | Node Renderer | PostgreSQL | Redis
```

- 对外仅一个 HTTPS 域名；网页使用 `/`，Go API 使用同源 `/api/*`。
- NAS 管理后台、SSH、数据库、Redis、Node 渲染服务均不得暴露公网。
- 反向代理负责 TLS 终止和同源路由。业务身份认证仍由 Go API 实施。

## 3. 组件与边界

| 组件 | 职责 | 公开性 |
| --- | --- | --- |
| Next.js 前端 | 项目创建、选择与编辑、进度、下载、扩展安装页 | 公网 HTTPS |
| Chrome 扩展 | 在用户已授权页面采集、维护到服务端的认证 WebSocket | 用户浏览器 |
| Go API | 用户与权限、项目状态机、采集校验、AI 编排、OpenAPI | 通过 `/api` 公开 |
| Node Renderer | HyperFrames 与 FFmpeg 渲染；由 Go 调度器调用 | Docker 内网 |
| PostgreSQL | 唯一事实源、作业表、元数据 | Docker 内网 |
| Redis | 心跳、在线映射、进度广播、限流 | Docker 内网 |
| NAS 挂载存储 | 原图、音频、封面、MP4 | Docker 内网 |

Go API 维护 REST JSON OpenAPI 合约；Next.js 与扩展使用由该合约生成的 TypeScript Client。Node Renderer 只接受持有作业租约的 Go 调度器发出的内部渲染请求，不直接服务用户浏览器或读写业务数据库。

## 4. 身份、权限与浏览器绑定

- 账号由管理员邀请创建，无公开注册。
- 角色只有 `admin` 与 `member`；成员只能访问自己创建的项目与产物，管理员可访问全量数据和服务端 AI 配置。
- 扩展安装包在系统网页下载。用户手动通过 Chrome 开发者模式加载后，以一次性绑定令牌关联到当前成员账号。
- 扩展主动建立认证 WebSocket，服务端通过该既有连接下发采集命令。服务端不主动连接用户电脑。
- 扩展只访问允许的京东商品页面内容和媒体；回传结构化快照或允许的媒体数据，绝不回传 Cookie、local storage、订单或账户资料。

## 5. 领域模型

### 5.1 项目聚合

`Project` 是用户可见的根对象，关联输入链接、一个或多个 `CaptureSession`、已选 SKU、画幅、模板、候选方案和最终产物。

建议项目状态：

```text
draft
-> awaiting_extension
-> collecting
-> awaiting_sku_selection
-> awaiting_template_selection
-> generating_candidates
-> awaiting_plan_selection
-> synthesizing_audio
-> rendering
-> completed

任一阶段可进入 failed 或 cancelled。
```

等待用户操作或等待扩展连接只是项目状态，绝不领取或占用运行中的 `Job`。

### 5.2 采集事实

- `CaptureSession` 绑定一个 Project、一个扩展连接和一次采集尝试。
- `ProductSnapshot` 为不可变记录，保存来源链接、标准商品链接、采集时间、可售 SKU、规格字段、包装清单、图片 URL／已下载素材引用和字段来源。
- SKU 仅在项目所属商品快照中有意义。多链接项目默认选择每个链接的默认可售 SKU；单链接项目默认选择该链接的全部可售 SKU；用户可调整。

### 5.3 候选方案与卡片

- 用户先选择 `16:9` 或 `9:16`，再选择三套视觉模板之一。
- Go API 用同一份已选事实分别发起三次独立 LLM 调用；三套提示词不同，候选方案不得在一次 LLM 调用中合并生成。
- `CandidatePlan` 是一套完整、可比较的卡片集合。
- `Card` 绑定一个 SKU，含视觉字段、口播文本、`source_refs` 和顺序。一个 SKU 可有多张卡。
- 用户可编辑口播、删除卡片、调整顺序；用户不能拖拽改版或写入无来源的规格。
- 每个事实类字段必须引用 ProductSnapshot。价格必须附采集时间；不允许将营销文案扩写成未经证实的功效或比较结论。

## 6. LLM、TTS 与成片

### 6.1 候选方案生成

- 使用硅基流动 Anthropic Messages 兼容接口。
- 管理员在后台配置模型、温度、最大输出等；普通成员不能选择模型。
- 每次调用强制返回 `submit_candidate_plan` 工具输入，其 `input_schema` 定义计划 JSON；Go API 在持久化前校验 JSON Schema、SKU 引用和 `source_refs`。
- LLM 调用只设置首个有效流式事件超时，不设置完整生成总时长超时。调用失败、流中断或 Schema 校验失败时不自动重试，保留 trace ID 并显示单独“重新生成”操作。
- 三套中任一失败不得阻塞其余成功方案展示。

### 6.2 音频、时间轴与封面

- 用户选中一套候选方案并编辑后，卡片逐张发送至 TTS。
- 默认 Edge TTS；用户可明确改用 MiniMax。任何付费 AI 调用（LLM、MiniMax、Seedance）不得自动重试，必须使用幂等键并提供人工重试。
- 读取每段已生成音频的真实时长，作为对应 Card 的播放时长。卡片间使用固定短转场。
- 第一版不生成字幕或逐字高亮。
- 封面为独立 JPG/PNG：可选 Seedance，也可由产品主图、标题与核心卖点自动排版；非 AI 封面不得只原样使用主图。

### 6.3 渲染

- Node Renderer 使用 HyperFrames 生成动态 HTML 卡片，FFmpeg 输出 H.264 MP4。
- 三套模板都必须有横版 `1920×1080` 与竖版 `1080×1920` 响应式定义；一次项目只渲染用户选定的一种画幅。
- 产出为一个按已选 SKU 和卡片顺序连续播放的合集 MP4，不自动拆分为单 SKU 视频。

## 7. 作业队列

### 7.1 事实源与领取规则

`jobs` 是 PostgreSQL 表，不是 Redis 队列。推荐字段：

| 字段 | 含义 |
| --- | --- |
| `id` | 作业 ID |
| `kind` | 作业类型 |
| `execution_lane` | 执行通道 |
| `project_id` | 所属项目 |
| `payload` | 仅含对象 ID 与执行参数 |
| `status` | `queued`、`leased`、`succeeded`、`failed`、`cancelled` |
| `available_at` | 最早可领取时间 |
| `lease_token`、`lease_until` | 领取身份与租约 |
| `attempt` | 尝试编号 |
| `idempotency_key` | 防止重复副作用 |
| `error_code`、`error_detail` | 可展示和审计的失败信息 |

Go Job Worker 必须以短事务、行锁和租约原子领取作业。Worker 运行时定期续租；崩溃、失联或 NAS 重启后，过期租约可重新领取。Worker 只能通过 `payload` 中的引用读取事实源，绝不把大对象或密钥塞进 payload。Node Renderer 不领取作业：持有 `render` 租约的 Go 调度器调用其内部 API，并负责把结果和状态写回 PostgreSQL。

### 7.2 执行通道与调度

不使用严格全局 FIFO，而使用**每个执行通道 FIFO**：

| 通道 | 典型作业 | 并发规则 |
| --- | --- | --- |
| `capture` | 向扩展派发、素材回收 | 每个扩展单并发 |
| `planning` | 三套候选 LLM 调用 | 同一项目可并行三次 |
| `media` | 下载安全素材、TTS、封面 | 有限并发 |
| `render` | Go 调度器调用 HyperFrames + FFmpeg | 全局单并发 |

同一通道按进入时间 FIFO。手动重试创建新的尝试，排到该通道队尾；管理员可取消尚未领取的作业。

### 7.3 重试规则

- 安全、无计费副作用的下载和素材抓取：最多自动重试两次。
- LLM、MiniMax TTS、Seedance 等付费 AI 调用：不自动重试；以幂等键、首包／首 token 超时、可审计失败和手动重试处理。
- 渲染失败保留中间产物和错误，允许从失败节点重新创建渲染作业，不重新采集。

## 8. Redis 与缓存

Redis 是可清空的辅助层，只保存：

- 扩展心跳和短期在线连接映射；
- WebSocket／SSE 项目进度广播；
- 登录与 API 限流计数；
- 经验证可重建的短期数据。

第一版不得使用 Redis 缓存项目列表、SKU、候选方案、权限或产物元数据。Redis 故障或重启后，系统从 PostgreSQL 恢复任务和页面状态。

## 9. 持久化与保留期

| 数据 | 存放位置 | 默认保留 |
| --- | --- | --- |
| 项目、状态、快照元数据、方案 | PostgreSQL | 随项目保留 |
| 原始采集资料与素材 | NAS 挂载存储 | 30 天 |
| 最终 MP4 与封面 | NAS 挂载存储 | 90 天 |

管理员可将产物标记为永久保留。清理作业只能删除到期且未永久保留的 Artifact，并记录清理审计事件。

## 10. 最小 API 面

API 的具体字段由 OpenAPI 定义，第一版至少包含：

```text
POST   /api/projects
GET    /api/projects
GET    /api/projects/{projectId}
POST   /api/projects/{projectId}/capture-sessions
POST   /api/projects/{projectId}/sku-selection
POST   /api/projects/{projectId}/candidate-plans
GET    /api/projects/{projectId}/candidate-plans
PATCH  /api/projects/{projectId}/candidate-plans/{planId}/cards
POST   /api/projects/{projectId}/produce
POST   /api/jobs/{jobId}/retry
POST   /api/extension-bindings
GET    /api/artifacts/{artifactId}/download
```

扩展 WebSocket 是扩展主动连接的独立认证通道，用于心跳、绑定确认、采集命令、进度和结构化采集回传；不得接受任意远程脚本或未经绑定的命令。

## 11. 验收条件

- 一个单链接项目能自动获得全部可售 SKU；一个多链接项目能获得各链接默认 SKU，且用户均可调整。
- 三套候选方案来自三次可审计的独立 LLM 调用，每张卡片均通过事实来源校验。
- 编辑口播后，卡片时长来自该卡真实 TTS 音频而非预设秒数。
- 扩展、Redis、Worker 或 NAS 重启后，过期租约可恢复，已完成阶段与产物不丢失。
- 渲染期间第二个项目能显示排队状态，且不会并行启动第二个渲染进程。
- 公网只能访问 HTTPS 网页和 `/api/*`；不可直接访问数据库、Redis、NAS 管理后台或 Node Renderer。
