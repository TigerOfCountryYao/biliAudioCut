"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { api, Detail, statusLabel } from "../../../lib/api";

type ProductSKU = Detail["sources"][number]["products"][number]["skus"][number];

function groupBySeries(skus: ProductSKU[]) {
  const groups = new Map<string, { label: string; ordinal: number; skus: ProductSKU[] }>();
  for (const sku of skus) {
    const label = sku.series_label || "默认系列";
    const key = `${sku.series_ordinal}:${label}`;
    const group = groups.get(key) ?? { label, ordinal: sku.series_ordinal, skus: [] };
    group.skus.push(sku);
    groups.set(key, group);
  }
  return [...groups.values()].sort((left, right) => left.ordinal - right.ordinal);
}

function sourceStatusLabel(source: Detail["sources"][number]) {
  if (source.interaction_kind === "login") return "需要完成京东登录";
  if (source.interaction_kind === "verification") return "需要完成京东验证";
  return statusLabel(source.status);
}

export default function ProjectPage({ params }: { params: Promise<{ id: string }> }) {
  const [detail, setDetail] = useState<Detail>();
  const [error, setError] = useState("");
  const [id, setID] = useState("");

  useEffect(() => { params.then((value) => setID(value.id)); }, [params]);
  const load = useCallback(async () => {
    if (!id) return;
    try { setDetail(await api.detail(id)); } catch (cause) { setError(String(cause)); }
  }, [id]);
  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 3000);
    return () => clearInterval(timer);
  }, [load]);

	if (!detail) return <p>正在加载… {error}</p>;
	const selected = detail.sources.flatMap((source) => source.products.flatMap((product) => product.skus.filter((sku) => sku.selected).map((sku) => sku.id)));
	const waitingForJDAction = detail.project.status === "awaiting_jd_login" || detail.project.status === "awaiting_jd_verification";
	const canResume = detail.project.status === "failed" || waitingForJDAction;
	  const canExport = detail.project.status === "awaiting_sku_selection" || detail.project.status === "failed" || waitingForJDAction;
	const canSelect = canExport;

  const updateSelection = async (next: string[]) => {
    try { await api.selection(id, next); await load(); } catch (cause) { setError(String(cause)); }
  };

  return <>
    <Link href="/">← 项目列表</Link>
    <div className="header">
      <div>
        <h1>{detail.project.name || "等待商品名称"}</h1>
        <span className="status">{statusLabel(detail.project.status)}</span>
		<span className="status">{detail.project.capture_all_skus ? "全部 SKU" : "默认 SKU"}</span>
		{(detail.project.status === "failed" || waitingForJDAction) && detail.project.failure_detail && <p className={waitingForJDAction ? "muted" : "error"}>{detail.project.failure_code}: {detail.project.failure_detail}</p>}
      </div>
      <div className="row">
		{canResume && <button className="secondary" onClick={async () => { try { await api.retry(id); await load(); } catch (cause) { setError(String(cause)); } }}>继续采集未完成链接</button>}
		        {canExport && <a href={`/api/projects/${id}/export.xlsx`}><button>{detail.project.status === "awaiting_sku_selection" ? "下载 Excel" : "下载已采集 Excel"}</button></a>}
		        {canExport && <a href={`/api/projects/${id}/main-images.zip`}><button className="secondary">{detail.project.status === "awaiting_sku_selection" ? "下载主图 ZIP" : "下载已采集主图 ZIP"}</button></a>}
      </div>
    </div>
    <section className="card">
	  <p className="muted">采集期间请保持 Chrome 扩展在线。页面每 3 秒刷新一次。{waitingForJDAction ? "请先在京东标签页手动完成登录或验证，再点击“继续采集未完成链接”。系统不会代替你提交登录、验证码或购买操作。" : detail.project.status === "failed" ? "可下载当前已采集到的部分结果；继续采集只会补采失败或未完成链接，已成功的链接不会重复采集。" : canSelect ? "采集结果已默认全选，可按需取消。" : "全部链接采集完成后可以调整 SKU。"}</p>
      {detail.sources.map((source) => <div className="source" key={source.id}>
	        <strong>链接 {source.ordinal + 1}</strong> <span className="status">{sourceStatusLabel(source)}</span>
        <a className="source-url muted" href={source.source_url} target="_blank" rel="noreferrer">{source.source_url}</a>
	        {source.failure_detail && <p className={source.interaction_kind ? "muted" : "error"}>{source.failure_code}: {source.failure_detail}</p>}
			{source.interaction_kind && <button className="secondary" onClick={async () => { try { await api.openJDAction(id, source.id); } catch (cause) { setError(String(cause)); } }}>{source.interaction_kind === "login" ? "打开京东登录页" : "查看并完成京东验证"}</button>}
        {source.products.map((product) => <div key={product.snapshot_id}>
          <h3>{product.title}</h3>
          {groupBySeries(product.skus).map((series) => {
            const seriesIDs = series.skus.map((sku) => sku.id);
            const allSelected = seriesIDs.every((skuID) => selected.includes(skuID));
            return <section className="series" key={`${series.ordinal}-${series.label}`}>
              <label className="series-toggle">
				<input type="checkbox" checked={allSelected} disabled={!canSelect} onChange={() => {
                  void updateSelection(allSelected ? selected.filter((skuID) => !seriesIDs.includes(skuID)) : [...new Set([...selected, ...seriesIDs])]);
                }} />
                <span><strong>{series.label}</strong> <small>{series.skus.length} 款</small></span>
              </label>
              <div className="series-skus">
                {series.skus.map((sku) => <label className="sku" key={sku.id}>
				  <input type="checkbox" checked={sku.selected} disabled={!canSelect} onChange={() => {
                    void updateSelection(selected.includes(sku.id) ? selected.filter((value) => value !== sku.id) : [...selected, sku.id]);
                  }} />
                  <span><strong>{sku.variant_label || sku.sku}</strong>{"　"}{sku.price ?? "—"}{"　"}<small>SKU：{sku.sku}</small>{"　"}{sku.title}</span>
                </label>)}
              </div>
            </section>;
          })}
        </div>)}
      </div>)}
    </section>
    {error && <p className="error">{error}</p>}
  </>;
}
