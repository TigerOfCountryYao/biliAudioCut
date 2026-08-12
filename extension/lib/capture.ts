type ProductImageSet = { variant_main: string[] };
type CapturedProduct = { sku: string; title: string; variant_label: string; resolved_url: string; price: string; availability: "available"; series_label: string; series_ordinal: number; summary: Record<string, string>; parameters: Record<string, string>; images: ProductImageSet };
type UnavailableVariant = { label: string; series_label: string; series_ordinal: number; thumbnail_url: string | null; high_resolution_image_url: string | null };
export type CaptureResult = { source_url: string; root_sku: string; products: CapturedProduct[]; unresolved_variants: UnavailableVariant[] };

/** Runs entirely in the JD page context through chrome.scripting.executeScript. */
export async function collectProductVariants(sourceURL: string): Promise<CaptureResult> {
  const text = (node: Element | null | undefined) => node?.textContent?.replace(/\s+/g, " ").trim() ?? "";
  const wait = (milliseconds: number) => new Promise<void>((resolve) => setTimeout(resolve, milliseconds));
  const absolute = (value: string | null) => {
    if (!value) return null;
    if (value.startsWith("//")) return `https:${value}`;
    try { return new URL(value, location.href).href; } catch { return null; }
  };
  const image = (node: Element | null) => node?.getAttribute("data-origin") ?? node?.getAttribute("data-url") ?? node?.getAttribute("data-lazy-img") ?? node?.getAttribute("src") ?? null;
  const currentSKU = () => location.pathname.match(/\/(\d+)\.html/)?.[1] ?? "";
  const seriesSelector = ".specification-series-item";
  const variantSelector = ".specification-item-sku";
  const selectedSeriesSelector = ".specification-series-item--selected";
  const selectedVariantSelector = ".specification-item-sku--selected";

  const read = (seriesLabel: string, seriesOrdinal: number, variantLabel: string): CapturedProduct => {
    const sku = currentSKU();
    if (!sku) throw new Error("当前 URL 不含京东 SKU");
    const summary: Record<string, string> = {};
    document.querySelectorAll(".page-content-left.preview-wrap #spec-n1 .item").forEach((row) => {
      const name = text(row.querySelector(".desc")); const value = text(row.querySelector(".value"));
      if (name && value) summary[name] = value;
    });
    const parameters: Record<string, string> = {};
    document.querySelectorAll(".page-content-left.preview-wrap .list .item").forEach((row) => {
      const name = text(row.querySelector(".label")); const valueNode = row.querySelector(".value");
      const value = valueNode?.getAttribute("title")?.trim() || text(valueNode);
      if (name && value) parameters[name] = value;
    });
    const variantMain = absolute(image(document.querySelector(`${selectedVariantSelector} img`)));
    const highResolutionVariantMain = variantMain?.replace(/\/s\d+x\d+_jfs\//, "/jfs/") ?? null;
    return { sku, title: text(document.querySelector(".sku-title-name")), variant_label: variantLabel, resolved_url: location.href, price: text(document.querySelector(".product-price--main")), availability: "available", series_label: seriesLabel, series_ordinal: seriesOrdinal, summary, parameters, images: { variant_main: highResolutionVariantMain ? [highResolutionVariantMain] : [] } };
  };
  const isUnavailable = (node: HTMLElement) => node.classList.contains("lack") || node.classList.contains("specification-item-sku--lack") || Boolean(node.closest(".lack,.specification-item-sku--lack")) || node.getAttribute("aria-disabled") === "true" || /无货/.test(node.getAttribute("title") ?? "");
  const indexOf = (selector: string, node: Element) => [...document.querySelectorAll(selector)].indexOf(node);
  const waitUntilSelected = async (selector: string, index: number) => {
    for (let attempt = 0; attempt < 20; attempt += 1) {
      await wait(200);
      const selected = document.querySelector(selector);
      if (selected && indexOf(selector.replace("--selected", ""), selected) === index) return;
    }
    throw new Error("京东页面未能完成款式切换");
  };
  const dismissSimilarProductDialog = () => {
    for (const button of document.querySelectorAll<HTMLElement>("button,.btn,.dialog-button")) {
      if (/取消|关闭/.test(text(button))) button.click();
    }
  };

  const products: CapturedProduct[] = [];
  const unresolvedVariants: UnavailableVariant[] = [];
  const seriesCount = document.querySelectorAll(seriesSelector).length || 1;
  let rootSKU = "";

  for (let seriesIndex = 0; seriesIndex < seriesCount; seriesIndex += 1) {
    const seriesNodes = [...document.querySelectorAll<HTMLElement>(seriesSelector)];
    const series = seriesNodes[seriesIndex];
    if (series) {
      const seriesLabel = text(series);
      if (!series.classList.contains("specification-series-item--selected")) {
        series.click();
        await waitUntilSelected(selectedSeriesSelector, seriesIndex);
      }
      const variantCount = document.querySelectorAll(variantSelector).length;
      for (let variantIndex = 0; variantIndex < variantCount; variantIndex += 1) {
        const variant = document.querySelectorAll<HTMLElement>(variantSelector)[variantIndex];
        if (!variant) continue;
        if (isUnavailable(variant)) {
          const thumbnailURL = absolute(image(variant.querySelector("img")));
          unresolvedVariants.push({ label: text(variant), series_label: seriesLabel, series_ordinal: seriesIndex, thumbnail_url: thumbnailURL, high_resolution_image_url: thumbnailURL?.replace(/\/s\d+x\d+_jfs\//, "/jfs/") ?? null });
          continue;
        }
        const variantLabel = text(variant);
        variant.click();
        await waitUntilSelected(selectedVariantSelector, variantIndex);
        dismissSimilarProductDialog();
        await wait(400);
        const product = read(seriesLabel, seriesIndex, variantLabel);
        if (!rootSKU) rootSKU = product.sku;
        products.push(product);
      }
    } else {
      const product = read("默认系列", 0, text(document.querySelector(selectedVariantSelector)));
      rootSKU = product.sku;
      products.push(product);
    }
  }
  if (products.length === 0) throw new Error("未采集到可售 SKU");
  return { source_url: sourceURL, root_sku: rootSKU, products, unresolved_variants: unresolvedVariants };
}
