# 随机名字 API — 接口文档

Base URL (本地开发): `http://localhost:8080`

## 公共响应头

| Header                | 含义                              |
|-----------------------|----------------------------------|
| `Content-Type`         | `application/json; charset=utf-8` |
| `X-Authed-Authed`       | `true` / `false`. 是否携带有效 key |
| `X-RateLimit-Limit`     | 匿名 RPM 上限 (默认 30)           |
| `X-RateLimit-Remaining` | 本分钟剩余请求数                   |
| `X-RateLimit-Reset`      | 当前 epoch 秒, 下次重置            |
| `Retry-After`           | 仅 429 时, 等待重试秒数            |

## 端点

### `GET /api/health`

免认证, 免限流 (仍走链路但不计桶).

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

匿名限流 30 req/min. 有效 API key 不限.

**Query 参数**:

| 名             | 类型             | 必填 | 默认       | 说明 |
|----------------|------------------|------|------------|------|
| `surname`        | string           | 否   | "张"      | 姓氏 (1 个汉字) |
| `n`              | int (1..50)       | 否   | 5          | 返回个数, 上限 50 |
| `strategy`       | `weighted` \| `uniform` | 否 | `weighted` | `uniform` 无放回均匀; `weighted` softmax(score·α) 采样 |
| `source`          | string           | 否   | "wealth"  | 来源 id 或 label. 可选: wealth / academic / modern_people / imperial_exam / ancient_names 或 "财富论" / ... |
| `avoid`           | csv string       | 否   | 无        | 需避开的姓名/字, 多个以逗号分隔. 例如 `avoid=赵钱孙,刘强` |
| `must`            | csv string       | 否   | 无        | 姓名中必须出现的字. 如 `must=明` |
| `mustPosition`    | `any` \| `second` \| `third` | 否 | `any`     | `second` = 必须在名第二位; `third` = 必须在第三位 |
| `style`           | `any` \| `loud` \| `soft`   | 否 | `any`     | 风格相关筛选. `loud` 偏向二三四声收尾; `soft` 偏向一二声收尾 |
| `seed`            | int              | 否   | 随机       | seed 填入可复现 sampling 过程 |
| `alpha`           | float            | 否   | 0.15       | weighted 模式锐度. 越大高分越排他. 0.05 = 几近均匀 |

**响应 200**:
```json
{
  "query": {"surname":"姚","mustPosition":"any","style":"any","sourcePreference":"academic"},
  "source": {"id":"academic","label":"五道口","count":8627},
  "strategy": "weighted",
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

**错误**:

| HTTP | `error`                 | 含义                                 |
|------|-------------------------|-------------------------------------|
| 400  | `unknown_source`         | source id/label 不识别                |
| 400  | `source_empty`           | 来源有元数据但 DB 无候选行            |
| 405  | `method_not_allowed`     | 非 GET                               |
| 422  | `surname_not_in_char_db` | 姓氏不在字库 (例如罕见异体字)        |
| 429  | `rate_limited`           | 匿名超额, 见 `Retry-After` 头         |

### `GET /api/name/{fullName}`

匿名限流. 路径变量 `{fullName}` = 姓 + 2 字名 = 共 3 字.

**Query 参数**:

| 名     | 类型   | 必填 | 默认 | 说明 |
|--------|--------|------|------|------|
| `source` | string | 否   | 无  | 指定来源 id/label. 不传则按 priority 顺序查 5 个 source 找第一个匹配. |

**响应 200**:
```json
{
  "fullName": "姚悟移",
  "name": "悟移",
  "score": 103,
  "breakdown": { ... },
  "sources": ["五道口"],
  "sourceNames": ["张悟移(国家自然基金)"],
  "pinyin": ["wù","yí"],
  "tonePattern": "242",
  "semantic": "",
  "phonetic": "...",
  "reasons": [...],
  "explanation": "...",
  "found": true
}
```

**错误**:

| HTTP | `error`                | 含义                                       |
|------|------------------------|---------------------------------------------|
| 400  | `missing_name` / `invalid_name` / `unknown_source` | 路径缺名字 / 长度<3 / source 不识别 |
| 404  | `not_found`             | 5 source 均无该候选名                       |
| 422  | `surname_not_in_char_db`| 姓氏不被字库识别                            |
| 429  | `rate_limited`          | 匿名超额                                    |

## 认证

请求头二选一:
- `X-API-Key: <KEY>`
- `Authorization: Bearer <KEY>`

有效 key (`api_keys.revoked_at IS NULL`) -> `X-Authed-Authed: true` 且不受匿名限流.
无效 / 已吊销 / 缺失 key -> `X-Authed-Authed: false` 且走匿名 30 rpm.

key 由运维 CLI 管理, 见 [部署方案](../deploy/random-name-api.md) §Key 管理.

## 限流

匿名令牌桶:
- 容量 = `RATE_LIMIT_BURST` (默认 = RPM)
- 速率 = `RATE_LIMIT_RPM / 60` tokens/sec
- 仅匿名请求 (authed=false) 走限流. 带 key 完全跳过.

超额返:
```
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 2
X-RateLimit-Limit: 30
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1783948938

{"error":"rate_limited","message":"请求过频, 请稍后再试"}
```