import { test, expect, describe } from "bun:test";
import { readFileSync } from "fs";
import { join } from "path";

interface SubLayerBreakdown {
  blocks: number;
  saved: number;
}

interface TokenCounts {
  original: number;
  after_layer0: number;
  after_layer1: number;
  final: number;
  saved: number;
  ratio: number;
}

interface RequestSummary {
  req_id: string;
  ts: string;
  provider: string;
  model: string;
  total_messages: number;
  messages_in_window: number;
  messages_compressed: number;
  layers_applied: number[];
  tokens: TokenCounts;
  layer1_breakdown: Record<string, SubLayerBreakdown>;
  cache_hit: boolean;
  secrets_redacted: number;
  proxy_latency_ms: number;
}

function loadRecords(): RequestSummary[] {
  const filePath = join(import.meta.dir, "../fixtures/sample_session.jsonl");
  const content = readFileSync(filePath, "utf-8");
  return content
    .split("\n")
    .filter((line) => line.trim().length > 0)
    .map((line) => JSON.parse(line) as RequestSummary);
}

describe("sample_session.jsonl", () => {
  test("is valid and parseable", () => {
    const records = loadRecords();
    expect(records.length).toBeGreaterThan(0);

    for (const record of records) {
      expect(typeof record.req_id).toBe("string");
      expect(record.req_id.length).toBeGreaterThan(0);

      expect(typeof record.ts).toBe("string");
      // ISO 8601 / RFC3339: must parse as a valid date
      const parsed = new Date(record.ts);
      expect(isNaN(parsed.getTime())).toBe(false);

      expect(typeof record.provider).toBe("string");
      expect(record.provider.length).toBeGreaterThan(0);

      expect(typeof record.tokens).toBe("object");
      expect(record.tokens.saved).toBeGreaterThanOrEqual(0);

      expect(Array.isArray(record.layers_applied)).toBe(true);
    }
  });

  test("RequestSummary tokens.saved equals original minus final", () => {
    const records = loadRecords();
    for (const record of records) {
      const expected = record.tokens.original - record.tokens.final;
      expect(record.tokens.saved).toBe(expected);
    }
  });

  test("layer1_breakdown entries have positive saved values", () => {
    const records = loadRecords();
    for (const record of records) {
      if (!record.layer1_breakdown) continue;
      for (const [, entry] of Object.entries(record.layer1_breakdown)) {
        expect(entry.saved).toBeGreaterThanOrEqual(0);
      }
    }
  });
});
