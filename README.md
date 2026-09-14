# Hello Coder

当前版本 **0.1.0**（见 [`internal/version/VERSION`](internal/version/VERSION)，变更记录见 [CHANGELOG.md](CHANGELOG.md)）。

中间件工具集（Kafka / Elasticsearch / Redis）。后端 Go 单二进制，连接配置存 SQLite；前端为嵌入式静态 HTML。

- **Windows**：双击 exe，独立窗口打开（WebView2），**无需登录**；关窗口即退出
- **Linux**：启动可执行文件，用浏览器访问，**需登录/注册**

Windows 桌面模式直接进入工作台；Linux（以及 Windows 的 `start-server.bat` 局域网服务）需在登录页注册账号。连接密码传输用 RSA-OAEP，落库用 AES-GCM。

## 环境要求

- Go **1.25+**
- Windows 桌面模式需要 WebView2（Win10+ 通常已自带 Edge 运行时）

## 目录结构

```text
hello-coder/
├── cmd/server/              # 进程入口
├── internal/
│   ├── app/                 # 组装配置、DB、HTTP
│   ├── auth/                # JWT
│   ├── config/              # 进程配置（端口、数据目录）
│   ├── middleware/          # 通用中间件
│   ├── model/               # 用户与各工具连接模型
│   ├── modules/             # 可插拔工具模块
│   │   ├── auth/            # 注册 / 登录
│   │   ├── kafka/           # Kafka 客户端
│   │   ├── es/              # Elasticsearch 客户端
│   │   └── redis/           # Redis 客户端
│   ├── secret/              # 传输 / 落库加解密
│   ├── desktop/             # Windows 独立窗口（WebView2）
│   ├── server/              # Gin 路由与 HTTP Server
│   ├── store/               # SQLite / GORM
│   └── version/             # 发布版本号（VERSION）
├── web/                     # 静态前端（go:embed）
├── data/                    # 运行时数据（不入库，见 .gitignore）
├── docs/                    # 文档
└── scripts/                 # 构建脚本
```

扩展新工具：在 `internal/modules/<name>` 实现 `modules.Module`，并在 `internal/server/router.go` 注册。

## 数据表

| 表 | 说明 |
| --- | --- |
| `user` | 用户：主键、用户名、密码（bcrypt） |
| `kafka_conn` | Kafka 连接 |
| `es_conn` | Elasticsearch 连接 |
| `redis_conn` | Redis 连接 |

启动时若 `data/hello-coder.db` 不存在则自动创建并迁移表结构。`data/` 含数据库、传输私钥、日志和桌面窗口配置，**不要提交到 Git**。

## 快速启动

开发（默认按当前系统：Windows 独立窗口，Linux 浏览器）：

```bash
go run ./cmd/server
```

调试前端时建议用服务模式，浏览器打开页面：

```bash
HELLO_CODER_MODE=server HELLO_CODER_STATIC_DIR=./web/static go run ./cmd/server
```

或用 VS Code / Cursor：运行配置 **Hello Coder**（服务模式）/ **Hello Coder (desktop)**。

- 登录页：http://localhost:10240/（仅 `server` 模式需要）
- 工作台：http://localhost:10240/app.html（`desktop` 模式直接进入；`server` 登录后跳转）
- 健康检查：`GET http://localhost:10240/api/health`（含 `version`）
- 查看版本：`go run ./cmd/server --version`

左侧可收缩功能栏（Kafka / Elasticsearch / Redis）。

## 打包

```powershell
./scripts/build.ps1
```

产物在 `dist/`（已 gitignore）：

| 包 | 用法 |
| --- | --- |
| `hello-coder-0.1.0-windows-amd64.zip` | 解压后双击 `hello-coder.exe`，独立窗口 |
| `hello-coder-0.1.0-linux-amd64.tar.gz` | `chmod +x hello-coder && ./hello-coder`，浏览器访问 |

Windows 若要以服务方式给局域网访问，用包内 `start-server.bat`。Linux systemd 示例见 `scripts/hello-coder.service`。可执行文件支持 `--version`。

环境变量见 [`.env.example`](.env.example)：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `HELLO_CODER_MODE` / `--mode` | Windows=`desktop`，其它=`server` | `desktop` 独立窗口且免登录；`server` 仅 HTTP 且需登录 |
| `--version` | — | 打印版本后退出 |
| `HELLO_CODER_AUTH` | 跟随 mode | `on`/`off` 强制开/关登录（一般不用改） |
| `HELLO_CODER_ADDR` | desktop=`127.0.0.1:10240`，server=`:10240` | 监听地址 |
| `HELLO_CODER_DATA_DIR` | `./data` | SQLite / 密钥 / 日志目录 |
| `HELLO_CODER_STATIC_DIR` | （空=embed） | 开发时指向 `web/static`；生产留空用嵌入资源 |
| `HELLO_CODER_LOG_LEVEL` | `info` | 日志级别 |
| `HELLO_CODER_JWT_SECRET` | 开发默认值 | JWT 密钥（生产务必修改） |
| `HELLO_CODER_JWT_TTL_HOURS` | `4` | JWT 有效期（小时） |
| `HELLO_CODER_DATA_KEY` | （派生自 JWT） | 连接密码 AES 落库密钥材料（生产务必单独设置） |

更多见 [docs/技术选型.md](docs/技术选型.md)、[docs/数据库设计.md](docs/数据库设计.md)。

## 免责声明

本软件按「现状」提供，**不附带任何担保**。连接并操作 Kafka / Elasticsearch / Redis 可能造成数据丢失、误写或凭证泄露；使用风险由你自行承担。作者与贡献者不对因此产生的任何损失负责。

你必须仅连接有权访问的系统。Windows 桌面模式默认免登录；`server` 模式勿使用默认密钥，也勿对公网裸暴露。完整条款见 [DISCLAIMER.md](DISCLAIMER.md)。使用本软件即视为同意该声明。

## 许可证

[Apache License 2.0](LICENSE)。版权与署名见 [NOTICE](NOTICE)。
