import { browser } from "wxt/browser";
import { collectProductVariants } from "../lib/capture";

const apiOrigin = import.meta.env.WXT_API_ORIGIN ?? "http://localhost:8080";

type ConnectionStatus = "authorization_required" | "connecting" | "connected" | "disconnected";
type Stored = { token?: string; connectionStatus?: ConnectionStatus };

let socket: WebSocket | undefined;
let running = false;

const getStored = () => browser.storage.local.get() as Promise<Stored>;
const setConnectionStatus = (connectionStatus: ConnectionStatus) => browser.storage.local.set({ connectionStatus });

const api = async (path: string, options: RequestInit = {}) => {
  const { token } = await getStored();
  return fetch(`${apiOrigin}/api${path}`, {
    ...options,
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token ?? ""}`, ...options.headers },
  });
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
      void browser.storage.local.remove("token");
      void setConnectionStatus("authorization_required");
      nextSocket.close();
    }
    if (message.type === "capture") void capture(message.task_id, message.source_url);
  };
  nextSocket.onclose = () => {
    if (socket !== nextSocket) return;
    socket = undefined;
    void setConnectionStatus("disconnected");
    setTimeout(() => { if (!socket) void connect(); }, 3000);
  };
}

async function capture(taskId: string, sourceUrl: string) {
  if (running) return;
  running = true;
  let tabId: number | undefined;
  try {
    const tab = await browser.tabs.create({ url: sourceUrl, active: false });
    tabId = tab.id;
    await waitForProductPage(tabId);
    const result = await browser.scripting.executeScript({ target: { tabId }, func: collectProductVariants, args: [sourceUrl] });
    const capture = result[0]?.result;
    if (!capture) throw new Error("no capture returned");
    const response = await api("/extension/capture-results", { method: "POST", body: JSON.stringify({ task_id: taskId, capture }) });
    if (!response.ok) throw new Error(await response.text());
  } catch (error) {
    await api("/extension/capture-failures", { method: "POST", body: JSON.stringify({ task_id: taskId, code: "capture_failed", detail: error instanceof Error ? error.message : String(error) }) });
  } finally {
    if (tabId !== undefined) await browser.tabs.remove(tabId).catch(() => undefined);
    running = false;
    socket?.send(JSON.stringify({ type: "heartbeat" }));
  }
}

async function waitForProductPage(tabId: number) {
  for (let i = 0; i < 40; i += 1) {
    const tab = await browser.tabs.get(tabId);
    if (/^https:\/\/item\.jd\.com\/\d+\.html/.test(tab.url ?? "")) {
      await new Promise((resolve) => setTimeout(resolve, 1500));
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error("商品页跳转超时");
}

browser.runtime.onInstalled.addListener(() => void connect());
browser.runtime.onStartup.addListener(() => void connect());
browser.runtime.onMessage.addListener((message) => { if (message?.type === "reconnect") void connect(); });
browser.alarms.create("heartbeat", { periodInMinutes: 1 });
browser.alarms.onAlarm.addListener(() => {
  if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "heartbeat" }));
  else void connect();
});

export default defineBackground(() => { void connect(); });
