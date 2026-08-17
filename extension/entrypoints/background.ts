import { browser } from "wxt/browser";
import { collectProductVariants } from "../lib/capture";
import { SerialTaskQueue } from "../lib/task-queue";
import { checkForExtensionUpdate } from "../lib/update-check";
import { classifyJDPage, clickJDClaimButton, desktopProductURLFromMobile } from "../lib/product-page-navigation";

const apiOrigin = import.meta.env.WXT_API_ORIGIN ?? "http://localhost:8080";
const publicOrigin = import.meta.env.WXT_PUBLIC_ORIGIN ?? "http://localhost:3000";

type ConnectionStatus = "authorization_required" | "connecting" | "connected" | "disconnected";
type Stored = { token?: string; connectionStatus?: ConnectionStatus };

let socket: WebSocket | undefined;
const captureResultUploadAttempts = 3;
type CaptureTask = { taskId: string; sourceUrl: string; captureAllSKUs: boolean };
const captureQueue = new SerialTaskQueue<CaptureTask>((task) => task.taskId, runCapture);

const getStored = () => browser.storage.local.get() as Promise<Stored>;
const setConnectionStatus = (connectionStatus: ConnectionStatus) => browser.storage.local.set({ connectionStatus });

async function requireAuthorization(expectedSocket?: WebSocket) {
  if (expectedSocket && socket !== expectedSocket) return;
  const activeSocket = expectedSocket ?? socket;
  if (socket === activeSocket) socket = undefined;
  await browser.storage.local.remove("token");
  await setConnectionStatus("authorization_required");
  activeSocket?.close();
}

