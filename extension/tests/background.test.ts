import { describe, expect, it } from "vitest";

describe("extension WebSocket lifecycle", () => {
  it("reconnects only when the socket that closed is still current", async () => {
    const source = await import("node:fs/promises").then((fs) => fs.readFile(new URL("../entrypoints/background.ts", import.meta.url), "utf8"));
    expect(source).toContain("if(socket===nextSocket)");
    expect(source).toContain("const previousSocket=socket;socket=nextSocket;previousSocket?.close()");
  });
});
