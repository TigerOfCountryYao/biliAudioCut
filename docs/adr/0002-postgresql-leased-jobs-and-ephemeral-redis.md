# PostgreSQL 租约作业队列与短生命周期 Redis

已接受。PostgreSQL 是项目、作业状态和结果元数据的唯一事实源；作业以表记录、行锁和租约领取实现。Redis 仅用于可丢失的心跳、WebSocket 在线映射、进度广播和限流，不存放唯一任务状态或业务查询缓存。

## Considered Options

- RabbitMQ、NATS、Redis Streams：适合更高吞吐或跨机器规模化，但会增加第一版的部署、跨语言消费与一致性成本。
- BullMQ：受 Node 生态约束，不适合作为 Go 与 Node 的共同任务边界。

## Consequences

Go Job Worker 崩溃后，租约到期的作业必须可重新领取。所有作业只从 `jobs` 引用业务对象，不能把图片、完整快照或密钥复制进队列；Node Renderer 由持有渲染作业租约的 Go 调度器调用，不直接领取数据库作业。
