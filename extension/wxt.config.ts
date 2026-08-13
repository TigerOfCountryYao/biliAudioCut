import { defineConfig } from "wxt";

const apiOrigin = process.env.WXT_API_ORIGIN ?? "http://localhost:8080";
const publicOrigin = process.env.WXT_PUBLIC_ORIGIN ?? "http://localhost:3000";
const apiURL = new URL(apiOrigin);
const publicURL = new URL(publicOrigin);
const apiHostPermission = `${apiURL.protocol}//${apiURL.hostname}/*`;
const publicHostPermission = `${publicURL.protocol}//${publicURL.hostname}/*`;

export default defineConfig({ manifest: { name:"京东商品采集", description:"在已授权京东商品页采集商品 SKU、规格与图片 URL。", permissions:["tabs","scripting","storage","identity","alarms"], host_permissions:[...new Set(["https://item.jd.com/*","https://item.m.jd.com/*","https://u.jd.com/*","https://union-click.jd.com/*","https://pro.m.jd.com/*",apiHostPermission,publicHostPermission])], action:{default_title:"京东商品采集"} } });
