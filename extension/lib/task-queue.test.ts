import { describe, expect, it } from "vitest";
import { SerialTaskQueue } from "./task-queue";

describe("SerialTaskQueue", () => {
  it("keeps a task received while another capture is running", async () => {
    const started: string[] = [];
    let finishFirst!: () => void;
    const firstFinished = new Promise<void>((resolve) => { finishFirst = resolve; });
    const queue = new SerialTaskQueue<{ taskId: string }>(
      (task) => task.taskId,
      async (task) => {
        started.push(task.taskId);
        if (task.taskId === "1") await firstFinished;
      },
    );

    queue.enqueue({ taskId: "1" });
    queue.enqueue({ taskId: "2" });
    await Promise.resolve();
    expect(started).toEqual(["1"]);

    finishFirst();
    await queue.whenIdle();
    expect(started).toEqual(["1", "2"]);
  });

  it("ignores a duplicate task that is active or already queued", async () => {
    const started: string[] = [];
    const queue = new SerialTaskQueue<{ taskId: string }>((task) => task.taskId, async (task) => { started.push(task.taskId); });
    queue.enqueue({ taskId: "1" });
    queue.enqueue({ taskId: "1" });
    await queue.whenIdle();
    expect(started).toEqual(["1"]);
  });
});
