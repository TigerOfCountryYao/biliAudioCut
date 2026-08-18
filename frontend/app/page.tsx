"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";
import { api, ExtensionDevice, Project, statusLabel, User } from "../lib/api";

const maxLinks = 20;
type PublishedExtension = { version: string; build_id: string; build_time: string; download_url?: string };

function isNewerExtensionBuild(currentBuildID: string, latestBuild: PublishedExtension) {
  const current = semanticVersionParts(currentBuildID);
  const latest = semanticVersionParts(latestBuild.version);
  if (!current || !latest) return false;
  for (let index = 0; index < latest.length; index += 1) {
    if (latest[index] !== current[index]) return latest[index] > current[index];
  }
  return false;
}

function semanticVersionParts(value: string) {
  const match = value.trim().match(/^v?(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$/);
  return match ? match.slice(1, 4).map(Number) : undefined;
}

async function latestExtensionBuild() {
  const response = await fetch("/downloads/jd-product-capture-extension.json", { cache: "no-store" });
  if (!response.ok) throw new Error("无法读取扩展版本信息");
  return await response.json() as PublishedExtension;
}

function createdAtLabel(value: string) {
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

function parseBatchLinks(value: string) {
  return value.split(/\r?\n/).map((link) => link.trim()).filter(Boolean);
}

export default function Home() {
  const [user, setUser] = useState<User>();
  const [items, setItems] = useState<Project[]>([]);
  const [extensionDevice, setExtensionDevice] = useState<ExtensionDevice>();
  const [latestExtension, setLatestExtension] = useState<PublishedExtension>();
  const [error, setError] = useState("");
  const [name, setName] = useState("");
  const [linkInput, setLinkInput] = useState("");
  const [links, setLinks] = useState<string[]>([]);
  const [batchOpen, setBatchOpen] = useState(false);
  const [batchInput, setBatchInput] = useState("");
  const [notice, setNotice] = useState("");
  const [captureAllSKUs, setCaptureAllSKUs] = useState(false);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  async function load() {
    try {
      setUser(await api.me());
      setItems(await api.projects());
    } catch {
      setUser(undefined);
    }
  }

  useEffect(() => { void load(); }, []);

  useEffect(() => {
    if (!user) {
      setExtensionDevice(undefined);
      setLatestExtension(undefined);
      return;
    }
    void Promise.all([api.extensionDevice(), latestExtensionBuild()])
      .then(([device, latest]) => {
        setExtensionDevice(device);
        setLatestExtension(latest);
      })
      .catch(() => undefined);
  }, [user]);

  useEffect(() => {
    if (!user) return;
    const interval = window.setInterval(() => {
      void api.projects().then(setItems).catch(() => undefined);
    }, 3000);
    return () => window.clearInterval(interval);
  }, [user]);

  async function login(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      await api.login(email, password);
      await load();
      setPassword("");
    } catch (cause) {
      setError(String(cause));
    }
  }

  function addLink(event: FormEvent) {
    event.preventDefault();
    const link = linkInput.trim();
    if (!link) return;
    if (links.length >= maxLinks) {
      setError(`一个项目最多添加 ${maxLinks} 条链接。`);
      return;
    }
    if (links.includes(link)) {
      setError("该链接已添加。");
      return;
    }
    setLinks((current) => [...current, link]);
    setLinkInput("");
    setError("");
    setNotice("");
  }

  function importBatchLinks() {
    const candidates = parseBatchLinks(batchInput);
    if (candidates.length === 0) {
      setError("请至少输入一条链接，每行一条。");
      return;
    }

    const seen = new Set(links);
    const additions: string[] = [];
    let duplicateCount = 0;
    for (const link of candidates) {
      if (seen.has(link)) {
        duplicateCount += 1;
        continue;
      }
      seen.add(link);
      additions.push(link);
    }
    if (additions.length === 0) {
      setError("批量输入的链接均已存在。");
      return;
    }
    if (links.length + additions.length > maxLinks) {
      setError(`导入后将有 ${links.length + additions.length} 条链接，一个项目最多添加 ${maxLinks} 条，请删减后重试。`);
      return;
    }

    setLinks((current) => [...current, ...additions]);
    setBatchInput("");
    setBatchOpen(false);
    setError("");
    setNotice(`已批量添加 ${additions.length} 条链接${duplicateCount > 0 ? `，忽略 ${duplicateCount} 条重复链接` : ""}。`);
  }

  async function create() {
    if (links.length === 0) {
      setError("请先添加至少一条京东商品链接。");
      return;
    }
    try {
      const project = await api.create(name, links, captureAllSKUs);
      location.href = `/projects/${project.id}`;
    } catch (cause) {
      setError(String(cause));
    }
  }

  async function deleteProject(project: Project) {
    const name = project.name || "这个项目";
    const confirmed = window.confirm(`确定删除“${name}”吗？\n\n删除后无法恢复，包括已采集的商品数据和 SKU 选择。`);
    if (!confirmed) return;
    try {
      await api.deleteProject(project.id);
      setItems((current) => current.filter((item) => item.id !== project.id));
    } catch (cause) {
      setError(String(cause));
    }
  }

  if (!user) {
    return <section className="card">
      <h1>商品采集工作台</h1>
      <p className="muted">使用内部账号登录。</p>
      <form onSubmit={login}>
        <input placeholder="邮箱" value={email} onChange={(event) => setEmail(event.target.value)} />
        <br /><br />
        <input type="password" placeholder="密码" value={password} onChange={(event) => setPassword(event.target.value)} />
        <br /><br />
        <button>登录</button>
        {error && <p className="error">{error}</p>}
      </form>
    </section>;
  }

  const extensionUpdateAvailable = Boolean(
    extensionDevice?.build_id
      && latestExtension
      && isNewerExtensionBuild(extensionDevice.build_id, latestExtension),
  );
  const extensionUpdateRequired = Boolean(extensionDevice?.bound && (!extensionDevice.build_id || extensionUpdateAvailable));

  return <>
    <div className="header">
      <div><h1>商品采集工作台</h1><p className="muted">{user.display_name} · {user.email}</p></div>
      <button className="secondary" onClick={async () => { await api.logout(); setUser(undefined); }}>退出</button>
    </div>
    <section className="card">
      <h2>新建采集项目</h2>
      <p className="muted">采集前请先在此 Chrome 浏览器手动登录 <a href="https://www.jd.com" target="_blank" rel="noreferrer">京东</a>，并保持 Chrome 扩展在线。扩展仅使用当前浏览器已有的京东登录态，不会读取或保存京东账号密码。</p>
      <input placeholder="项目名称（可选，默认使用首个商品标题）" value={name} onChange={(event) => setName(event.target.value)} />
      <form className="link-adder" onSubmit={addLink}>
        <input placeholder="粘贴一条京东商品链接" value={linkInput} onChange={(event) => setLinkInput(event.target.value)} />
        <button type="submit">添加链接</button>
        <button type="button" className="secondary" aria-expanded={batchOpen} aria-controls="batch-link-panel" onClick={() => {
          setBatchOpen((current) => !current);
          setError("");
          setNotice("");
        }}>{batchOpen ? "收起批量添加" : "批量添加"}</button>
      </form>
      {batchOpen && <div className="batch-link-panel" id="batch-link-panel">
        <label htmlFor="batch-links"><strong>批量添加链接</strong></label>
        <p className="muted">每行粘贴一条链接，导入时会自动忽略空行和重复链接。</p>
        <textarea id="batch-links" wrap="off" placeholder={"https://item.jd.com/100000000001.html\nhttps://u.jd.com/example"} value={batchInput} onChange={(event) => setBatchInput(event.target.value)} />
        <div className="batch-actions">
          <span className="muted">当前识别 {parseBatchLinks(batchInput).length} 条非空链接</span>
          <div>
            <button type="button" className="secondary" onClick={() => { setBatchInput(""); setBatchOpen(false); setError(""); }}>取消</button>
            <button type="button" disabled={parseBatchLinks(batchInput).length === 0} onClick={importBatchLinks}>导入链接</button>
          </div>
        </div>
      </div>}
      <p className="muted">已添加 {links.length} / {maxLinks} 条。采集会按添加顺序逐条执行。</p>
      {notice && <p className="success">{notice}</p>}
      {links.length > 0 && <ol className="link-list">
        {links.map((link, index) => <li key={link}>
          <span>{link}</span>
          <button type="button" className="secondary" onClick={() => setLinks((current) => current.filter((_, itemIndex) => itemIndex !== index))}>移除</button>
        </li>)}
      </ol>}
      <label className="capture-scope">
        <input type="checkbox" checked={captureAllSKUs} onChange={(event) => setCaptureAllSKUs(event.target.checked)} />
        <span><strong>采集全部 SKU</strong><small>不勾选时，每条链接只采集链接当前默认的 SKU；勾选后会遍历该商品的全部可售系列与款式。</small></span>
      </label>
      <button onClick={() => void create()} disabled={links.length === 0}>提交并采集</button>
      {error && <p className="error">{error}</p>}
    </section>
    <section className="card">
      <h2>项目</h2>
      {items.length === 0 ? <p className="muted">尚无项目。</p> : <div className="project-list">
        {items.map((project) => <div className="project-row" key={project.id}>
          <div className="project-meta">
            <Link href={`/projects/${project.id}`}>{project.name || "等待采集名称"}</Link>
            {project.status !== "awaiting_sku_selection" && <span className="status">{statusLabel(project.status)}</span>}
            <p className="muted project-created">创建于 {createdAtLabel(project.created_at)}</p>
          </div>
          <button type="button" className="secondary danger" onClick={() => void deleteProject(project)}>删除</button>
        </div>)}
      </div>}
    </section>
    <section className="card">
      <h2>Chrome 扩展 {extensionUpdateRequired && <span className="extension-update">有新版本可以安装</span>}</h2>
      <p>下载并解压扩展包后，打开 <code>chrome://extensions</code>，启用开发者模式，点击“加载已解压的扩展程序”并选择解压后的文件夹。点击扩展图标登录并保持在线。</p>
      {extensionDevice?.bound && !extensionDevice.build_id && <p className="extension-update-detail">当前已安装的是不支持版本上报的旧扩展，请下载并安装新版。</p>}
      {extensionUpdateAvailable && <p className="extension-update-detail">当前扩展有新版本可安装。下载后解压，在 <code>chrome://extensions</code> 中移除旧扩展并重新加载新文件夹。</p>}
      {extensionDevice?.build_id && latestExtension && !extensionUpdateAvailable && <p className="muted">当前已安装最新扩展版本。</p>}
      <a href="/downloads/jd-product-capture-extension.zip" download><button>下载 Chrome 扩展</button></a>
    </section>
  </>;
}
