# 随机名字 API — 接口文档

Base URL (本地开发): `http://localhost:8080`

## 公共响应头

| Header            | 何时出现     | 含义                               |
|-------------------|--------------|------------------------------------|
| `Content-Type`    | 始终         | `application/json; charset=utf-8`  |
| `X-Authed-Authed` | 始终         | `true` / `false`，是否携带有效 key |
| `X-RateLimit-Limit` | **仅 429** | 匿名 RPM 上限 (默认 30)            |
| `X-RateLimit-Remaining` | **仅 429** | 本分钟剩余请求数（超额时为 0） |
| `X-RateLimit-Reset` | **仅 429** | 当前 epoch 秒，下次重置参考        |
| `Retry-After`     | **仅 429**   | 建议等待重试秒数                   |

> 现状：成功响应不附带 `X-RateLimit-*`（handler 未回写中间件桶状态）。限流信息仅在 429 可见。

## 端点

全部 `/api/*` 走同一中间件链：`Auth → RateLimit → Handler`。有效 key 跳过限流；**help / health 匿名请求同样计入限流桶**。

### `GET /api/help`

返回静态 API 帮助（端点列表、参数、来源、认证/限流说明）。无 DB 调用。

**响应 200** 摘要与运行时 `handler_help.go` 一致，关键默认值：

| 参数 | 默认（help 中） |
|------|-----------------|
| `surname` | 百家姓随机 |
| `strategy` | `uniform` |
| `source` | 全部来源合并 |
| `n` | `5` |
| `mustPosition` / `style` | `any` |
| `seed` | `0`（非确定） |
| `alpha` | `0.15` |

### `GET /api/health`

探活 + 五来源静态清单（`core.SourceConfigs`，无 DB）。

**响应 200**:
```json
{
  "ok": true,
  "sources": [
    {"id":"wealth","label":"财富论","priority":1,"weight":14,"category":"wealth","description":"..."},
    {"id":"academic","label":"五道口","priority":2,"weight":17,"category":"academic","description":"..."},
    {"id":"modern_people","label":"他山石","priority":3,"weight":16,"category":"modern","description":"..."},
    {"id":"imperial_exam","label":"登科录","priority":4,"weight":9,"category":"historic","description":"..."},
    {"id":"ancient_names","label":"古人云","priority":5,"weight":8,"category":"historic","description":"..."}
  ]
}
```

### `GET /api/random`

按条件筛选候选名后采样返回。匿名限流 30 req/min；有效 API key 不限。

**Query 参数**:

| 名 | 类型 | 必填 | 默认 | 说明 |
|----|------|------|------|------|
| `surname` | string | 否 | **百家姓随机** | 1 个汉字。缺省从 PG `surnames` 表随机抽；表空时兜底 `"张"` |
| `n` | int (1..50) | 否 | `5` | 返回个数，上限 50 |
| `strategy` | `uniform` \| `weighted` | 否 | **`uniform`** | `uniform` 无放回均匀；`weighted` 按 softmax(score·α) 采样。非法值回退 uniform |
| `source` | string | 否 | **全部来源合并** | 来源 id 或中文 label。缺省合并五源并按 name 去重（保留 priority 更高者）；显式传则单源。亦接受 `sourcePreference` 别名 |
| `avoid` | csv string | 否 | 无 | 避开的姓名/字，逗号分隔。例 `avoid=赵钱孙,刘强` |
| `must` | csv string | 否 | 无 | 名中必须出现的字。例 `must=明` |
| `mustPosition` | `any` \| `second` \| `third` | 否 | `any` | must 字位置：`second`=名第一字；`third`=名第二字 |
| `style` | `any` \| `loud` \| `soft` | 否 | `any` | `loud` 偏二三四声收尾；`soft` 偏一二声收尾 |
| `seed` | int | 否 | `0`（随机） | 非 0 时可复现采样与缺省姓抽取（共用同一 RNG） |
| `alpha` | float | 否 | `0.15` | weighted 锐度；越大高分越排他 |

**可选来源 id / label**:

| id | label |
|----|-------|
| `wealth` | 财富论 |
| `academic` | 五道口 |
| `modern_people` | 他山石 |
| `imperial_exam` | 登科录 |
| `ancient_names` | 古人云 |

