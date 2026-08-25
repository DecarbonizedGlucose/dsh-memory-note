import { createRequire } from "node:module";

interface PackageInfo {
  version?: unknown;
}

/** Returns the protocol major carried by one application version. */
export function protocolMajorFrom(version: string): number {
  const match = /^(\d+)\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.exec(version);
  if (match === null) {
    throw new Error(`adapter package version is invalid: ${version}`);
  }
  return Number(match[1]);
}

const require = createRequire(import.meta.url);
const pkg = require("../package.json") as PackageInfo;

if (typeof pkg.version !== "string") {
  throw new Error("adapter package version is missing");
}

export const applicationVersion = pkg.version;
export const protocolMajor = protocolMajorFrom(applicationVersion);
