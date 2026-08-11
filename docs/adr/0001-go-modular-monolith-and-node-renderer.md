# Go 模块化单体与 Node 渲染服务

已接受。核心 API、身份与权限、项目状态机、采集结果校验和 AI 编排由 Go 模块化单体承担；HyperFrames 与 FFmpeg 由仅在 Docker 内网暴露的 Node 渲染服务承担。这样保留 Go 作为主要学习与业务语言，同时避免把浏览器视频渲染生态强行迁移出 Node。

## Considered Options

- 全 TypeScript：可共享类型，但牺牲以 Go 实现核心后端的目标。
- 全 Go：会为 HyperFrames 和浏览器渲染增加不必要的桥接成本。

## Consequences

跨语言边界只使用 REST JSON 与 OpenAPI。不得让 Next.js、扩展或 Node 渲染服务绕过 Go 直接读写业务数据库；Go 后台 Worker 持有作业租约，并将 Node Renderer 作为内网执行引擎调用。
