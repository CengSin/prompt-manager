## Context

现有搜索在 `internal/store.Search` 里用 `instr(body_search, 小写查询)` 做整段连续子串。列表摘要由 `prompt.Excerpt` 取原文开头约 120 个字符。全文在 `detail.html` 里放进 `<pre>`，经 `html/template` 转义。数据库只有 `prompts` 一张表，进程只监听 `127.0.0.1`，`database/sql` 连接数上限为 1。行为见 `specs/prompt-library/spec.md` 与 `specs/prompt-derivation/spec.md`。动机见 `proposal.md`。

## Goals / Non-Goals

**Goals:**

- 字面匹配、向量打分和高亮共用同一套命中区间，打开详情时按查询重算，不信任链接里带来的区间。
- 保存和改正的 HTTP 响应不等待模型。派生在进程内排队，失败或未完成时不编造「意思相近」。
- 测试不访问外网。模型用可替换的接口，脚本化响应走和正式路径相同的校验与入库。

**Non-Goals:**

- 勾选气质词、起草新提示词。
- 引入独立向量数据库或全文引擎。个人库的向量在进程内做余弦比较。
- 把示例链接、用户自建分类或标签纳入匹配。

## Decisions

### 派生物单独存放，原文仍只通过现有保存函数写入

新增三张表，不改 `prompts.body` 的写入路径。`prompts` 增加 `derive_generation` 整数，默认 0，以及 `derive_status`（`none`、`pending`、`ready`、`failed`）、`derive_error`、`derive_failed_at`。改正原文时先加一，删掉该条的派生物，清掉失败原因，再把新的一代排进后台。只改示例链接时不增加代数，也不改派生物和失败状态。删除提示词时显式删掉派生物和失败记录，不依赖外键级联。

```text
prompt_terms
  id, prompt_id, kind, phrase, start, end, generation
  kind: keyword | style | medium | composition | lighting | genre

prompt_spellings
  term_id, spelling

prompt_vectors
  id, prompt_id, source, source_id, start, end, model, dim, vector, generation
  source: section | term
```

`start` 与 `end` 是原文 Go 字符串的字节下标，并且落在符文边界上。气质词的种类用 `style`、`medium`、`composition`、`lighting`、`genre` 五者之一，对应风格、媒介、构图、光线、体裁。关键词的 `kind` 为 `keyword`。分辨率、时长、帧率即使被模型标成气质词，入库前也丢掉；判断用明确的形态（如 `1920×1080`、`1920x1080`、`24 fps`、`45秒`、`9分30秒`），而不是把所有数字都丢掉。短语必须是原文的连续子串，对不上的丢掉。归并写法可以不是原文里的字，挂在对应气质词上。

向量以 little-endian `float32` 存在 `vector` 里。个人库的规模用全表扫描余弦即可。维度或模型标识和当前查询不一致的向量不参与打分。

备选是把向量放进 `prompts` 的一列。一条提示词有多段、多个词，一列装不下，所以不用。

### 后台用一代数防旧结果回写

`internal/web` 在成功保存或改正原文后，把 `(id, generation)` 送进进程内的单工人队列。工人读出原文，调用派生接口；写回前再读代数，不一致就丢弃。SQLite 仍是单连接，工人和请求共用 `*store.Store`。

抽词和嵌入的配置分开，都写在 `./data/config.json`，启动时读取。不从环境变量读取 base URL、API key、model、相近线或条数上限。监听端口和数据库路径仍用现有的 `PORT` 与 `PROMPT_MANAGER_DB`。文件不存在、JSON 无法解析，或某一边的 API key、model 为空，这一边视为未配置：不发送对应请求，状态保持 `none`，不记为失败。两边都齐了才把提示词排进队列。

```json
{
  "term": { "baseUrl": "", "apiKey": "", "model": "" },
  "embed": { "baseUrl": "", "apiKey": "", "model": "" },
  "similarityMin": 0.35,
  "meaningLimit": 5
}
```

`baseUrl` 留空时用 `https://openrouter.ai/api/v1`。API key 和 model 没有默认值，也不再默认某个模型名。`data/config.json` 含有 API key，不提交进仓库。相近线和条数上限缺省时分别是 `0.35` 和 `5`。测试注入自己的门槛、上限和端点，不读这个文件。

- 抽词：`POST {baseURL}/chat/completions`，OpenAI 兼容的 JSON（`model`、`messages`）。模型正文取完成消息的 `content`。只发送原文，不发送示例链接。要求 JSON：关键词、气质词（种类、原文短语、其他写法）。
- 嵌入：`POST {baseURL}/embeddings`，OpenAI 兼容的 JSON（`model`、`input`）。不请求模型列表，也不擅自挑选模型。不发送 `dimensions`，向量长度以响应为准。

备选是继续用环境变量，或让抽词走固定的 `/v1/responses`。配置要落在文件里，网关认的是 `/chat/completions`，所以用上面的文件和路径。

抽词和嵌入都成功才在一个事务里写入，并把状态设为 `ready`、清掉失败原因。任一步失败则不写入半套派生物，状态设为 `failed`，记下时间和原因。原因写入前去掉其中出现的 API key。原文保持可按连续字句查找。

重跑把状态改回 `pending` 并清掉失败展示，使该条离开失败页；工人仍按当前代数执行。再次失败则写回新的原因和时间。查询时的嵌入使用配置文件里的同一套 base URL、API key 和 model。

