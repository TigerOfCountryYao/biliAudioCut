import currentBuild from "../.generated/build-info.json";

export type ExtensionBuildInfo = {
  version: string;
  build_id: string;
  build_time: string;
  download_url?: string;
};

export const extensionBuild = currentBuild satisfies ExtensionBuildInfo;

export function isNewerExtensionBuild(current: ExtensionBuildInfo, latest: ExtensionBuildInfo) {
  if (!latest.build_id || latest.build_id === current.build_id) return false;
  const currentTime = Date.parse(current.build_time);
  const latestTime = Date.parse(latest.build_time);
  return Number.isFinite(currentTime) && Number.isFinite(latestTime) && latestTime > currentTime;
}

export function formatBuildTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}
