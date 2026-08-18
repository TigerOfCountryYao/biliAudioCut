import { describe, expect, it } from "vitest";
import { formatBuildTime, isNewerExtensionBuild } from "./build-info";

describe("extension build information", () => {
  const current = { version: "0.1.0", build_id: "0.1.0", build_time: "2026-08-13T01:00:00.000Z" };

  it("detects a later published semantic version", () => {
    expect(isNewerExtensionBuild(current, { ...current, version: "0.2.0", build_id: "0.2.0", build_time: "2026-08-13T02:00:00.000Z" })).toBe(true);
  });

  it("does not suggest an older or identical semantic version, even if it was rebuilt later", () => {
    expect(isNewerExtensionBuild(current, current)).toBe(false);
    expect(isNewerExtensionBuild(current, { ...current, build_time: "2026-08-14T02:00:00.000Z" })).toBe(false);
    expect(isNewerExtensionBuild(current, { ...current, version: "0.0.9", build_id: "0.0.9" })).toBe(false);
  });

  it("formats a valid build time for display", () => {
    expect(formatBuildTime(current.build_time)).toMatch(/2026/);
  });
});
