import { test, expect } from "bun:test";
import { join } from "path";

const moduleRoot = join(import.meta.dir, "../..");

test("tokenproxy version prints version", () => {
  const result = Bun.spawnSync({
    cmd: ["go", "run", "./cmd/tokenproxy", "version"],
    cwd: moduleRoot,
  });
  const stdout = result.stdout.toString();
  expect(stdout.toLowerCase()).toContain("tokenproxy");
  // Version string pattern: digits with dots (e.g. 1.0.0)
  expect(stdout).toMatch(/\d+\.\d+/);
});

test("tokenproxy gain --help shows usage on bad args", () => {
  const result = Bun.spawnSync({
    cmd: ["go", "run", "./cmd/tokenproxy", "gain", "badperiod"],
    cwd: moduleRoot,
  });
  expect(result.exitCode).toBe(1);
});

test("tokenproxy debug --help shows subcommands", () => {
  const result = Bun.spawnSync({
    cmd: ["go", "run", "./cmd/tokenproxy", "debug"],
    cwd: moduleRoot,
  });
  const stderr = result.stderr.toString();
  expect(stderr).toContain("paths");
  expect(stderr).toContain("last");
  expect(stderr).toContain("replay");
});
