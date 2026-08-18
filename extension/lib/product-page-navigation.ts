export type JDPageKind = "product" | "mobile_product" | "coupon" | "login" | "verification" | "rate_limited" | "other";
export type ClaimButtonResult = { status: "clicked" | "missing" | "ambiguous" };
export type LoginRedirectState = { startedAt?: number; confirmed: boolean };

export const loginRedirectConfirmationMilliseconds = 5_000;

// Some JD short-link flows briefly visit a login host while handing the
// request back to an already authenticated browser. Only a sustained stay on
// that host should be treated as a real login failure.
export function observeLoginRedirect(pageKind: JDPageKind, startedAt: number | undefined, now: number): LoginRedirectState {
  if (pageKind !== "login") return { startedAt: undefined, confirmed: false };
  const nextStartedAt = startedAt ?? now;
  return {
    startedAt: nextStartedAt,
    confirmed: now-nextStartedAt >= loginRedirectConfirmationMilliseconds,
  };
}

export function desktopProductURLFromMobile(rawURL: string): string | null {
  try {
    const url = new URL(rawURL);
    if (url.protocol !== "https:" || url.hostname !== "item.m.jd.com") return null;

    let sku = "";
    if (url.pathname === "/ware/view.action") {
      sku = url.searchParams.get("wareId") ?? "";
    } else {
      sku = url.pathname.match(/^\/product\/(\d+)\.html$/)?.[1] ?? "";
    }
    if (!/^\d+$/.test(sku)) return null;
    return `https://item.jd.com/${sku}.html`;
  } catch {
    return null;
  }
}

export function classifyJDPage(rawURL: string): JDPageKind {
  try {
    const url = new URL(rawURL);
    if (url.protocol === "https:" && url.hostname === "item.jd.com" && /^\/\d+\.html$/.test(url.pathname)) {
      return "product";
    }
    if (url.protocol === "https:" && url.hostname === "item.m.jd.com" &&
      (url.pathname === "/ware/view.action" || /^\/product\/[^/]+\.html$/.test(url.pathname))) {
      return "mobile_product";
    }
    if (url.protocol === "https:" && url.hostname === "pro.m.jd.com" && url.pathname.startsWith("/mall/active/")) {
      return "coupon";
    }
	if (url.protocol === "https:" && (url.hostname === "plogin.m.jd.com" || url.hostname === "passport.jd.com")) {
		return "login";
	}
	if (url.protocol === "https:" && url.hostname === "cfe.m.jd.com") {
		return "verification";
	}
    if (url.protocol === "https:" && url.hostname === "pc-frequent-pro.pf.jd.com") {
      return "rate_limited";
    }
  } catch {
    // A partially loaded tab can briefly have a non-URL value. Keep waiting.
  }
  return "other";
}

// This function is serialized by chrome.scripting.executeScript, so it must
// remain self-contained and may not depend on module-level browser state.
export function clickJDClaimButton(): ClaimButtonResult {
  const candidates = Array.from(document.querySelectorAll<HTMLElement>("body *")).filter((element) => {
    if (element.children.length !== 0 || element.textContent?.trim() !== "一键领取") {
      return false;
    }
    const style = window.getComputedStyle(element);
    return style.display !== "none" && style.visibility !== "hidden" && element.getClientRects().length > 0;
  });

  if (candidates.length === 0) return { status: "missing" };
  if (candidates.length !== 1) return { status: "ambiguous" };
  candidates[0].click();
  return { status: "clicked" };
}