**响应 200**（指定 source 示例）:
```json
{
  "query": {"surname":"姚","mustPosition":"any","style":"any","sourcePreference":"academic"},
  "source": {"id":"academic","label":"五道口","count":8627},
  "strategy": "uniform",
  "seed": 0,
  "total_filtered": 30,
  "results": [
    {
      "fullName": "姚悟移",
      "name": "悟移",
      "score": 103,
      "breakdown": {"semantic":35,"phonetic":30,"source":20,"explainability":5,"charQuality":8,"rarity":5},
      "sources": ["五道口"],
      "sourceNames": ["张悟移(国家自然基金)"],
      "pinyin": ["wù","yí"],
      "tonePattern": "242",
      "semantic": "",
      "phonetic": "读音顺畅，未发现同音、叠声或叠韵问题",
      "reasons": ["读音顺畅，未发现同音、叠声或叠韵问题","来源：五道口","用字频率可接受"],
      "explanation": "姚悟移：总分 103；来源：五道口；音律：读音顺畅，未发现同音、叠声或叠韵问题"
    }
  ]
}
```

**缺省 source 时** `source` 字段为：
```json
{"id":"all","label":"全部来源","count": <合并去重后候选数>}
```
且 `query.sourcePreference` 为 `"default"`。

**错误**:

| HTTP | `error` | 含义 |
|------|---------|------|
| 400 | `unknown_source` | source id/label 不识别 |
| 400 | `source_empty` | 来源无候选行（未 import 或空表） |
| 405 | `method_not_allowed` | 非 GET |
| 422 | `surname_not_in_char_db` | 姓氏不在字库 |
| 429 | `rate_limited` | 匿名超额，见 `Retry-After` |
| 500 | `char_db_loading` | 字库未就绪 |

### `GET /api/name/{fullName}`

单名详情。路径 `{fullName}` = 姓 + 2 字名 = 共 3 字（非汉字会先剥离）。

**Query 参数**:

| 名 | 类型 | 必填 | 默认 | 说明 |
|----|------|------|------|------|
| `source` | string | 否 | 无 | 指定来源 id/label。不传则按 priority 顺序查 5 源，返回第一个匹配 |

**响应 200**:
```json
{
  "fullName": "姚悟移",
  "name": "悟移",
  "score": 103,
  "breakdown": { "...": "..." },
  "sources": ["五道口"],
  "sourceNames": ["张悟移(国家自然基金)"],
  "pinyin": ["wù","yí"],
  "tonePattern": "242",
  "semantic": "",
  "phonetic": "...",
  "reasons": ["..."],
  "explanation": "...",
  "found": true
}
```

**错误**:

| HTTP | `error` | 含义 |
|------|---------|------|
| 400 | `missing_name` / `invalid_name` / `unknown_source` | 路径缺名 / 不足 3 字 / source 不识别 |
| 404 | `not_found` | 指定范围内无该候选名 |
| 422 | `surname_not_in_char_db` | 姓氏不在字库 |
| 429 | `rate_limited` | 匿名超额 |
| 500 | `char_db_loading` | 字库未就绪 |

## 认证

请求头二选一：
- `X-API-Key: <KEY>`
- `Authorization: Bearer <KEY>`

有效 key（`api_keys.revoked_at IS NULL`）→ `X-Authed-Authed: true`，跳过匿名限流。  
无效 / 已吊销 / 缺失 → `X-Authed-Authed: false`，走匿名 30 rpm。

key 由运维 CLI 管理，见 [部署方案](../deploy/random-name-api.md)。

## 限流

匿名令牌桶（`internal/ratelimit`）：
- 容量 = `RATE_LIMIT_BURST`（默认 = RPM）
- 速率 = `RATE_LIMIT_RPM / 60` tokens/sec
- 仅 `authed=false` 扣桶；带有效 key 完全跳过
- **所有** 匿名 `/api/*`（含 help/health）均扣桶

超额：
```
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 2
X-RateLimit-Limit: 30
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1783948938
X-Authed-Authed: false

{"error":"rate_limited","message":"请求过频, 请稍后再试。详见 GET /api/help"}
```

## 错误响应格式

```json
{"error":"<code>","message":"<说明>。详见 GET /api/help"}
```

`message` 末尾固定附加 `。详见 GET /api/help`。
