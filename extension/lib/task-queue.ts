export class SerialTaskQueue<T> {
  private readonly queued: T[] = [];
  private readonly knownKeys = new Set<string>();
  private draining?: Promise<void>;

  constructor(
    private readonly keyOf: (task: T) => string,
    private readonly run: (task: T) => Promise<void>,
  ) {}

  enqueue(task: T) {
    const key = this.keyOf(task);
    if (this.knownKeys.has(key)) return;
    this.knownKeys.add(key);
    this.queued.push(task);
    if (!this.draining) this.draining = this.drain();
  }

  async whenIdle() {
    await this.draining;
  }

  private async drain() {
    try {
      while (this.queued.length > 0) {
        const task = this.queued.shift()!;
        try {
          await this.run(task);
        } finally {
          this.knownKeys.delete(this.keyOf(task));
        }
      }
    } finally {
      this.draining = undefined;
      if (this.queued.length > 0) this.draining = this.drain();
    }
  }
}