const api = async (path: string, options: RequestInit = {}) => {
  const { token } = await getStored();
  const response = await fetch(`${apiOrigin}/api${path}`, {
    ...options,
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token ?? ""}`, ...options.headers },
  });
  if (response.status === 401) await requireAuthorization();
  return response;
};

async function connect() {
  const { token } = await getStored();
  if (!token) {
    await setConnectionStatus("authorization_required");
    return;
  }

  await setConnectionStatus("connecting");
  const wsOrigin = apiOrigin.replace(/^http/, "ws");
  const nextSocket = new WebSocket(`${wsOrigin}/api/extension/ws`);
  const previousSocket = socket;
  socket = nextSocket;
  previousSocket?.close();

  nextSocket.onopen = () => nextSocket.send(JSON.stringify({ type: "authenticate", token }));
  nextSocket.onmessage = (event) => {
    const message = JSON.parse(event.data);
    if (message.type === "authenticated") void setConnectionStatus("connected");
    if (message.type === "error" && message.code === "unauthorized") {
      void requireAuthorization(nextSocket);
    }
    if (message.type === "capture") {
      captureQueue.enqueue({ taskId: message.task_id, sourceUrl: message.source_url, captureAllSKUs: message.capture_all_skus === true });
    }
  };
  nextSocket.onclose = () => {
    if (socket !== nextSocket) return;
    socket = undefined;
    void setConnectionStatus("disconnected");
    setTimeout(() => { if (!socket) void connect(); }, 3000);
  };
}

async function runCapture({ taskId, sourceUrl, captureAllSKUs }: CaptureTask) {
  let tabId: number | undefined;
  try {
    const tab = await browser.tabs.create({ url: sourceUrl, active: false });
    tabId = tab.id;
    await waitForProductPage(tabId);
    const result = await browser.scripting.executeScript({ target: { tabId }, func: collectProductVariants, args: [sourceUrl, captureAllSKUs] });
    const capture = result[0]?.result;
    if (!capture) throw new Error("no capture returned");
    await uploadCaptureResult(taskId, capture);
  } catch (error) {
    const detail = await captureFailureDetail(error, tabId);
    const code = detail.startsWith("采集结果回传失败") ? "capture_result_upload_failed" : "capture_failed";
    await api("/extension/capture-failures", { method: "POST", body: JSON.stringify({ task_id: taskId, code, detail }) });
  } finally {
    if (tabId !== undefined) await browser.tabs.remove(tabId).catch(() => undefined);
    socket?.send(JSON.stringify({ type: "heartbeat" }));
  }
}

async function captureFailureDetail(error: unknown, tabId?: number) {
  const generic = error instanceof Error ? error.message : String(error);
  if (tabId === undefined) return generic;

  const tab = await browser.tabs.get(tabId).catch(() => undefined);
  if (classifyJDPage(tab?.url ?? "") === "rate_limited") {
    return "京东已触发访问频率限制（403），请稍后重试或先在当前 Chrome 手动访问京东商品页；系统不会绕过验证";
  }
  return generic;
}

async function uploadCaptureResult(taskId: string, capture: unknown) {
  let lastError: unknown;
  for (let attempt = 1; attempt <= captureResultUploadAttempts; attempt += 1) {
    try {
      const response = await api("/extension/capture-results", { method: "POST", body: JSON.stringify({ task_id: taskId, capture }) });
      if (response.ok) return;
      throw new Error(`HTTP ${response.status}: ${await response.text()}`);
    } catch (error) {
      lastError = error;
      if (attempt < captureResultUploadAttempts) {
        await new Promise((resolve) => setTimeout(resolve, 500 * 2 ** (attempt - 1)));
      }
    }
  }
  const message = lastError instanceof Error ? lastError.message : String(lastError);
  throw new Error(`采集结果回传失败，已自动重试 ${captureResultUploadAttempts} 次：${message}`);
}

async function waitForProductPage(tabId: number) {
  let claimClicked = false;
  let sawCouponLanding = false;
  let mobileProductConverted = false;
  for (let i = 0; i < 80; i += 1) {
    const tab = await browser.tabs.get(tabId);
    const pageKind = classifyJDPage(tab.url ?? "");
    if (pageKind === "product") {
      await new Promise((resolve) => setTimeout(resolve, 1500));
      const confirmedTab = await browser.tabs.get(tabId);
      const confirmedPageKind = classifyJDPage(confirmedTab.url ?? "");
      if (confirmedPageKind === "product") return;
      continue;
    }
    if (pageKind === "login") {
      throw new Error("京东未登录或登录已失效，请先在当前 Chrome 登录京东后重新采集");
    }
    if (pageKind === "rate_limited") {
      throw new Error("京东已触发访问频率限制（403），请稍后重试或先在当前 Chrome 手动访问京东商品页；系统不会绕过验证");
    }
    if (pageKind === "mobile_product" && !mobileProductConverted) {
      const desktopURL = desktopProductURLFromMobile(tab.url ?? "");
      if (!desktopURL) {
        throw new Error("京东手机商品页缺少有效的数字 SKU，无法转换到桌面商品页");
      }
      mobileProductConverted = true;
      await browser.tabs.update(tabId, { url: desktopURL });
    }
    if (pageKind === "coupon") {
      sawCouponLanding = true;
      if (!claimClicked) {
        const result = await browser.scripting.executeScript({ target: { tabId }, func: clickJDClaimButton });
        const claimResult = result[0]?.result;
        if (claimResult?.status === "ambiguous") {
          throw new Error("领券活动页出现多个“一键领取”按钮，已停止自动操作");
        }
        if (claimResult?.status === "clicked") {
          claimClicked = true;
        }
      }
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }

  if (mobileProductConverted) {
    throw new Error("已将京东手机商品页转换为同 SKU 桌面页，但桌面商品页加载超时");
  }
  if (sawCouponLanding && claimClicked) {
    throw new Error("已点击“一键领取”，但活动页未跳转到京东商品页");
  }
  if (sawCouponLanding) {
    throw new Error("领券活动页未找到唯一可见的“一键领取”按钮");
  }
  throw new Error("商品页跳转超时");
}

browser.runtime.onInstalled.addListener(() => void connect());
browser.runtime.onStartup.addListener(() => void connect());
browser.runtime.onMessage.addListener((message) => { if (message?.type === "reconnect") void connect(); });
browser.alarms.create("heartbeat", { periodInMinutes: 1 });
browser.alarms.create("extension-update", { periodInMinutes: 60 });
browser.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === "heartbeat") {
    if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "heartbeat" }));
    else void connect();
  }
  if (alarm.name === "extension-update") {
    void checkForExtensionUpdate(publicOrigin).catch(() => undefined);
  }
});

export default defineBackground(() => {
  void connect();
  void checkForExtensionUpdate(publicOrigin).catch(() => undefined);
});
