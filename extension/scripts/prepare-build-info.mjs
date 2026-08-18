import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";

const packageJSON = JSON.parse(await readFile(resolve("package.json"), "utf8"));
const buildTime = new Date().toISOString();
const buildInfo = {
  version: packageJSON.version,
  // A build may be recreated while only the API or webpage changes. Keep the
  // extension identity tied to its explicit package version so that does not
  // produce a false update notice for already-installed extensions.
  build_id: packageJSON.version,
  build_time: buildTime,
};
const destination = resolve(".generated/build-info.json");

await mkdir(dirname(destination), { recursive: true });
await writeFile(destination, `${JSON.stringify(buildInfo, null, 2)}\n`, "utf8");
console.log(`Extension build: ${buildInfo.build_id} at ${buildInfo.build_time}`);
