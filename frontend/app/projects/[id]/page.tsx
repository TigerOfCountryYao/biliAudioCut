"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { api, Detail, statusLabel } from "../../../lib/api";

export default function ProjectPage({ params }: { params: Promise<{ id: string }> }) {
  const [detail, setDetail] = useState<Detail>();
  const [error, setError] = useState("");
  const [id, setID] = useState("");

  useEffect(() => { params.then((value) => setID(value.id)); }, [params]);
  const load = useCallback(async () => {
    if (!id) return;
    try {
      setDetail(await api.detail(id));
    } catch (cause) {
      setError(String(cause));
    }
  }, [id]);
  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 3000);
    return () => clearInterval(timer);
  }, [load]);

  if (!detail) return <p>正在加载… {error}</p>;
  const selected = detail.sources.flatMap((source) => source.products.flatMap((product) => product.skus
    .filter((sku) => sku.selected)
    .map((sku) => sku.id)));

  return <>
    <Link href="/">← 项目列表</Link>
    <div className="header">
      <div>
        <h1>{detail.project.name || "等待商品名称"}</h1>
        <span className="status">{statusLabel(detail.project.status)}</span>
        {detail.project.failure_detail && <p className="error">{detail.project.failure_code}: {detail.project.failure_detail}</p>}
      </div>
      <div className="row">
        {detail.project.status === "failed" && <button className="secondary" onClick={async () => {
          try { await api.retry(id); await load(); } catch (cause) { setError(String(cause)); }
        }}>重新采集失败链接</button>}
        <a href={`/api/projects/${id}/export.xlsx`}><button>下载 Excel</button></a>
      </div>
    </div>
    <section className="card">
      <p className="muted">采集期间请保持 Chrome 扩展在线。页面每 3 秒刷新一次。</p>
      {detail.sources.map((source) => <div className="source" key={source.id}>
        <strong>链接 {source.ordinal + 1}</strong> <span className="status">{statusLabel(source.status)}</span>
        <a className="source-url muted" href={source.source_url} target="_blank" rel="noreferrer">{source.source_url}</a>
        {source.failure_detail && <p className="error">{source.failure_code}: {source.failure_detail}</p>}
        {source.products.map((product) => <div key={product.snapshot_id}>
          <h3>{product.title}</h3>
          {product.skus.map((sku) => <label className="sku" key={sku.id}>
            <input type="checkbox" checked={sku.selected} onChange={async () => {
              const next = selected.includes(sku.id) ? selected.filter((value) => value !== sku.id) : [...selected, sku.id];
              try { await api.selection(id, next); await load(); } catch (cause) { setError(String(cause)); }
            }} />
            <span><strong>{sku.sku}</strong>{"　"}{sku.price ?? "—"}{"　"}{sku.title}</span>
          </label>)}
        </div>)}
      </div>)}
    </section>
    {error && <p className="error">{error}</p>}
  </>;
}