分段在调用嵌入之前完成：按空行切开；只起引出作用的标题行（以 `：` 或 `:` 结尾，或整行是 `【…】`）并入下一段。空段不嵌入。每个关键词、每个气质词再各嵌入一次，文本用其原文短语，不用归并写法。

备选是把所有关键词拼成一个向量。那会把多个词平均成一个点，和已确认的「每个词各算一个、取最高分」不一致，所以不用。

### 搜索先字面，字面为空才算查询向量

`strings.Fields` 切开查询。每一段都要在同一条的 `body_search`、关键词短语或任一气质词写法的小写形式里连续出现。拉丁字母沿用 `prompt.SearchText` 的 Unicode 小写。字面命中按 `created_at DESC, id DESC`，与现在的列表顺序相同。有字面命中时不计算查询向量，也不显示「意思相近」。

没有字面命中时，把整句查询嵌入一次，不先抽词。一条提示词的分数是它所有可用向量里的最高余弦。只保留分数大于等于配置文件里 `similarityMin`（缺省 `0.35`）的条目，最多 `meaningLimit`（缺省 `5`）条，按分数从高到低。这两个数是所选嵌入模型的起步配置。没有向量、嵌入失败或没有过线，都走现有的「没有匹配」。

关键词短语本身是原文子串，字面阶段对它的匹配多半已被原文覆盖。气质词的其他写法不是原文子串，必须单独参加字面匹配。

### 失败页只列出 `failed`

`GET /prompts/failed` 列出 `derive_status = failed` 的提示词，每条给出摘要、`derive_error` 和 `derive_failed_at`。`none`、`pending`、`ready` 不出现。没有失败项时，页面说明当前没有失败。库列表只在失败数大于 0 时链到这一页。

`POST /prompts/{id}/derive` 重跑一条，`POST /prompts/failed/retry` 重跑当前全部失败项。两条都走现有的 POST 来源校验。路由使用字面路径 `/prompts/failed`，避免被 `/prompts/{id}` 吃掉。重跑时若抽词或嵌入的配置已经缺失，不发出请求，该条留在失败页，原因保持为上一次的失败。

### 高亮用同一组区间，并且先转义再包标记

字面命中：查询段在原文里出现时，标出每一处（大小写按小写副本对齐回原文）。查询段只因归并写法命中时，标出该气质词记录的原文区间的每一处出现，不把查询词插进原文。「意思相近」只标最高分那个向量自带的区间。

列表摘要仍以约 120 个字符为上限。无查询时从开头截。有命中时从第一处高亮略前开始截，使高亮落在摘要内。详情链接带上当前 `q`。详情页用这个 `q` 对这一条重算区间。没有 `q` 时全文不高亮。

页面不能把原文交给模板当 HTML。先把每一段 `html.EscapeString`，再在区间上包 `<mark>`，最后作为 `template.HTML` 输出。含 `<script>` 的原文在高亮后仍是转义文本。

### 代码落点

```text
internal/prompt/     分段、摘要窗口、小写与区间查找
internal/derive/     模型请求、JSON 校验、分辨率等形态过滤
internal/search/     字面匹配、余弦、命中区间
internal/store/      三张新表、代数、失败状态、按代写入和删除
internal/web/        队列、列表两组结果、高亮渲染、失败页和重跑
```

## Risks / Trade-offs

- [模型没有抽出「垂直构图」，或把交付规格标成气质词] → 提示词里写明五类和禁止项；形态过滤丢掉分辨率、时长、帧率。自动化测试用脚本化响应走过同一校验，CI 不调用外网。线上抽出的质量要靠这个提示词，不靠测试替模型保证。
- [旧的派生结果在改正后写回] → 写回前核对 `derive_generation`。改正原文时先删掉旧派生物，间隙里只有字面查找。
- [默认相近线不适合实际嵌入模型] → 改配置文件里的 `similarityMin` 和 `meaningLimit`。规格不写死数字。
- [高亮把原文里的标签变成页面结构] → 只拼接已转义的片段，并用含 `<script>` 的测试锁住。
- [嵌入模型更换后旧向量不可比] → 向量记下模型标识和维度；不一致的不参与打分，并在启动时把这些提示词重新排队。
- [原文被送到抽词模型和嵌入端点] → 只有配置文件里该边的 API key 和 model 都有时才发送。两边都不带示例链接。失败原因里不保留 API key。缺文件或缺字段时不把整库记为失败。环境变量里的模型地址和密钥不生效。

## Migration Plan

已有的 `prompts` 行在打开数据库时补上 `derive_generation`、`derive_status`、`derive_error`、`derive_failed_at`（缺列则 `ALTER TABLE`）和新表。不改写已有原文。已有行的代数为 0、状态为 `none`，没有派生物，搜索与现在的连续字句一致。配置文件里抽词和嵌入的 API key、model 都有时，启动后为尚无派生物且未在失败页上的行补算一次。此前写在环境变量里的模型配置不再读取；要发送请求，把对应字段写进 `./data/config.json`。

回退：停掉进程，用回上一版程序。新表和新列可以留下，旧程序不读它们。不需要改写原文。

## Open Questions

无。相近线和条数上限留在配置文件里，等换嵌入模型时再改文件，不改变规格里的行为。
