# 提示词库

本机页面，用来保存自己贴上的提示词，以及一个可选的示例链接。

## 启动

```bash
go run ./cmd/prompt-manager
```

浏览器打开 http://127.0.0.1:8787

数据库默认写在 `./data/prompts.db`。可以用环境变量改端口和文件位置：

```bash
PORT=8787 PROMPT_MANAGER_DB=./data/prompts.db go run ./cmd/prompt-manager
```

`PROMPT_MANAGER_DB` 是数据库文件路径。`PORT` 是端口。进程只监听 `127.0.0.1`。

抽词和嵌入的模型写在 `./data/config.json`。这个文件不从环境变量读取，里面有 API key，不要提交进仓库。`baseUrl` 留空时用 `https://openrouter.ai/api/v1`。`apiKey` 和 `model` 需要自己填。某一边缺 `apiKey` 或 `model` 时，那一边不发请求。

```json
{
  "term": { "baseUrl": "", "apiKey": "", "model": "" },
  "embed": { "baseUrl": "", "apiKey": "", "model": "" },
  "astra": {
    "apiEndpoint": "https://DATABASE_ID-REGION.apps.astra.datastax.com",
    "applicationToken": "",
    "keyspace": "default_keyspace",
    "collection": "prompt_vectors"
  },
  "similarityMin": 0.35,
  "shortQuerySimilarityMin": 0.60,
  "meaningLimit": 5
}
```

`term` 是抽词，`embed` 是嵌入。`similarityMin` 是意思相近的最低分，`meaningLimit` 是最多显示几条。

去掉首尾空白后，长度不超过 4 个 Unicode 字符的非空查询使用更严格的语义门槛：`similarityMin` 和 `shortQuerySimilarityMin` 中的较大值。`shortQuerySimilarityMin` 默认是 0.60，可以在本地配置中调整。更长的查询继续使用 `similarityMin`。这个规则只过滤语义结果，字面匹配不受影响；列表页和详情页使用相同规则。

## Astra DB 向量存储

向量写入 Astra DB Serverless 的向量集合，语义搜索通过 Data API 完成。原文、链接、派生词和处理状态仍保存在本地 SQLite。`astra.apiEndpoint` 使用数据库详情页的 API Endpoint，不能使用组织控制台链接；`applicationToken` 填入具有集合创建、读写权限的 Application Token。`keyspace` 和 `collection` 留空时分别使用 `default_keyspace` 和 `prompt_vectors`。每个提示词库应使用独立集合。

集合第一次使用时自动创建，采用 cosine，维度从实际嵌入向量读取。已有集合必须具有相同维度和配置。更换为不同维度的嵌入模型时，需要配置新集合并重新生成向量；不要直接复用旧集合。嵌入仍由 `embed` 指定的模型生成。Astra 返回的 cosine 分数转换为原有的余弦分数，因此 `similarityMin` 的含义保持不变。

启动时自动上传已有 SQLite 向量；每条提示词上传成功后才删除对应的本地向量，迁移失败会保留尚未迁移的向量供下次重试。也可以单独迁移：

```bash
go run ./cmd/prompt-manager -migrate-vectors
```

修改正文或删除提示词时同步删除 Astra 中的对应向量；只修改示例链接不重新生成向量。查询按当前正文版本、模型和维度过滤，取每条提示词得分最高的向量，保留原文高亮，排除字面命中后再应用结果上限。缺少 Astra 配置或查询失败时，字面搜索仍可用；新向量写入失败会进入失败列表，可重试。Token 仅保存在忽略提交的 `data/config.json` 中。

## 搜索外语提示词

可以在搜索框输入中文整句描述，找回意思相近的外语提示词。页面先列出原文或派生词直接命中的结果，再在「意思相近」下列出达到相似度门槛的其他结果；同一条提示词不会重复出现。搜索结果和详情页始终展示并高亮保存的原文，不会生成或保存译文。

跨语言找回依赖支持多语种的嵌入模型，以及提示词已成功生成向量。结果质量会随模型和 `similarityMin` 而变化。每个不同的非空查询首次使用时会发送嵌入请求，查询向量在进程内缓存 15 分钟，最多 256 条；相同查询的并发请求合并，同一查询的详情页也复用缓存。失败请求不缓存，重启进程会清空缓存。搜索结果不缓存，因此正文修改、删除和派生完成后仍按当前数据查询 Astra。日志记录嵌入耗时、缓存命中、整次搜索耗时以及 Astra 查询次数和耗时，不记录查询原文或 Token。模型未配置或查询请求失败时，原文字面搜索仍可用。


## Cloudflare 部署

线上地址：https://prompts.cengsin.ccwu.cc 。按当前配置，所有访客都可以浏览、新增、修改和删除提示词。

`cloudflare/` 是 Cloudflare Workers 入口，保留原有页面和搜索规则；原文、关键词、别名和处理状态存储在 D1 `prompt-manager`，向量存储在 Astra 的独立集合 `prompt_vectors_cloudflare`。本机 Go 服务仍使用 SQLite 和 `prompt_vectors`；两份提示词库独立，后续修改不会自动互相同步。

保存原文后，`prompt-manager-derive` Workflow 在后台提取词语、分批生成嵌入并写入 Astra，完成后将当前正文版本标记为 ready。失败记录可在网页中重跑。云端查询嵌入缓存存储在 D1，保留 15 分钟、最多 256 个查询；短查询使用相同的 0.60 阈值。

配置与凭据：

- `cloudflare/wrangler.jsonc` 保存 Worker、D1、Workflow 和自定义域名配置。
- Worker Secret `APP_CONFIG` 是与本机 `data/config.json` 同结构的 JSON，`astra.collection` 使用云端集合；凭据不进入源代码和页面。
- 初始迁移包括 3 条原文、45 个关键词、50 个别名和 78 个 4096 维向量。迁移快照和 SQL 位于忽略提交的 `data/cloudflare-*` 文件中；导入 SQL 仅在初次迁移时执行，不能重复插入现有数据。

```sh
cd cloudflare
npm install
npm test
npx wrangler types
npx wrangler deploy --dry-run
npx wrangler deploy
```

重新配置云端凭据时，将新的 JSON 写入本机受保护的文件，再通过标准输入更新 Secret：

```sh
npx wrangler secret put APP_CONFIG < ../data/cloudflare-config.json
```

新建环境时先创建 D1、更新配置中的数据库 ID，并执行 `npx wrangler d1 execute prompt-manager --remote --file schema.sql`。网页写操作检查同源 Origin/Referer；这是防止其他网站代替访客提交表单，不限制公开访问。
