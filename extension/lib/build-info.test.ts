import { describe, expect, it } from "vitest";
import { formatBuildTime, isNewerExtensionBuild } from "./build-info";

describe("extension build information", () => {
  const current = { version: "0.1.0", build_id: "old", build_time: "2026-08-13T01:00:00.000Z" };

  it("detects a later published build", () => {
    expect(isNewerExtensionBuild(current, { ...current, build_id: "new", build_time: "2026-08-13T02:00:00.000Z" })).toBe(true);
  });

  it("does not suggest an older or identical build", () => {
    expect(isNewerExtensionBuild(current, current)).toBe(false);
    expect(isNewerExtensionBuild(current, { ...current, build_id: "older", build_time: "2026-08-12T02:00:00.000Z" })).toBe(false);
  });

  it("formats a valid build time for display", () => {
    expect(formatBuildTime(current.build_time)).toMatch(/2026/);
  });
});
