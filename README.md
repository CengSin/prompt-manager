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
  "similarityMin": 0.35,
  "meaningLimit": 5
}
```

`term` 是抽词，`embed` 是嵌入。`similarityMin` 是意思相近的最低分，`meaningLimit` 是最多显示几条。
