// 一次性脚本: 用 TS 版 name-core 跑若干查询, 产出 Go 移植版的对照 fixtures.
// 用法: pnpm exec tsx scripts/generateFixtures.ts
// 产物: server/testdata/fixtures/*.json
import path from "node:path";
import fs from "node:fs";
import type { CandidateName, CharDb, CompactCandidateDb, QueryConfig, SourcePreference } from "../packages/name-core/src";
import { DEFAULT_SOURCE_ID, SOURCE_CONFIGS, hydrateCandidateDb, queryNames, toPublicResult } from "../packages/name-core/src";

const root = path.resolve(__dirname, "..");
const candidateDir = path.resolve(root, "api", "database", "candidate");
const outDir = path.resolve(root, "server", "testdata", "fixtures");
fs.mkdirSync(outDir, { recursive: true });

interface Fix {
  query: QueryConfig;
  sourceId: string;
  sourceLabel: string;
  candidateCount: number;
  results: ReturnType<typeof toPublicResult>[];
}

const cases: Array<{
  name: string;
  surname: string;
  source: SourcePreference | "default";
  avoid?: string[];
  must?: string[];
  mustPosition?: "any" | "second" | "third";
  style?: "any" | "loud" | "soft";
}> = [
  { name: "wealth_yao_default",            surname: "姚",   source: "wealth" },
  { name: "academic_yao_default",           surname: "姚",   source: "academic" },
  { name: "ancient_names_yao_default",      surname: "姚",   source: "ancient_names" },
  { name: "modern_people_yao_default",      surname: "姚",   source: "modern_people" },
  { name: "imperial_exam_yao_default",      surname: "姚",   source: "imperial_exam" },
  { name: "wealth_zhang_default",           surname: "张",   source: "wealth" },
  { name: "academic_li_loud",               surname: "李",   source: "academic", style: "loud" },
  { name: "ancient_zhang_soft",             surname: "张",   source: "ancient_names", style: "soft" },
  { name: "wealth_yao_avoid",               surname: "姚",   source: "wealth", avoid: ["赵钱孙","刘强","李建国"] },
  { name: "academic_yao_must_second",       surname: "姚",   source: "academic", must: ["明"], mustPosition: "second" },
  { name: "ancient_li_must_third",          surname: "李",   source: "ancient_names", must: ["玉"], mustPosition: "third" },
  { name: "modern_chen_must_any",           surname: "陈",   source: "modern_people", must: ["华"] },
];

const charDb: CharDb = JSON.parse(fs.readFileSync(path.join(candidateDir, "candidate_char_db.json"), "utf8"));
const sourceIndex = JSON.parse(fs.readFileSync(path.join(candidateDir, "source_index.json"), "utf8"));

for (const c of cases) {
  const sourceId =
    c.source === "default"
      ? sourceIndex.defaultSourceId || DEFAULT_SOURCE_ID
      : SOURCE_CONFIGS.find((s) => s.id === c.source || s.label === c.source)!.id;
  const stats = sourceIndex.sources[sourceId];
  if (!stats) {
    console.error("无 stats:", sourceId);
    continue;
  }
  const compact = JSON.parse(
    fs.readFileSync(path.join(candidateDir, stats.file.replace(/\\/g, "/")), "utf8"),
  ) as CompactCandidateDb;
  const sourceNames = stats.sourceNameFile
    ? JSON.parse(fs.readFileSync(path.join(candidateDir, stats.sourceNameFile.replace(/\\/g, "/")), "utf8"))
    : {};
  const candidateDb: CandidateName[] = hydrateCandidateDb({
    data: compact,
    sourceId,
    charDb,
    sourceNamesByName: sourceNames,
  });
  if (candidateDb.length === 0) {
    console.warn("candidateDb 为空:", c.name);
  }
  const query: QueryConfig = {
    surname: c.surname,
    avoid: c.avoid,
    must: c.must,
    mustPosition: c.mustPosition,
    style: c.style,
    sourcePreference: c.source === "default" ? "default" : (c.source as SourcePreference),
    limit: 200,
  };
  const results = queryNames({ candidateDb, charDb, query }).map(toPublicResult);
  const fix: Fix = {
    query,
    sourceId,
    sourceLabel: stats.label,
    candidateCount: candidateDb.length,
    results,
  };
  const out = path.join(outDir, c.name + ".json");
  fs.writeFileSync(out, JSON.stringify(fix, null, 2) + "\n", "utf8");
  console.log(`wrote ${out}: ${results.length} results / ${candidateDb.length} candidates`);
}

console.log("done");
// 注: Go 端测试直接从 PG 拉字库与候选, 故不再 dump charDb 到 fixtures