import { afterEach, describe, expect, it, vi } from "vitest";
import { classifyJDPage, clickJDClaimButton, desktopProductURLFromMobile } from "./product-page-navigation";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("JD product-page navigation", () => {
  it("recognizes only canonical product and coupon landing pages", () => {
    expect(classifyJDPage("https://item.jd.com/100012345678.html?cu=true")).toBe("product");
    expect(classifyJDPage("https://item.m.jd.com/ware/view.action?wareId=100287955796")).toBe("mobile_product");
    expect(classifyJDPage("https://pro.m.jd.com/mall/active/example/index.html?sku=encrypted")).toBe("coupon");
    expect(classifyJDPage("https://plogin.m.jd.com/login/login?returnurl=example")).toBe("login");
    expect(classifyJDPage("https://passport.jd.com/new/login.aspx")).toBe("login");
    expect(classifyJDPage("https://pc-frequent-pro.pf.jd.com/?from=pc_item&reason=403")).toBe("rate_limited");
    expect(classifyJDPage("https://pro.m.jd.com/other/index.html")).toBe("other");
    expect(classifyJDPage("https://pro.m.jd.com.example.com/mall/active/example/index.html")).toBe("other");
  });

  it("converts the observed mobile product URL to the same desktop SKU", () => {
    expect(desktopProductURLFromMobile("https://item.m.jd.com/ware/view.action?wareId=100287955796"))
      .toBe("https://item.jd.com/100287955796.html");
    expect(desktopProductURLFromMobile("https://item.m.jd.com/product/100287955796.html?utm_source=test"))
      .toBe("https://item.jd.com/100287955796.html");
  });

  it("rejects malformed mobile product URLs instead of constructing a destination", () => {
    expect(classifyJDPage("https://item.m.jd.com/ware/view.action?wareId=100287955796abc")).toBe("mobile_product");
    expect(desktopProductURLFromMobile("https://item.m.jd.com/ware/view.action?wareId=100287955796abc")).toBeNull();
    expect(desktopProductURLFromMobile("https://item.m.jd.com/ware/view.action?wareId=https://evil.example")).toBeNull();
    expect(desktopProductURLFromMobile("https://item.m.jd.com.example.com/ware/view.action?wareId=100287955796")).toBeNull();
  });

  it("clicks the only visible exact 一键领取 label", () => {
    const click = vi.fn();
    const exactVisible = element("一键领取", click);
    const similarVisible = element("立即一键领取");
    const exactHidden = element("一键领取", undefined, false);
    stubDOM([exactVisible, similarVisible, exactHidden]);

    expect(clickJDClaimButton()).toEqual({ status: "clicked" });
    expect(click).toHaveBeenCalledOnce();
  });

  it("does not click when more than one exact visible label exists", () => {
    const firstClick = vi.fn();
    const secondClick = vi.fn();
    stubDOM([element("一键领取", firstClick), element("一键领取", secondClick)]);

    expect(clickJDClaimButton()).toEqual({ status: "ambiguous" });
    expect(firstClick).not.toHaveBeenCalled();
    expect(secondClick).not.toHaveBeenCalled();
  });
});

function element(textContent: string, click = vi.fn(), visible = true) {
  return {
    children: [],
    textContent,
    click,
    getClientRects: () => (visible ? [{}] : []),
  };
}

function stubDOM(elements: ReturnType<typeof element>[]) {
  vi.stubGlobal("document", { querySelectorAll: () => elements });
  vi.stubGlobal("window", {
    getComputedStyle: () => ({ display: "block", visibility: "visible" }),
  });
}
