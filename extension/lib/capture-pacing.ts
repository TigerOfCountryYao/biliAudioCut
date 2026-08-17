export const minimumCaptureCooldownMilliseconds = 8_000;
export const maximumCaptureCooldownMilliseconds = 18_000;

export function nextCaptureCooldownMilliseconds(random = Math.random) {
  return Math.floor(
    minimumCaptureCooldownMilliseconds
      + random() * (maximumCaptureCooldownMilliseconds - minimumCaptureCooldownMilliseconds + 1),
  );
}
