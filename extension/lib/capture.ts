type ProductImageSet = { main: string[]; variant_main: string[]; detail: string[] };
type CapturedProduct = { sku: string; title: string; resolved_url: string; price: string; availability: "available"; summary: Record<string, string>; parameters: Record<string, string>; images: ProductImageSet };
type UnavailableVariant = { label: string; series_label: string; thumbnail_url: string | null; high_resolution_image_url: string | null };
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
  const urls = (values: (string | null)[]) => [...new Set(values.map(absolute).filter((value): value is string => Boolean(value)))];
  const image = (node: Element | null) => node?.getAttribute("src") ?? node?.getAttribute("data-origin") ?? node?.getAttribute("data-url") ?? node?.getAttribute("data-lazy-img") ?? null;
  const currentSKU = () => location.pathname.match(/\/(\d+)\.html/)?.[1] ?? "";
  const seriesSelector = ".specification-series-item";
  const variantSelector = ".specification-item-sku";
  const selectedSeriesSelector = ".specification-series-item--selected";
  const selectedVariantSelector = ".specification-item-sku--selected";

  const read = (): CapturedProduct => {
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
    const html = document.querySelector("#detail-main")?.innerHTML ?? "";
    const detail = urls([...html.matchAll(/url\(\s*['\"]?([^)'"]+)['\"]?\s*\)/g)].map((match) => match[1]).filter((url) => url.includes("360buyimg.com/sku/")));
    const main = urls([...document.querySelectorAll(".page-content-left.preview-wrap img,#jdImage img,.preview-wrap img")].map(image));
    return { sku, title: text(document.querySelector(".sku-title-name")), resolved_url: location.href, price: text(document.querySelector(".product-price--main")), availability: "available", summary, parameters, images: { main, variant_main: urls([image(document.querySelector(`${selectedVariantSelector} img`))]), detail } };
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
  const seenSKUs = new Set<string>();
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
      // Series switching replaces the variant nodes, so capture the count now
      // and resolve every node again by index before interacting with it.
      const variantCount = document.querySelectorAll(variantSelector).length;
      for (let variantIndex = 0; variantIndex < variantCount; variantIndex += 1) {
        const variant = document.querySelectorAll<HTMLElement>(variantSelector)[variantIndex];
        if (!variant) continue;
        if (isUnavailable(variant)) {
          const thumbnailURL = absolute(image(variant.querySelector("img")));
          unresolvedVariants.push({ label: text(variant), series_label: seriesLabel, thumbnail_url: thumbnailURL, high_resolution_image_url: thumbnailURL?.replace(/\/s\d+x\d+_jfs\//, "/jfs/") ?? null });
          continue;
        }
        variant.click();
        await waitUntilSelected(selectedVariantSelector, variantIndex);
        // Defensive only: never let an unexpected dialog block later SKUs or
        // accept JD's "switch to similar product" offer.
        dismissSimilarProductDialog();
        await wait(400);
        const product = read();
        if (!rootSKU) rootSKU = product.sku;
        if (!seenSKUs.has(product.sku)) { seenSKUs.add(product.sku); products.push(product); }
      }
    } else {
      const product = read();
      rootSKU = product.sku;
      products.push(product);
    }
  }
  if (products.length === 0) throw new Error("未采集到可售 SKU");
  return { source_url: sourceURL, root_sku: rootSKU, products, unresolved_variants: unresolvedVariants };
}
