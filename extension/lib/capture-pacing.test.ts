import { describe, expect, it } from "vitest";
import {
  maximumCaptureCooldownMilliseconds,
  minimumCaptureCooldownMilliseconds,
  nextCaptureCooldownMilliseconds,
} from "./capture-pacing";

describe("capture pacing", () => {
  it("chooses a cooldown within the configured random interval", () => {
    expect(nextCaptureCooldownMilliseconds(() => 0)).toBe(minimumCaptureCooldownMilliseconds);
    expect(nextCaptureCooldownMilliseconds(() => 0.999999)).toBe(maximumCaptureCooldownMilliseconds);
  });
});
