import { browser } from "wxt/browser";
import { ExtensionBuildInfo, extensionBuild, isNewerExtensionBuild } from "./build-info";

export type ExtensionUpdateState = {
  updateBuildID: string;
  updateAvailable: boolean;
  latestBuild?: ExtensionBuildInfo;
  updateCheckedAt: string;
};

export async function checkForExtensionUpdate(publicOrigin: string): Promise<ExtensionUpdateState> {
  const metadataURL = new URL("/downloads/jd-product-capture-extension.json", publicOrigin);
  const response = await fetch(metadataURL, { cache: "no-store" });
  if (!response.ok) throw new Error(`update metadata HTTP ${response.status}`);
  const latestBuild = await response.json() as ExtensionBuildInfo;
  const state = {
    updateBuildID: extensionBuild.build_id,
    updateAvailable: isNewerExtensionBuild(extensionBuild, latestBuild),
    latestBuild,
    updateCheckedAt: new Date().toISOString(),
  };
  await browser.storage.local.set(state);
  await browser.action.setBadgeBackgroundColor({ color: "#d92d20" });
  await browser.action.setBadgeText({ text: state.updateAvailable ? "新" : "" });
  return state;
}
