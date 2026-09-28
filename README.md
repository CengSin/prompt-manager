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
