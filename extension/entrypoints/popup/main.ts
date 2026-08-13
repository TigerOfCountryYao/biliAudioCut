import { browser } from "wxt/browser";
import { extensionBuild, formatBuildTime } from "../../lib/build-info";
import { checkForExtensionUpdate } from "../../lib/update-check";
import "./style.css";

const apiOrigin = import.meta.env.WXT_API_ORIGIN ?? "http://localhost:8080";
const publicOrigin = import.meta.env.WXT_PUBLIC_ORIGIN ?? "http://localhost:3000";
const root = document.querySelector<HTMLDivElement>("#app")!;

type ConnectionStatus = "authorization_required" | "connecting" | "connected" | "disconnected";
type Stored = { token?: string; connectionStatus?: ConnectionStatus; updateBuildID?: string; updateAvailable?: boolean; latestBuild?: typeof extensionBuild & { download_url?: string } };

async function authorize() {
  const verifier = crypto.getRandomValues(new Uint8Array(32));
  const verifierText = btoa(String.fromCharCode(...verifier)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=/g, "");
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifierText));
  const challenge = btoa(String.fromCharCode(...new Uint8Array(digest))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=/g, "");
  const redirect = browser.identity.getRedirectURL();
  const result = await browser.identity.launchWebAuthFlow({ interactive: true, url: `${apiOrigin}/api/extension/authorize?code_challenge=${encodeURIComponent(challenge)}&redirect_uri=${encodeURIComponent(redirect)}` });
  if (!result) throw new Error("未收到授权结果");
  const code = new URL(result).searchParams.get("code");
  if (!code) throw new Error("授权失败");
  const response = await fetch(`${apiOrigin}/api/extension/token`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ code, code_verifier: verifierText, device_name: navigator.userAgent.slice(0, 100) }) });
  if (!response.ok) throw new Error("设备令牌获取失败");
  const { access_token } = await response.json();
  await browser.storage.local.set({ token: access_token, connectionStatus: "connecting" });
  await browser.runtime.sendMessage({ type: "reconnect" });
}

async function disconnect() {
  const { token } = await browser.storage.local.get("token");
  if (token) await fetch(`${apiOrigin}/api/extension/device`, { method: "DELETE", headers: { Authorization: `Bearer ${token}` } });
  await browser.storage.local.remove("token");
  await browser.storage.local.set({ connectionStatus: "authorization_required" });
}

function statusText(status: ConnectionStatus | undefined) {
  if (status === "connected") return "扩展已连接（服务器已确认）。提交项目后会在后台自动采集。";
  if (status === "connecting") return "正在连接采集服务，请稍候。";
  if (status === "disconnected") return "与采集服务暂时断开，正在自动重连。";
  return "登录工作台并授权此 Chrome 扩展。";
}

async function render() {
  const { token, connectionStatus, updateBuildID, updateAvailable, latestBuild } = await browser.storage.local.get() as Stored;
  root.innerHTML = token
    ? `<h1>京东商品采集</h1><p>${statusText(connectionStatus)}</p><button id="off">断开此设备</button><div id="update"></div><p class="build"></p>`
    : `<h1>京东商品采集</h1><p>${statusText("authorization_required")}</p><button id="on">网页登录授权</button><div id="update"></div><p class="build"></p>`;
  const build = document.querySelector<HTMLElement>(".build");
  if (build) build.textContent = `版本 ${extensionBuild.version} · 构建于 ${formatBuildTime(extensionBuild.build_time)}`;
  const update = document.querySelector<HTMLDivElement>("#update");
  if (updateBuildID === extensionBuild.build_id && updateAvailable && latestBuild && update) {
    const message = document.createElement("p");
    message.className = "update-message";
    message.textContent = `发现新版：${formatBuildTime(latestBuild.build_time)}`;
    const download = document.createElement("a");
    download.className = "download";
    download.href = new URL("/downloads/jd-product-capture-extension.zip", publicOrigin).href;
    download.target = "_blank";
    download.textContent = "下载新版";
    update.append(message, download);
  }
  document.querySelector("#on")?.addEventListener("click", () => void authorize().catch((error) => alert(String(error))));
  document.querySelector("#off")?.addEventListener("click", () => void disconnect());
}

browser.storage.local.onChanged.addListener(() => void render());
void render();
void checkForExtensionUpdate(publicOrigin).catch(() => undefined);
