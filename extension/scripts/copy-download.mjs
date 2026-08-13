import { copyFile, mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";

const outputDirectory = resolve(".output");
const zipName = (await readdir(outputDirectory)).find((name) => /^jd-product-capture-extension-.*-chrome\.zip$/.test(name));
if (!zipName) throw new Error("extension ZIP was not produced");
const source = resolve(outputDirectory, zipName);
const destination = resolve("../frontend/public/downloads/jd-product-capture-extension.zip");
const metadataSource = resolve(".generated/build-info.json");
const metadataOutput = resolve(".output/jd-product-capture-extension.json");
const metadataDestination = resolve("../frontend/public/downloads/jd-product-capture-extension.json");
const copyToFrontend = process.argv.includes("--copy-to-frontend");
const buildInfo = JSON.parse(await readFile(metadataSource, "utf8"));
const metadata = {
  ...buildInfo,
  download_url: "/downloads/jd-product-capture-extension.zip",
};

await writeFile(metadataOutput, `${JSON.stringify(metadata, null, 2)}\n`, "utf8");
if (copyToFrontend) {
  await mkdir(dirname(destination), { recursive: true });
  await copyFile(source, destination);
  await copyFile(metadataOutput, metadataDestination);
  console.log(`Extension download package: ${destination}`);
  console.log(`Extension latest metadata: ${metadataDestination}`);
}
