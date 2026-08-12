import { copyFile, mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";

const source = resolve(".output/jd-product-capture-extension-0.1.0-chrome.zip");
const destination = resolve("../frontend/public/downloads/jd-product-capture-extension.zip");

await mkdir(dirname(destination), { recursive: true });
await copyFile(source, destination);
console.log(`Extension download package: ${destination}`);
