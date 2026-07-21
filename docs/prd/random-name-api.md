# 随机名字 API — 产品需求文档 (PRD)

## 目标

为基于「好名有据」候选名库的中文起名需求, 提供 **HTTP JSON API**: 输入可选姓氏与筛选参数,
自动应用音韵学/避讳/语义规则筛出符合条件的好名字, 再按策略采样 N 个返回。
目标用户: 第三方集成、个人快速起名、给老前端改名/调用。

## 范围

**做**:
- 输入可选姓氏 + 候选筛选参数, 返回评分通过的 N 个候选名 (按 uniform / weighted 采样).
  - 缺省姓氏: 从百家姓池随机.
  - 缺省来源: 五源合并去重.
  - 缺省采样: uniform.
- 返回单个名字的详细评分卡.
- 匿名限流 (per-IP, 30 rpm 默认), 有效 API key 不限流.
- key 发放 / 吊销运维 CLI.
- 静态帮助端点 `GET /api/help`.

**不做** (不在本期):
- 不做 UI. 前端继续走原 Taro SPA, 不动.
- 不做 key 的 HTTP 发放端点. 仅 CLI.
- 不做 Redis / 分布式限流.
- 不做 JWT.
- 不做 GraphQL / gRPC.
- 不做成功响应上的 RateLimit 头回写 (仅 429 带限流头).

## 验收标准

1. `GET /api/health` 返回 200, 含五来源静态清单 (匿名请求计入限流桶).
2. `GET /api/help` 返回 200, 参数默认值与实现一致 (surname 百家姓随机 / source 全源 / strategy uniform).
3. `GET /api/random?n=&source=&strategy=&avoid=&must=&mustPosition=&style=&seed=&alpha=&` (surname 可选):
   - 返回 `n` 个 PublicResult (n ≤ 50), 均通过音韵/语义/避讳/必选字规则.
   - 缺省 `source` 时 `source.id == "all"`.
   - 缺省 `strategy` 时为 `uniform`.
   - 缺省 `surname` 时从 `surnames` 池抽取 (表空兜底「张」).
4. `GET /api/name/{fullName}?source=` 返回单名详情或 404.
5. 匿名低于 30 rpm 正常使用, 超额返 429 + Retry-After + 限流头.
6. 携带有效 `X-API-Key` 或 `Authorization: Bearer` 不受匿名限流约束, 响应头 `X-Authed-Authed: true`.
7. `cmd/keymgmt issue/list/revoke` 命令行可用.
8. core Go 端与 TS `packages/name-core` 的评分输出在 12 套 fixture 上严格字段一致.

## 不变约束

- 评分算法与 TS 版 1:1 镜像 (见 `server/internal/core/PORTING.md`).
- DB Schema `server/internal/db/schema.sql` 为事实来源, 一次性 import 脚本填数据 (含 surnames).

## 风险与权衡

- **大源 (wealth 57k, modern 60k) QueryNames 单线程耗时 ~300ms (缓存命中后)**, 全源合并更重.
  本期接受. 优化方向 (预 SQL 过滤 must 的 first_char、查询并行、缓存 queryNames top-N 切片) 留给后续版本.
- 同分排序 Node localeCompare("zh-Hans-CN") ICU 实现细节复刻成本极高, Go 用"拼音主序 + Unicode tiebreak"
  近等价, 随机 API 语义本身不依赖此排序. fixture 对照测试按同分数组集合严格比对 (不验证同组内顺序).
