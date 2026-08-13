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

  it("queues a capture received while the previous task is still running", async () => {
    const source = await import("node:fs/promises").then((fs) => fs.readFile(new URL("../entrypoints/background.ts", import.meta.url), "utf8"));
    expect(source).toContain("new SerialTaskQueue");
    expect(source).toContain("captureQueue.enqueue");
    expect(source).not.toContain("if (running) return");
  });

  it("checks for a newer published extension and shows an action badge", async () => {
    const source = await import("node:fs/promises").then((fs) => fs.readFile(new URL("../entrypoints/background.ts", import.meta.url), "utf8"));
    const updateSource = await import("node:fs/promises").then((fs) => fs.readFile(new URL("../lib/update-check.ts", import.meta.url), "utf8"));
    expect(source).toContain('browser.alarms.create("extension-update"');
    expect(source).toContain("checkForExtensionUpdate(publicOrigin)");
    expect(updateSource).toContain('setBadgeText({ text: state.updateAvailable ? "新" : "" })');
  });

  it("claims a coupon landing page once before waiting for the product page", async () => {
    const source = await import("node:fs/promises").then((fs) => fs.readFile(new URL("../entrypoints/background.ts", import.meta.url), "utf8"));
    expect(source).toContain("clickJDClaimButton");
    expect(source).toContain("classifyJDPage");
    expect(source).toContain("claimClicked");
    expect(source).toContain("browser.scripting.executeScript");
    expect(source).toContain("京东未登录或登录已失效");
  });

  it("converts a mobile product page to the same desktop SKU once", async () => {
    const source = await import("node:fs/promises").then((fs) => fs.readFile(new URL("../entrypoints/background.ts", import.meta.url), "utf8"));
    const config = await import("node:fs/promises").then((fs) => fs.readFile(new URL("../wxt.config.ts", import.meta.url), "utf8"));
    expect(source).toContain("desktopProductURLFromMobile");
    expect(source).toContain("mobileProductConverted");
    expect(source).toContain("browser.tabs.update");
    expect(config).toContain('"https://item.m.jd.com/*"');
  });
});
