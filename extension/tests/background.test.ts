import { describe, expect, it } from "vitest";

describe("extension WebSocket lifecycle", () => {
  it("reconnects only when the socket that closed is still current", async () => {
    const source = await import("node:fs/promises").then((fs) => fs.readFile(new URL("../entrypoints/background.ts", import.meta.url), "utf8"));
    expect(source).toContain("if (socket !== nextSocket) return;");
    expect(source).toContain("const previousSocket = socket;");
    expect(source).toContain("previousSocket?.close();");
  });

  it("marks the extension connected only after the server authenticates the socket", async () => {
    const source = await import("node:fs/promises").then((fs) => fs.readFile(new URL("../entrypoints/background.ts", import.meta.url), "utf8"));
    expect(source).toContain('message.type === "authenticated"');
    expect(source).toContain('setConnectionStatus("connected")');
    expect(source).toContain('setConnectionStatus("authorization_required")');
  });

  it("retries transient capture-result uploads before reporting a capture failure", async () => {
    const source = await import("node:fs/promises").then((fs) => fs.readFile(new URL("../entrypoints/background.ts", import.meta.url), "utf8"));
    expect(source).toContain("const captureResultUploadAttempts = 3");
    expect(source).toContain("async function uploadCaptureResult");
    expect(source).toContain("采集结果回传失败，已自动重试");
    expect(source).toContain('code = detail.startsWith("采集结果回传失败") ? "capture_result_upload_failed" : "capture_failed"');
  });
});
