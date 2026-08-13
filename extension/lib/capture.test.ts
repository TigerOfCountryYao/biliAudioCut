import { describe, expect, it } from "vitest";

describe("capture module", () => {
  it("does not export browser state or storage helpers", async () => {
    const source = await import("node:fs/promises").then((fs) => fs.readFile(new URL("./capture.ts", import.meta.url), "utf8"));
    expect(source).not.toContain("document.cookie");
    expect(source).not.toContain("localStorage");
    expect(source).not.toContain("sessionStorage");
  });

  it("supports JD's current direct SKU nodes and unavailable marker", async () => {
    const source = await import("node:fs/promises").then((fs) => fs.readFile(new URL("./capture.ts", import.meta.url), "utf8"));
    expect(source).toContain('".specification-item-sku"');
    expect(source).toContain('".specification-series-item"');
    expect(source).toContain('"specification-item-sku--lack"');
    expect(source).toContain('series_label');
    expect(source).not.toContain('seenSKUs');
    expect(source).toContain('series_ordinal');
    expect(source).toContain('variant_label');
    expect(source).not.toContain('"detail"');
    expect(source).not.toContain('"main"');
    expect(source).toContain('dismissSimilarProductDialog');
  });

  it("captures only the link's current SKU unless full enumeration is requested", async () => {
	const source = await import("node:fs/promises").then((fs) => fs.readFile(new URL("./capture.ts", import.meta.url), "utf8"));
	expect(source).toContain("captureAllSKUs = false");
	expect(source).toContain("if (!captureAllSKUs)");
	expect(source).toContain("products: [product]");
  });
});
