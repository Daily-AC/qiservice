# API Reference

本服务提供兼容 OpenAI 的标准接口，并扩展了一些管理功能。

## 🔐 鉴权认证

所有请求都需要携带 `Authorization` Header。

- **格式**: `Bearer <YOUR_CLIENT_KEY>`
- **获取**: 访问 Web Dashboard 的 **Security / Access Control** 页面生成以 `sk-` 开头的密钥。

---

## 🤖 核心模型接口 (OpenAI Compatible)

### 1. Chat Completions (对话)

完全兼容 OpenAI `/v1/chat/completions` 标准。

- **URL**: `POST /v1/chat/completions`
- **Content-Type**: `application/json`

**请求参数**:

| 参数          | 类型    | 必填 | 描述                                                  |
| :------------ | :------ | :--- | :---------------------------------------------------- |
| `model`       | string  | 是   | 服务名 (Service Name)，对应 Dashboard 配置的 "Name"。 |
| `messages`    | array   | 是   | 消息列表。                                            |
| `stream`      | boolean | 否   | 是否启用流式响应 (SSE)。默认为 `false`。              |
| `temperature` | number  | 否   | 采样温度 (0-2)。                                      |
| `tools`       | array   | 否   | 工具定义列表 (支持 OpenAI/Anthropic 协议互转)。       |

**Example (cURL)**:

```bash
curl http://localhost:11451/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-station-example" \
  -d '{
    "model": "gpt-4-proxy",
    "messages": [
      {"role": "user", "content": "Hello!"}
    ],
    "stream": true
  }'
```

### 2. List Models (列出模型)

获取当前可用的服务列表。

- **URL**: `GET /v1/models`

**Response**:

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4-proxy",
      "object": "model",
      "created": 1677610602,
      "owned_by": "openai"
    },
    {
      "id": "claude-3-opus",
      "object": "model",
      "created": 1677610602,
      "owned_by": "openai"
    }
  ]
}
```

---

## 📊 统计与管理接口 (Private API)

> ⚠️ 注意：以下接口主要用于 Dashboard，虽然使用同样的 Bearer Token 可以访问，但 API 结构可能会变动。

### 1. Get Statistics (获取统计数据)

获取指定日期的调用统计、模型分布和 Token 消耗。

- **URL**: `GET /api/stats`
- **Query Params**:
  - `date`: `YYYY-MM-DD` (默认今日)

**Response**:

```json
{
  "date": "2025-12-31",
  "total_requests": 150,
  "summary": {
    "gpt-4-proxy": {
      "requests": 100,
      "tokens_in": 5000,
      "tokens_out": 2000
    },
    ...
  },
  "records": [
    {
      "time": "2025-12-31T18:00:00Z",
      "model": "gpt-4-proxy",
      "duration_ms": 1200,
      "success": true,
      "tokens_in": 50,
      "tokens_out": 20
    },
    ...
  ]
}
```

### 2. Manage Services (增删改查服务)

- **URL**: `GET /api/config/services` (列出)
- **URL**: `POST /api/config/services` (更新全量列表)
