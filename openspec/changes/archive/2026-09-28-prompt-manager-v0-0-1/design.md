## Context

仓库里还没有应用代码。本机已有 Go 1.27，行为要求见 `specs/prompt-library/spec.md`。动机见 `proposal.md`。

这是一个给人自己用的本机库：贴入大段提示词，偶尔带一个作品链接，之后按原文里的字句找回。提示词里经常含有 HTML、脚本和 Markdown，页面必须把它们当成文字。

## Goals / Non-Goals

**Goals:**

- 一个只监听本机的进程，一个 SQLite 文件，打开浏览器就能完成规格里的全部行为。
- 原文与链接的校验、搜索和排序集中在一处，页面只负责提交和展示。
- 用测试锁住规格里的场景，包括中文子串、拉丁字母大小写、链接不参与匹配，以及提示词中的 HTML 不被执行。

**Non-Goals:**

- 视觉品牌、动效或多页营销式界面。页面只求长文可读。
- 账号、同步、多用户、模型调用、抓取 example URL。
- 独立的全文检索引擎或向量索引。

## Decisions

### 本机页面，而不是命令行

长提示词要整段阅读，example URL 要点开。用浏览器页面承载列表、阅读、表单和删除确认。

备选是命令行。它能存和搜，但阅读大段原文和点开链接都更别扭，所以不采用。

### Go 标准库 HTTP 与 `html/template`

用 Go 1.27 写一个进程：`net/http` 提供页面，`html/template` 渲染。模板默认转义，直接满足「提示词按字面展示」。界面文案用中文。

备选是 Node 上的单页应用。它多一套构建和转义责任，对这个只有表单和列表的库没有好处。

进程只绑定 `127.0.0.1`。默认端口 `8787`，可用环境变量 `PORT` 覆盖。数据文件默认 `./data/prompts.db`，可用 `PROMPT_MANAGER_DB` 覆盖。

变更请求只接受来自本站页面的提交：用 POST，并核对 `Origin`（没有 `Origin` 时核对 `Referer`）是否指向本服务。这样本机上的其他网页不能替用户改库。

### SQLite，纯 Go 驱动

用 `database/sql` 和 `modernc.org/sqlite`。不启用 CGO，本机不用另装 C 编译器。第一次打开数据库时创建表，没有旧数据需要迁移。

```text
prompts
  id            TEXT PRIMARY KEY   -- UUID
  body          TEXT NOT NULL      -- 用户贴入的原文，原样保存
  body_search   TEXT NOT NULL      -- 仅用于搜索的小写副本
  example_url   TEXT               -- 没有链接时为 NULL
  created_at    TEXT NOT NULL      -- UTC RFC3339，决定列表顺序
  updated_at    TEXT NOT NULL
```

列表按 `created_at` 降序。改正只更新 `body`、`body_search`、`example_url` 和 `updated_at`。

备选是一个 JSON 文件。能存原文，但「最新在前」和子串搜索要自己扫整份文件，并发写入也更容易损坏。个人库的规模用 SQLite 足够，也不必上独立数据库服务。

### 用小写副本做子串搜索

写入时用 Go 的 Unicode 小写规则生成 `body_search`。查询先去掉首尾空白；空白查询不加过滤，直接返回整份列表。否则用 SQLite `instr(body_search, 小写查询)` 做连续子串匹配。`example_url` 不进入 `body_search`。

SQLite 自带的 `lower()` 只处理 ASCII，中文不受影响，但把大小写规则留在 Go 里，测试可以同时覆盖 `剪纸` 和 `Paper-cut`。不用 `LIKE`，因为查询里的 `%` 和 `_` 会被当成通配符。不用 FTS5，因为它的分词会把中文切碎，和「连续字句」不一致。

列表摘要只用于展示：取原文开头约 120 个字符，换行改成空格，过长则加省略号。详情页用 `white-space: pre-wrap` 显示完整原文。

### 链接只校验，不访问

example URL 去掉首尾空白后，必须能解析成带 host 的绝对 `http` 或 `https` URL，否则整次提交失败，已保存的记录不变。通过后按去掉首尾空白的字符串保存。服务端不创建出站 HTTP 客户端，页面上的链接使用保存值作为 `href`。

校验失败时回到原表单，保留用户刚贴上的原文和链接，并说明原因。避免一条长提示词因为链接少写了协议就被清掉。

### 删除要经过确认页

详情页进入确认页。确认页的提交才执行删除，取消则回到详情。不依赖浏览器脚本，刷新或直接打开确认页也不会删掉记录。

### 代码落点

```text
cmd/prompt-manager/          进程入口、端口和数据库路径
internal/prompt/             原文、链接、摘要、搜索折叠的规则
internal/store/              SQLite 读写
internal/web/                路由、表单、模板
```

模块路径用 `prompt-manager`。

## Risks / Trade-offs

- [链接日后失效或帖子被删] → 本版接受。字段只负责记住地址，不抓取、不存快照。
- [SQLite 的 `lower()` 与规格不一致] → 搜索只用 Go 生成的 `body_search`，并给中文和大小写各写测试。
- [`body` 与 `body_search` 写歪] → 只通过一个保存函数写入这两列，测试断言改正后的搜索结果跟着变。
- [库变大后 `instr` 变慢] → 个人收藏的规模可以接受。本版不引入全文引擎；真的变慢再单独做变更。
- [提示词里的 HTML 被浏览器执行] → 全部经 `html/template` 输出，测试覆盖含 `<script>` 的原文。
- [本机其他网站向 localhost 提交表单] → 只绑定环回地址，并拒绝来源不是本站的变更请求。

## Migration Plan

没有旧版本。首次启动执行 `CREATE TABLE IF NOT EXISTS`，不会改写已有原文。

回退：停掉进程，删掉 `PROMPT_MANAGER_DB` 指向的文件（默认 `./data/prompts.db`）。本变更不连接外部服务。

部署就是在仓库里启动这个进程，并用浏览器打开 `http://127.0.0.1:8787`。
