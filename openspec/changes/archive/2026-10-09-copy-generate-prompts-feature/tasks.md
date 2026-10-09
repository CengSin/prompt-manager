## 1. 写作配置

- [x] 1.1 在 `derive.LoadFile` 中读取 `draft.baseUrl`、`draft.apiKey`、`draft.model`，并提供 `DraftReady`。`baseUrl` 留空时用 `https://openrouter.ai/api/v1`，API key 和 model 没有默认值，且不从环境变量读取。验证：`internal/derive` 测试覆盖自定义端点、空 `baseUrl`、缺 model，以及只在环境变量里提供密钥时 `DraftReady` 仍为假。
- [x] 1.2 Cloudflare 的 `configuration` 给 `draft` 补上同样的默认 base URL，`redact` 去掉 `draft.apiKey`。验证：`cloudflare/test` 中缺 `draft` 的旧配置仍可解析；含密钥的失败文本不再出现该密钥。

## 2. 范例检索

- [x] 2.1 为本机增加独立于 `Store.Match` 的范例检索。需求整句嵌入一次，门槛只用 `similarityMin`，候选包含字面也能命中的当前版本，没有向量的跳过，按相似度取最多 3 条全文。嵌入未配置或失败时返回空范例，不中断后续写作。验证：存储或 web 测试覆盖四条里取过线的三条、五条只留最高的三条、字面命中仍入选、四个字符以内仍用普通门槛、缺向量被跳过、嵌入失败得到空范例。
- [x] 2.2 在 Cloudflare 用全部 `ready` 的当前提示词调用 `meaningSearch`，上限 3，门槛为 `similarityMin` 而不是 `queryMinimum`。嵌入失败时范例为空。验证：`cloudflare/test` 覆盖与 2.1 相同的选择结果，并且不调用搜索页那条排除字面命中的路径。

## 3. 写作请求

- [x] 3.1 在 `derive.Config` 上新增写作方法，向 `{baseUrl}/chat/completions` 发送需求作为题材、选中全文只作为写法范例，不发送示例链接。只取完成消息正文并去掉首尾空白，内部换行保留。空白结果视为失败。未配置时不发请求。失败文本去掉 API key。验证：脚本化 HTTP 测试断言路径、题材与范例的区分、示例链接不在请求里、首尾空白被去掉、空白响应失败、缺 model 时零请求。
- [x] 3.2 在 Cloudflare 用 `draft` 端点发送同一含义的请求，并复用现有响应读取和脱敏。验证：`cloudflare/test` 断言请求正文包含需求、把范例标成写法、不含示例链接，空白响应不进入保存页。

## 4. 复制和添加日期

- [x] 4.1 增加固定的 `/copy.js`。脚本只读取对应隐藏文本框的值并调用 `navigator.clipboard.writeText`，成功显示 `已复制`，失败显示 `没复制成`，不使用 `innerHTML`。`bottom.html` 引入它。验证：请求 `/copy.js` 得到这段脚本，响应里没有 `innerHTML`。
- [x] 4.2 本机列表、字面结果和「意思相近」显示 `添加于 YYYY-MM-DD`，用 `Asia/Shanghai` 从 `created_at` 转换。详情页不显示添加日期。每条列表和详情都有 `复制`，隐藏文本框里是转义后的库存全文。含 `<script>` 和 `</textarea>` 的原文不能执行，也不能提前结束文本框。验证：页面测试断言 `2026-09-28T16:00:00Z` 显示 `添加于 2026-09-29`、`2026-09-28T15:59:59Z` 显示 `添加于 2026-09-28`，改正后日期不变；摘要短于全文时文本框含全文且不含示例链接；详情没有 `添加于`；正文中的 `<script>` 仍是转义文字。现有整页拒绝 `<script` 的断言改为只约束提示词正文，同时允许 `/copy.js`。
- [x] 4.3 Cloudflare 的列表、两类搜索结果和详情做同样的日期与复制标记。CSP 增加 `script-src 'self'`，其余指令保持，并提供同一个 `/copy.js`。验证：`cloudflare/test` 覆盖 4.2 的页面结果，CSP 含 `script-src 'self'`，提示词里的 `<script>` 仍是文字。

## 5. 草稿页

- [x] 5.1 本机把 `GET /prompts/draft` 和 `POST /prompts/draft` 注册为固定路径，页头提供 `写一条`。空需求显示 `要写的内容必填` 且不调用模型。未配置显示 `还不能生成` 且不发写作请求。成功时渲染现有新提示词表单，正文是草稿，示例链接为空，显示 `这是生成的草稿，改完再保存`；没有范例时再显示 `这次没有参考`。这次响应不入库、不入派生队列。写作失败或空白时留在草稿页，保留需求，显示 `没有生成出来`，页面不含 API key。验证：`internal/web` 测试覆盖这些分支，并断言成功的草稿响应之后库的条数不变；再提交该表单才新增一条，且不保存原来的需求。
- [x] 5.2 Cloudflare 实现同样的路径、文案和「先草稿、后保存」顺序，沿用现有的同源 POST 校验。验证：`cloudflare/test` 覆盖 5.1 的分支，包括嵌入失败时仍生成并显示 `这次没有参考`，以及未保存就离开时库不变。

## 6. 说明和收尾

- [x] 6.1 README 的配置示例和说明写入 `draft` 的 base URL、API key、model，并写明缺 API key 或 model 时不生成。验证：说明里同时有 `draft` 和「不生成」，且没有为写作端点新增环境变量名。
- [x] 6.2 跑通本机和 Cloudflare 的测试。验证：`go test ./...` 退出码为 0，`cloudflare` 目录下 `npm test` 退出码为 0。
