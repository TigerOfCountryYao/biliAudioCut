import { defineConfig } from "wxt";
export default defineConfig({ manifest: { name:"京东商品采集", description:"在已授权京东商品页采集商品 SKU、规格与图片 URL。", permissions:["tabs","scripting","storage","identity","alarms"], host_permissions:["https://item.jd.com/*","https://u.jd.com/*","http://localhost:8080/*"], action:{default_title:"京东商品采集"} } });
