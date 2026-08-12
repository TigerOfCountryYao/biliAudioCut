"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";
import { api, Project, statusLabel, User } from "../lib/api";

const maxLinks = 20;

export default function Home() {
  const [user, setUser] = useState<User>();
  const [items, setItems] = useState<Project[]>([]);
  const [error, setError] = useState("");
  const [name, setName] = useState("");
  const [linkInput, setLinkInput] = useState("");
  const [links, setLinks] = useState<string[]>([]);
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

  async function login(event: FormEvent) {
    event.preventDefault();
    try {
      await api.login(email, password);
      await load();
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
  }

  async function create() {
    if (links.length === 0) {
      setError("请先添加至少一条京东商品链接。");
      return;
    }
    try {
      const project = await api.create(name, links);
      location.href = `/projects/${project.id}`;
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

  return <>
    <div className="header">
      <div><h1>商品采集工作台</h1><p className="muted">{user.display_name} · {user.email}</p></div>
      <button className="secondary" onClick={async () => { await api.logout(); setUser(undefined); }}>退出</button>
    </div>
    <section className="card">
      <h2>新建采集项目</h2>
      <input placeholder="项目名称（可选，默认使用首个商品标题）" value={name} onChange={(event) => setName(event.target.value)} />
      <form className="link-adder" onSubmit={addLink}>
        <input placeholder="粘贴一条京东商品链接" value={linkInput} onChange={(event) => setLinkInput(event.target.value)} />
        <button type="submit">添加链接</button>
      </form>
      <p className="muted">已添加 {links.length} / {maxLinks} 条。采集会按添加顺序逐条执行。</p>
      {links.length > 0 && <ol className="link-list">
        {links.map((link, index) => <li key={link}>
          <span>{link}</span>
          <button type="button" className="secondary" onClick={() => setLinks((current) => current.filter((_, itemIndex) => itemIndex !== index))}>移除</button>
        </li>)}
      </ol>}
      <button onClick={() => void create()} disabled={links.length === 0}>提交并采集</button>
      {error && <p className="error">{error}</p>}
    </section>
    <section className="card">
      <h2>项目</h2>
      {items.length === 0 ? <p className="muted">尚无项目。</p> : items.map((project) => <p key={project.id}>
        <Link href={`/projects/${project.id}`}>{project.name || "等待采集名称"}</Link> <span className="status">{statusLabel(project.status)}</span>
      </p>)}
    </section>
    <section className="card">
      <h2>Chrome 扩展</h2>
      <p>下载并解压扩展包后，打开 <code>chrome://extensions</code>，启用开发者模式，点击“加载已解压的扩展程序”并选择解压后的文件夹。点击扩展图标登录并保持在线。</p>
      <a href="/downloads/jd-product-capture-extension.zip" download><button>下载 Chrome 扩展</button></a>
    </section>
  </>;
}
