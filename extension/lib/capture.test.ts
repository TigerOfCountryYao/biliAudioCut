import { describe, expect, it } from "vitest";

describe("capture module", () => {
  it("does not export browser state or storage helpers", async () => {
    const source = await import("node:fs/promises").then((fs) => fs.readFile(new URL("./capture.ts", import.meta.url), "utf8"));
    expect(source).not.toContain("document.cookie");
    expect(source).not.toContain("localStorage");
    expect(source).not.toContain("sessionStorage");
  });
});
