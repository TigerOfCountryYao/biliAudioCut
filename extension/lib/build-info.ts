import currentBuild from "../.generated/build-info.json";

export type ExtensionBuildInfo = {
  version: string;
  build_id: string;
  build_time: string;
  download_url?: string;
};

export const extensionBuild = currentBuild satisfies ExtensionBuildInfo;

export function isNewerExtensionBuild(current: ExtensionBuildInfo, latest: ExtensionBuildInfo) {
  return compareSemanticVersions(latest.version, current.version) > 0;
}

function compareSemanticVersions(left: string, right: string) {
  const leftParts = semanticVersionParts(left);
  const rightParts = semanticVersionParts(right);
  if (!leftParts || !rightParts) return 0;
  for (let index = 0; index < leftParts.length; index += 1) {
    if (leftParts[index] !== rightParts[index]) return leftParts[index] > rightParts[index] ? 1 : -1;
  }
  return 0;
}

function semanticVersionParts(value: string) {
  const match = value.trim().match(/^v?(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$/);
  return match ? match.slice(1, 4).map(Number) : undefined;
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
