# ✨ LLM Service Station (AI 服务中转站) | Go

一个轻量级的 AI 模型聚合网关，基于 Go 语言开发。
它像一个插座一样，将不同厂商（OpenAI, Anthropic, Gemini, etc.）的 API 标准化，并提供统一的接口管理、流式中转和安全控制。

## 📸 界面预览

<p align="center">
  <img src="https://github.com/user-attachments/assets/9ccaf48c-b3c8-49c6-a64f-c948d152087b" width="100%" alt="Service Management" />
</p>

<p align="center">
  <img src="https://github.com/user-attachments/assets/da3d3de6-3aa0-45b7-a69b-474f561e02bc" width="48%" alt="Security Access" />
  <img src="https://github.com/user-attachments/assets/0e3ab044-dc76-4fd9-aedf-9a136d7a4a51" width="48%" alt="Playground" />
</p>

## 🚀 核心特性

- **多协议互通**:
  - 支持 **OpenAI** 格式客户端（如 Cherry Studio, NextChat）。
  - 支持 **Anthropic** 格式客户端（如 Claude Code, Cursor）。
  - 🔄 **双向协议转换**: 用 OpenAI 客户端调用 Claude 模型，或用 Claude 客户端调用 GPT 模型，全部自动抹平差异！
- **流式传输 (Streaming)**: 完美支持 SSE (Server-Sent Events) 打字机效果，针对 Claude Code 等严格客户端进行了深度优化。
- **安全管理**:
  - 全站 HTTPS/Token 鉴权。
  - 内置简单的 Admin 密码锁，保护管理后台。
- **可视化管理**:
  - 内置美观的 Web 控制台。
  - 可视化配置服务商 API Key 和路由规则。
- **灵活路由**: 根据 Model Name 自动分流请求到不同的上游服务。

## 🛠️ 快速开始

### 1. 环境要求

- Go 1.21+

### 2. 编译与运行

```bash
# 进入项目目录
cd qiservice

# 编译
go build -o service-station.exe cmd/server/main.go

# 运行
./service-station.exe
```

### 3. 初次设置

首次运行时，终端会随机生成一个 **Admin Password**，用于登录 Web 管理后台。

```text
⚠️  ADMIN PASSWORD NOT SET. GENERATED: xxxxxxxx-xxxx-xxxx...
```

请复制该密码，浏览器访问 `http://localhost:11451` 进行解锁。
解锁后，建议在 Web 后台将其修改为好记的密码。

## � 服务器部署 (Linux/Ubuntu)

本项目提供了一键安装脚本，适配 Ubuntu 24.04 等 Systemd 发行版。

### 1. 运行安装脚本

将项目上传至服务器，需确保已安装 Go 环境。

```bash
# 赋予执行权限
chmod +x install.sh

# 运行安装 (需要 root 权限)
sudo ./install.sh
```

脚本会自动编译、移动文件到 `/opt/qiservice` 并注册系统服务。

### 2. 服务管理

```bash
# 启动服务
sudo systemctl start qiservice

# 停止服务
sudo systemctl stop qiservice

# 查看实时日志
sudo journalctl -u qiservice -f

# 开机自启 (脚本已默认开启)
sudo systemctl enable qiservice
```

## �🔌 接入指南

### 方式 A: OpenAI 兼容客户端 (推荐)

适用于 Cherry Studio, NextChat, LangChain 等。

- **Base URL**: `http://localhost:11451/v1`
- **API Key**: (在 "Access Control" 页面生成的 Key)
- **Model**: (你在 "My AI Services" 页面配置的服务名)

### 方式 B: Anthropic 客户端

适用于 Claude Code, Cursor 等原生支持 Claude 的工具。

- **Base URL**: `http://localhost:11451/v1` (部分工具可能需要配置为 `/v1/messages`)
- **API Key**: 填 `x-api-key` 或者 Bearer Token 均可。

## 📂 项目结构

```
.
├── cmd/
│   └── server/       # 程序入口
├── internal/
│   ├── api/          # HTTP 处理器与路由
│   └── provider/     # 各大模型厂商适配层 (OpenAI/Anthropic/Gemini)
├── web/              # 前端静态资源 (HTML/CSS/JS)
└── config.json       # 配置文件 (自动生成，请勿提交到 Git)
```

## ⚠️ 注意事项

- 本项目旨在作为本地或私有网络的中转网关，请勿在未加固的情况下直接暴露在公网。
- `config.json` 包含敏感 API Key，**已被 .gitignore 忽略**，请勿提交。

## 📄 License

MIT
