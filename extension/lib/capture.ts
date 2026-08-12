type ProductImageSet = {
  main: string[];
  variant_main: string[];
  detail: string[];
};

type CapturedProduct = {
  sku: string;
  title: string;
  resolved_url: string;
  price: string;
  availability: "available";
  summary: Record<string, string>;
  parameters: Record<string, string>;
  images: ProductImageSet;
};

type UnavailableVariant = {
  label: string;
  thumbnail_url: string | null;
  high_resolution_image_url: string | null;
};

export type CaptureResult = {
  source_url: string;
  root_sku: string;
  products: CapturedProduct[];
  unresolved_variants: UnavailableVariant[];
};

/**
 * This runs entirely in the JD page context via chrome.scripting.executeScript.
 * Keep all helpers inside this function: injected functions cannot use imports.
 */
export async function collectProductVariants(sourceURL: string): Promise<CaptureResult> {
  const text = (node: Element | null | undefined) => node?.textContent?.replace(/\s+/g, " ").trim() ?? "";
  const wait = (milliseconds: number) => new Promise<void>((resolve) => setTimeout(resolve, milliseconds));
  const absolute = (value: string | null) => {
    if (!value) return null;
    if (value.startsWith("//")) return `https:${value}`;
    try {
      return new URL(value, location.href).href;
    } catch {
      return null;
    }
  };
  const urls = (values: (string | null)[]) => [...new Set(values.map(absolute).filter((value): value is string => Boolean(value)))];
  const image = (node: Element | null) => node?.getAttribute("src")
    ?? node?.getAttribute("data-origin")
    ?? node?.getAttribute("data-url")
    ?? node?.getAttribute("data-lazy-img")
    ?? null;

  const currentSKU = () => location.pathname.match(/\/(\d+)\.html/)?.[1] ?? "";
  const read = (): CapturedProduct => {
    const sku = currentSKU();
    if (!sku) throw new Error("当前 URL 不含京东 SKU");

    const summary: Record<string, string> = {};
    document.querySelectorAll(".page-content-left.preview-wrap #spec-n1 .item").forEach((row) => {
      const name = text(row.querySelector(".desc"));
      const value = text(row.querySelector(".value"));
      if (name && value) summary[name] = value;
    });

    const parameters: Record<string, string> = {};
    document.querySelectorAll(".page-content-left.preview-wrap .list .item").forEach((row) => {
      const name = text(row.querySelector(".label"));
      const valueNode = row.querySelector(".value");
      const value = valueNode?.getAttribute("title")?.trim() || text(valueNode);
      if (name && value) parameters[name] = value;
    });

    const html = document.querySelector("#detail-main")?.innerHTML ?? "";
    const detail = urls([...html.matchAll(/url\(\s*['\"]?([^)'\"]+)['\"]?\s*\)/g)]
      .map((match) => match[1])
      .filter((url) => url.includes("360buyimg.com/sku/")));
    const main = urls([...document.querySelectorAll(".page-content-left.preview-wrap img,#jdImage img,.preview-wrap img")].map(image));
    const selectedVariantImage = document.querySelector(
      ".specification-item-sku--selected img,.specification-item-sku .selected img,.specification-item-sku .active img",
    );

    return {
      sku,
      title: text(document.querySelector(".sku-title-name")),
      resolved_url: location.href,
      price: text(document.querySelector(".product-price--main")),
      availability: "available",
      summary,
      parameters,
      images: {
        main,
        variant_main: urls([image(selectedVariantImage)]),
        detail,
      },
    };
  };

  // JD's current product page uses the item itself as the SKU button. Older
  // layouts use one of the descendants, so select exactly one non-overlapping
  // selector family rather than concatenating all matches.
  const selectorCandidates = [
    ".specification-item-sku",
    ".specification-item-sku li",
    ".specification-item-sku [role='button']",
    ".specification-item-sku .item",
  ];
  const selector = selectorCandidates.find((candidate) => document.querySelector(candidate)) ?? null;
  const options = selector ? [...document.querySelectorAll<HTMLElement>(selector)] : [];
  const products: CapturedProduct[] = [];
  const unresolvedVariants: UnavailableVariant[] = [];
  const seenSKUs = new Set<string>();

  for (const option of options) {
    const unavailable = option.classList.contains("lack")
      || option.classList.contains("specification-item-sku--lack")
      || Boolean(option.closest(".lack,.specification-item-sku--lack"))
      || option.getAttribute("aria-disabled") === "true";
    if (unavailable) {
      const thumbnailURL = absolute(image(option.querySelector("img")));
      unresolvedVariants.push({
        label: text(option),
        thumbnail_url: thumbnailURL,
        high_resolution_image_url: thumbnailURL?.replace(/\/s\d+x\d+_jfs\//, "/jfs/") ?? null,
      });
      continue;
    }

    const priorSKU = currentSKU();
    option.click();
    for (let attempt = 0; attempt < 12; attempt += 1) {
      await wait(250);
      const selected = document.querySelector(".specification-item-sku--selected");
      const nextSKU = currentSKU();
      if (selected === option && (nextSKU !== priorSKU || attempt >= 3)) break;
    }

    const product = read();
    if (!seenSKUs.has(product.sku)) {
      seenSKUs.add(product.sku);
      products.push(product);
    }
  }

  if (products.length === 0) products.push(read());
  return { source_url: sourceURL, root_sku: products[0].sku, products, unresolved_variants: unresolvedVariants };
}
