# 随机名字 API — 产品需求文档 (PRD)

## 目标

为基于「好名有据」候选名库的中文起名需求, 提供 **HTTP JSON API**: 输入姓氏 + 可选参数,
自动应用音韵学/避讳/语义规则筛出符合条件的好名字, 再按策略采样 N 个返回。
目标用户: 第三方集成、个人快速起名、给老前端改名/调用。

## 范围

**做**:
- 输入姓氏 + 候选筛选参数, 返回评分通过的 N 个候选名 (按 uniform / weighted 采样).
- 返回单个名字的详细评分卡.
- 匿名限流 (per-IP, 30 rpm 默认), 有效 API key 不限流.
- key 发放 / 吊销运维 CLI.

**不做** (不在本期):
- 不做 UI. 前端继续走原 Taro SPA, 不动.
- 不做 key 的 HTTP 发放端点. 仅 CLI.
- 不做 Redis / 分布式限流.
- 不做 JWT.
- 不做 GraphQL / gRPC.

## 验收标准

1. `GET /api/health` 始终 200, 含五来源静态清单.
2. `GET /api/random?surname=&n=&source=&strategy=&avoid=&must=&mustPosition=&style=&seed=&alpha=&` 返回
   `n` 个 PublicResult (n ≤ 50), 均通过音韵/语义/避讳/必选字规则.
3. `GET /api/name/{fullName}?source=` 返回单名详情或 404.
4. 匿名低于 30 rpm 正常使用, 超额返 429 + Retry-After.
5. 携带有效 `X-API-Key` 或 `Authorization: Bearer` 不受匿名限流约束,响应头 `X-Authed-Authed: true`.
6. `cmd/keymgmt issue/list/revoke` 命令行可用.
7. core Go 端与 TS `packages/name-core` 的评分输出在 12 套 fixture 上严格字段一致.

## 不变约束

- 评分算法与 TS 版 1:1 镜像 (见 `server/internal/core/PORTING.md`).
- DB Schema `server/internal/db/schema.sql` 为事实来源, 一次性 import 脚本填数据.

## 风险与权衡

- **大源 (wealth 57k, modern 60k) QueryNames 单线程耗时 ~300ms (缓存命中后)**, 距 PRD 100ms 目标仍有距离.
  本期接受. 优化方向 (预 SQL 过滤 must 的 first_char、查询并行、缓存 queryNames top-N 切片) 留给后续版本.
- 同分排序 Node localeCompare("zh-Hans-CN") ICU 实现细节复刻成本极高, Go 用"拼音主序 + Unicode tiebreak"
  近等价¹, 随机 API 语义本身不依赖此排序. fixture 对照测试按同分数组集合严格比对 (不验证同组内顺序).