# 前端（静态 HTML + ES modules）

当前为嵌入式静态前端（`go:embed`），按「客户端」拆目录，每个界面独立文件。

```text
static/
  app.html                 # 工作台壳（侧栏 + 视图挂载点）
  index.html               # 登录页
  assets/
    app.css
    app.js                 # 壳逻辑：导航 / 挂载客户端界面
    auth.js
    shared/                # 共用 api / util
    clients/
      kafka/
        connections.html   # 连接列表界面
        connections.js
        topics.html        # Topic 列表界面
        topics.js
        messages.html      # 消息消费预览
        messages.js
      es/
        connections.html   # 连接列表
        connections.js
        indices.html       # 索引
        indices.js
        docs.html          # 文档检索
        docs.js
      redis/
        connections.html   # 连接列表
        connections.js
        details.html       # 连接详情抽屉
        details.js
        cache.html
        cache.js
```

约定：

- 新增客户端：在 `assets/clients/<name>/` 建文件夹，每界面一对 `.html` + `.js`，并在 `app.js` 的 `screens` 注册
- 界面模块导出 `mount(root, ctx, params)`，返回 `{ unmount() }`
- 生产：静态文件由后端 `go:embed` 嵌入单二进制
