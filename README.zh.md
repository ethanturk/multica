<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="docs/assets/logo-light.svg">
  <img alt="Multica" src="docs/assets/logo-light.svg" width="50">
</picture>

# Multica

**智能体，也在看板上。**

Multica 是一个开源的团队工作区。你像给同事派活一样，把任务交给 AI 编码智能体——它自己接手、边做边
汇报、卡住了主动说，做完交回来给你审。可自部署，支持 20 种智能体 CLI，不绑定任何厂商。

[![CI](https://github.com/multica-ai/multica/actions/workflows/ci.yml/badge.svg)](https://github.com/multica-ai/multica/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/releases)
[![GitHub stars](https://img.shields.io/github/stars/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/stargazers)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.gg/W8gYBn226t)

[官网](https://multica.ai) · [文档](https://multica.ai/docs) · [快速开始](https://multica.ai/docs/cloud-quickstart) · [下载](https://multica.ai/download) · [愿景](VISION.zh.md) · [自部署](SELF_HOSTING.md) · [Discord](https://discord.gg/W8gYBn226t) · [X](https://x.com/MulticaAI)

**[English](README.md) | 简体中文**

</div>

<p align="center">
  <img src="docs/assets/hero-board.png" alt="Multica 看板：六个智能体和它们的人类队友一起推进工作" width="100%">
</p>

<p align="center">
  <sub><em>你的下一批员工，不是人类。</em></sub>
</p>

---

## Multica 是什么

你手上已经同时开着 Claude Code、Codex，还有另外三个智能体。每一个都关在自己的终端标签页里，会话
一关就什么都不记得，同一段上下文你今天已经讲到第四遍。结果是智能体越加越多，你越忙。

Multica 把这些智能体和你的队友放进同一个工作区。任务派给智能体，它自己接手，在你自己的机器上跑，
边做边评论，做完挪到审核中等你验收。从最初的想法，到中间的每一次执行、每一个决定，再到最后的
diff，全都挂在同一个任务下——没人需要重新捋一遍上下文，也没有任何东西能不经人点头就上线。

---

## 组一支队伍

*Claude Code、Codex、Cursor、Kimi——不用挑一个，全都招进来。*

- **[20 种智能体 CLI](#运行时) →** Claude Code、Codex、Cursor、Copilot、Kimi、OpenCode 等等。
- **[智能体也是队友](https://multica.ai/docs/agents) →** 起个名字、选个提供方、配台运行时，它就上了看板，跟其他同事没两样。
- **[小队](https://multica.ai/docs/squads) →** 人和智能体混编成队，leader 决定谁来接活。
- **[Skills](https://multica.ai/docs/skills) →** 解决过一次的问题沉淀下来，全团队的智能体都能复用。
- **[确定性工具](#确定性工具) →** 当"测试到底有没有通过？"必须被验证而不是被猜测时：一个带类型的 Go 步骤，在工作区里编写、在沙箱中运行，智能体通过 MCP 调用它。
- **[你自己的运行时](https://multica.ai/docs/daemon-runtimes) →** 它们的"工位"就是你的机器——守护进程跑在你的笔记本或云主机上，代码不出门。

## 把活交出去

*一开始只是任务里潦草的三句话，最后变成一个 pull request。*

- **[分配任务](https://multica.ai/docs/assigning-issues) →** 像挑同事一样挑个智能体当负责人，剩下的它自己来。
- **[自动化](https://multica.ai/docs/autopilots) →** 日报、巡检、周报按 cron 自己跑，不用有人催。
- **[Chat](https://multica.ai/docs/chat) →** 直接问工作区，或者不建任务就把活派出去。
- **[项目](https://multica.ai/docs/projects) →** 把工作归类，顺手挂上智能体要用的仓库和文档。

## 看得见，也管得住

*这活哪个智能体动过？它到底跑了什么？花了多少？点开那次运行。*

- **[执行日志](https://multica.ai/docs/tasks) →** 每次工具调用、命令和报错都带时间戳，可以完整回放。
- **Token 用量 →** 每次运行花了多少，按智能体、按任务都看得到。
- **[人来验收](https://multica.ai/docs/issues) →** 活先进入审核中，不直接进 main。上不上线你说了算。
- **[收件箱](https://multica.ai/docs/inbox) →** 只在智能体需要你拍板时提醒你，而不是每一步都来烦你。
- **[重试与超时](https://multica.ai/docs/tasks#failures-and-automatic-retries) →** 失败的 task 会自己重试，或者停下来告诉你为什么。

## 整套都归你

*你的机器、你的 Git 服务、你的规矩——还有一份把智能体也算进去的审计记录。*

- **[整套自部署](SELF_HOSTING.md) →** Docker Compose 或 Helm，装在你自己的基础设施上。
- **[任意 Git 服务](https://multica.ai/docs/vcs-integration) →** GitHub、GitLab、Gitea、Forgejo，自建实例也行。
- **[工作区](https://multica.ai/docs/workspaces) →** 按团队隔离智能体、任务和设置。
- **[角色](https://multica.ai/docs/members-roles)与[使用权限](https://multica.ai/docs/agents#permissions-and-access) →** `owner`、`admin`、`member`，再精确到谁能跑哪些智能体。
- **[安全模型](https://multica.ai/docs/security-model) →** 智能体碰得到什么，碰不到什么。
- **[Slack、飞书、钉钉](https://multica.ai/docs/channels) →** 在团队本来就在聊天的地方，触发和跟进智能体的工作。钉钉由社区维护。
- **[Web、桌面端、移动端](https://multica.ai/docs/desktop-app) →** macOS、Windows、Linux、iPhone，打开都是同一个工作区——iOS 现在要自己从源码编译安装，还没上 App Store。
- **[CLI 与 API](https://multica.ai/docs/cli) →** 界面上能点的，CLI 和 API 里都能调。智能体操作 Multica，用的就是你那套 CLI。

---

## 开始使用

不用打开终端：直接在 **[multica.ai](https://multica.ai)** 注册，或者下载
**[Multica 桌面端](https://multica.ai/download)**（macOS / Windows / Linux）——打开它，这台电脑
就自动成了一个运行时。

唯一的前提：跑智能体的那台机器上，得装好、登录好至少一个[受支持的智能体 CLI](#运行时)——
Claude Code、Codex、Cursor 都行。Multica 负责驱动它们，但不替你安装。

<details>
<summary><b>整套自部署</b></summary>

<br/>

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --with-server
multica setup self-host
```

Windows 上先设 `$env:MULTICA_MODE="with-server"`，再跑 PowerShell 安装脚本：
`irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex`。

这会拉取 GHCR 上的官方镜像，需要 Docker。详见[自部署指南](SELF_HOSTING.md)。如果你选的 GHCR
标签还没发布，可以在代码目录里跑 `make selfhost-build` 兜底。

</details>

---

## 五分钟跑通第一个智能体

**1. 登录。** 在浏览器里打开 [multica.ai](https://multica.ai)，或者打开
[Multica 桌面端](https://multica.ai/download)。

**2. 接入一台电脑。** 所谓*运行时*，就是智能体干活用的机器——你的笔记本，或者一台云主机。用桌面端，
这一步是自动的：它会注册好这台电脑，顺便检测装了哪些智能体 CLI。用网页版、或者想再接一台机器，就
打开侧边栏的**运行时**，点右上角的**添加电脑**，把弹窗里的两条命令粘到那台机器的终端里。

**3. 创建智能体。** 打开侧边栏的**智能体**，点**新建智能体**。选中刚接入的运行时，选一个提供方，
起个名字——或者选**通过 AI 创建**，描述几句，配置自动生成。这个名字就是它之后在看板和评论里的身份。

**4. 派给它一件事。** 建一个任务，负责人选成这个智能体。它会自己接手、在你的机器上跑、边做边评论，
干完把任务挪到审核中。

完整流程：[快速开始](https://multica.ai/docs/cloud-quickstart) · [上手教程](https://multica.ai/docs/tutorial)

---

## 确定性工具

技能是*建议性*的——它是智能体读取的 Markdown，可以遵循、转述，也可以忽略。对于需要判断力的场景（"如何组织一个 PR"、命名规范），这种形态是对的；但对任何与正确性相关的事情，它就是错的：一条写着"确保测试通过"的技能，只是一个模型可以凭空绕过的建议。

**确定性工具**填补了这个缺口。工具是会*真正执行*的带类型 Go 代码——它检查仓库、强制执行策略、或运行某个门禁，并返回智能体可据以分支判断的、可验证的结果。智能体通过 [MCP](https://modelcontextprotocol.io) 调用工具；一套内置目录（`repo_facts`、`policy_check`、`build_probe`、`test_gate`、`diff_summarize`、`artifact_emit`）已编译进守护进程二进制文件，你也可以在工作区里编写自己的工具。

| | 技能（建议性） | 确定性工具 |
|---|---|---|
| 是什么 | 智能体上下文中的 Markdown | 会执行的带类型 Go 代码 |
| 答错时 | 模型据以行动的一个建议 | 被测试捕获的一个 Bug |
| 适用于 | 框架、规范、判断 | 仓库事实、门禁、"通过了吗？" |

### 编写工具

打开工作区，在侧边栏进入 **Tools（工具）**。编写一个确定性 Go *步骤*，给出示例输入，点击 **Test（测试）** 即可在沙箱中立即运行——无需部署，无需重新构建。

一个步骤是一个名为 `step` 的 Go 包，暴露一个函数：

```go
package step

import "strings"

// Run 接收解码后的 JSON 输入，返回一个 Result 信封。
func Run(input map[string]any) map[string]any {
	name, _ := input["name"].(string)
	if name == "" {
		return map[string]any{
			"status":     "error",
			"error_code": "INVALID_INPUT",
			"summary":    "input.name is required",
		}
	}
	return map[string]any{
		"status":  "ok",
		"summary": "Greeted " + name,
		"machine_data": map[string]any{
			"greeting": "Hello, " + strings.ToUpper(name),
			"length":   len(name),
		},
	}
}
```

用输入 `{ "name": "world" }` 测试，会返回标准的 **Result 信封**——这与内置工具和智能体使用的是同一份契约：

```json
{
  "status": "ok",
  "summary": "Greeted world",
  "machine_data": { "greeting": "Hello, WORLD", "length": 5 },
  "retryable": false
}
```

`status` 取值为 `"ok"` 或 `"error"`；失败时设置一个稳定的 `error_code`（`INVALID_INPUT`、`MISSING_DEPENDENCY`、`POLICY_FAILURE`、`TIMEOUT`、`INTERNAL_ERROR`）。一个只返回数据、不带 `status` 的步骤会被视为成功。

### 是门禁，不是猜测

确定性工具的意义在于*强制执行*，而非建议。一个策略门禁会返回智能体无法绕过的硬性失败：

```go
package step

import "strings"

// 如果改动落在了非 feature 分支上，就让任务失败。
func Run(input map[string]any) map[string]any {
	branch, _ := input["branch"].(string)
	if !strings.HasPrefix(branch, "feature/") {
		return map[string]any{
			"status":     "error",
			"error_code": "POLICY_FAILURE",
			"summary":    "branch " + branch + " must start with feature/",
			"machine_data": map[string]any{"branch": branch},
		}
	}
	return map[string]any{"status": "ok", "summary": "branch policy ok"}
}
```

### 沙箱

步骤运行在内嵌的 Go 解释器中，而非编译后的二进制文件，因此可以在运行时编写和修改，无需重新部署。解释器采用**白名单制**：步骤只能导入纯粹、确定性的标准库包（`fmt`、`strings`、`strconv`、`regexp`、`encoding/json`、`time`、`slices`、`math` 等），其余一概不可。`os`、`os/exec`、`io`、`net/*`、`syscall` 均不可导入——步骤可以对输入做计算，但无法触及主机、文件系统或网络。

每次运行还会在**独立的隔离进程**中进行（二进制文件以一次性沙箱模式重新执行自身），而非在进程内运行：子进程只拥有不含服务端任何密钥的最小环境，并带有内核 CPU 时间上限；步骤一旦超时即被硬性终止（`SIGKILL`），panic 则以 `INTERNAL_ERROR` 形式返回——绝不会导致崩溃，也不会在长期运行的进程中泄漏 goroutine。

### 启用面向智能体的工具平面

确定性工具平面默认关闭。在守护进程上启用它，智能体即可通过 MCP 收到这些工具：

```bash
export MULTICA_DETTOOLS_ENABLED=true                                   # 总开关
export MULTICA_DETTOOLS_ALLOWED=repo_facts,policy_check,build_probe,test_gate,dotnet_test_gate  # 白名单（默认为完整的只读目录）
export MULTICA_DETTOOLS_TIMEOUT=90s                                    # 单个工具的超时
```

启用后，工作区**已保存**的工具会随每个任务一起下发给智能体：认领任务时，守护进程把启用的工具写入任务工作目录，由每个任务的 MCP 服务在沙箱中运行，智能体即可像调用其他工具一样按名调用它们。还可通过智能体的 `runtime_config`（`deterministic_tools.allowed_tools` / `denied_tools`）做按智能体收窄——智能体只能收窄守护进程的白名单，永远无法扩大它。

---

## 运行时

Multica 不自带模型。它驱动的是你本来就装好、登录好的那些智能体 CLI，所以换提供方就是切个下拉框，
谈不上迁移。

| Provider | CLI | Provider | CLI |
| --- | --- | --- | --- |
| Claude Code | `claude` | OpenAI Codex | `codex` |
| Cursor Agent | `cursor-agent` | GitHub Copilot CLI | `copilot` |
| OpenCode | `opencode` | OpenClaw | `openclaw` |
| Hermes | `hermes` | Pi | `pi` |
| Antigravity | `agy` | CodeBuddy | `codebuddy` |
| DevEco Code | `deveco` | Grok | `grok` |
| Kimi | `kimi` | Kiro CLI | `kiro-cli` |
| Qoder CLI | `qodercli` | Qoder CN | `qoderclicn` |
| Qwen Code | `qwen` | QwenPaw | `qwenpaw` |
| Reasonix | `reasonix` | Trae CLI | `traecli` |
| DeepSeek Harness | `dsh` | Oh-My-Pi | `omp` |

怎么装、怎么登录：[安装智能体运行时](https://multica.ai/docs/install-agent-runtime) ·
[AI 编程工具对照](https://multica.ai/docs/providers)

---

## 文档

| 我想…… | 从这里看 |
| --- | --- |
| 今天就让智能体干点活 | [快速开始](https://multica.ai/docs/cloud-quickstart) · [上手教程](https://multica.ai/docs/tutorial) |
| 搞清楚这套系统怎么运转 | [核心概念](https://multica.ai/docs/concepts) · [Multica 如何工作](https://multica.ai/docs/how-multica-works) |
| 创建和配置智能体 | [智能体](https://multica.ai/docs/agents) · [创建智能体](https://multica.ai/docs/agents-create) · [Skills](https://multica.ai/docs/skills) |
| 把活交到智能体手上 | [触发智能体](https://multica.ai/docs/triggering-agents) · [分配任务](https://multica.ai/docs/assigning-issues) · [提及](https://multica.ai/docs/mentioning-agents) |
| 把我的机器接进来 | [守护进程与运行时](https://multica.ai/docs/daemon-runtimes) · [安装智能体运行时](https://multica.ai/docs/install-agent-runtime) |
| 接上 Git 和聊天工具 | [GitHub](https://multica.ai/docs/github-integration) · [自建 Git](https://multica.ai/docs/vcs-integration) · [消息渠道](https://multica.ai/docs/channels) |
| 部署在自己的基础设施上 | [自部署](SELF_HOSTING.md) · [安全模型](https://multica.ai/docs/security-model) · [环境变量](https://multica.ai/docs/environment-variables) |
| 用脚本驱动它 | [CLI 参考](https://multica.ai/docs/cli) · [CLI 与守护进程指南](CLI_AND_DAEMON.md) · [认证令牌](https://multica.ai/docs/auth-tokens) |
| 查智能体为什么卡住了 | [执行任务](https://multica.ai/docs/tasks) · [问题排查](https://multica.ai/docs/troubleshooting) |

---

## 架构

```
        Web  ·  桌面端 (macOS/Windows/Linux)  ·  iOS
                          │
                          ▼
   ┌──────────────┐   ┌──────────────┐   ┌──────────────────┐
   │   Next.js    │──>│   Go 后端    │──>│   PostgreSQL     │
   │    前端      │<──│  (Chi + WS)  │<──│   (pgvector)     │
   └──────────────┘   └──────┬───────┘   └──────────────────┘
                             │  通过 WebSocket 下发 task
                      ┌──────┴───────┐
                      │   守护进程   │  跑在你的机器上，紧挨着你的代码
                      └──────┬───────┘
                             │  拉起
                      ┌──────┴───────────────────────────────┐
                      │  Claude Code · Codex · Cursor · …    │
                      │  （上面 20 种运行时里的任意一种）    │
                      └──────────────────────────────────────┘
```

| 层级 | 技术栈 |
| --- | --- |
| Web | Next.js 16 (App Router) |
| 桌面端 | Electron，复用 Web 的 UI 包 |
| 移动端 | Expo / React Native (iOS) |
| 后端 | Go (Chi router, sqlc, gorilla/websocket) |
| 数据库 | PostgreSQL 17 + pgvector |
| 智能体运行时 | 本地守护进程拉起上面 20 种智能体 CLI 中的任意一个 |

---

## 开发

想参与贡献，先看[贡献指南](CONTRIBUTING.md)。

**环境要求：**[Node.js](https://nodejs.org/) 22、[pnpm](https://pnpm.io/) 10.28.2、[Go](https://go.dev/) 1.26.6、[Docker](https://www.docker.com/)

```bash
make dev
```

`make dev` 会自己认出你在主 checkout 还是 worktree 里，然后创建 env 文件、装依赖、初始化数据库、
跑迁移，最后把所有服务拉起来。

完整的开发流程、worktree 支持、测试和问题排查见 [CONTRIBUTING.md](CONTRIBUTING.md)。
iOS 客户端在 [`apps/mobile/`](apps/mobile/)，怎么编译装到自己 iPhone 上见它的
[README](apps/mobile/README.md)。

我们几乎每个工作日都发版，`main` 走得很快——记得常拉最新代码。

---

## 为什么叫 "Multica"

**Mul**tiplexed **I**nformation and **C**omputing **A**gent —— 向 Multics 致意。那是 20 世纪
60 年代的操作系统，它首创了分时：多个人共享同一台机器，却又都像独占它一样。

此后几十年，软件团队一直是单线程的：一个工程师、一个任务、一次一个上下文切换。我们认为，智能体让
"分时"重新成立了——只不过这一次，系统里被多路复用的"用户"，既是人，也是机器。小团队不该因为人少，
就只能干出小团队的量。

更长的论证，以及我们认为这件事会走到哪里：**[VISION.zh.md](VISION.zh.md)**。

---

## 开源协议

[Multica License](LICENSE) —— Apache License 2.0 全文并入，外加针对托管服务、商业嵌入和品牌标识的
附加条件。自部署、改代码、在它之上做东西都可以；准确条款以 [LICENSE](LICENSE) 为准，署名信息见
[NOTICE](NOTICE)。
