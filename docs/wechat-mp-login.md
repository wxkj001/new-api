# 微信小程序扫码登录

基于 `github.com/silenceper/wechat/v2` 库实现的微信小程序扫码登录，支持小程序码轮转复用和 QR 图片缓存。

## 目录

- [接口列表](#接口列表)
- [完整登录流程](#完整登录流程)
- [配置项](#配置项)
- [小程序端接入](#小程序端接入)
- [小程序码轮转复用](#小程序码轮转复用)
- [文件清单](#文件清单)

---

## 接口列表

| 方法 | 路径 | 说明 | 限流 |
|------|------|------|------|
| `POST` | `/api/wechat-mp/url` | 生成小程序码 | ✅ CriticalRateLimit |
| `GET` | `/api/wechat-mp/status` | 轮询登录状态 | ❌ 无限流 |
| `POST` | `/api/wechat-mp/login` | 小程序端登录 | ❌ 无限流 |

### `POST /api/wechat-mp/url`

生成登录用的微信小程序码，优先复用缓存图片。

**请求**

无参数。

**响应**

```json
{
  "success": true,
  "message": "",
  "data": {
    "code": "550e8400-e29b-41d4-a716-446655440000",
    "qr_image": "data:image/png;base64,iVBORw0KGgo..."
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `code` | string | 轮询登录状态的唯一标识（UUID），不嵌入小程序码 |
| `qr_image` | string | base64 编码的 PNG 图片，可直接用于 `<img src>` |

**错误响应**

| 条件 | 响应 |
|------|------|
| 小程序登录未启用 | `{ "success": false, "message": "微信小程序登录未启用" }` |
| 池满且全部活跃 | `{ "success": false, "message": "当前排队人数较多，请稍后再试" }` |
| 微信 API 调用失败 | `{ "success": false, "message": "生成微信小程序二维码失败" }` |

---

### `POST /api/wechat-mp/login`

> **由微信小程序调用**，Web 前端不直接调此接口。

小程序扫码后调用 `wx.login()` 获取临时 `js_code`，连同二维码中的 `scene` 发给后端完成登录。

**请求**

```json
{
  "scene": "s_a1b2c3d4",
  "code": "0b1C2D3e4F5g6H7i8J9k0L"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `scene` | string | ✅ | 扫码获得的小程序码 scene 值（≤32 字符） |
| `code` | string | ✅ | `wx.login()` 返回的临时登录凭证 |

**成功响应**

```json
{ "success": true, "message": "Login successful" }
```

**错误响应**

| 条件 | 响应 |
|------|------|
| 参数缺失 | `{ "success": false, "message": "Invalid request parameters" }` |
| scene 无效或已过期 | `{ "success": false, "message": "Invalid or expired login code" }` |
| 微信 code 换 openid 失败 | `{ "success": false, "message": "WeChat authorization failed" }` |
| 管理员关闭注册 | `{ "success": false, "message": "Registration is disabled" }` |
| 用户已注销 | `{ "success": false, "message": "User has been deleted" }` |

**内部处理**

1. 校验 `scene` 是否存在且 `status = pending` 且未过期
2. 调用微信 `Code2Session` → 换取 `openid`
3. `openid` 已存在 → 查找用户（登录）
4. `openid` 不存在 → 创建新用户，用户名 `wxmp_{id}`，角色普通用户
5. 标记对应 `code` 为 `success`，写入 `user_id`（供前端轮询）

---

### `GET /api/wechat-mp/status`

Web 前端轮询登录状态，建议每 2 秒轮询一次。登录成功时自动写入 session。

**请求**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `code` | string (query) | ✅ | `POST /wechat-mp/url` 返回的轮询码 |

**响应 — 等待扫码**

```json
{
  "success": true,
  "status": "pending",
  "message": "Waiting for scan"
}
```

**响应 — 登录成功**

```json
{
  "success": true,
  "message": "",
  "data": {
    "id": 42,
    "username": "wxmp_42",
    "display_name": "WeChat MP User",
    "role": 1,
    "status": 1,
    "group": "default"
  }
}
```

同时设置 session cookie，后续请求自动认证。

**响应 — 失败**

| 条件 | 响应 |
|------|------|
| 已过期（5 分钟 TTL） | `{ "success": false, "status": "expired", "message": "Login code expired" }` |
| 登录失败 | `{ "success": false, "status": "failed", "message": "Login failed or expired" }` |
| 用户被封禁 | `{ "success": false, "status": "failed", "message": "User is banned" }` |
| code 不存在 | `{ "success": false, "status": "expired", "message": "Login code not found or expired" }` |

---

## 完整登录流程

```
┌─ Web 前端 ────────────────────────────────────────────┐
│                                                        │
│  1. POST /api/wechat-mp/url                            │
│     → { code: "uuid", qr_image: "base64..." }          │
│                                                        │
│  2. 展示小程序码 <img src="data:image/png;base64,...">  │
│                                                        │
│  3. setInterval(2s): GET /api/wechat-mp/status?code=xx │
│     ← status: "pending"                                │
│     ← status: "pending"                                │
│     ← status: "pending"                                │
│                                                        │
│               ┌─ 用户操作 ────────────────────────────┐│
│               │                                       ││
│               │  4. 微信扫描小程序码                    ││
│               │     → 进入小程序，获得 scene           ││
│               │                                       ││
│               │  5. 小程序 wx.login() → js_code       ││
│               │                                       ││
│               │  6. 小程序 POST /api/wechat-mp/login  ││
│               │     { scene, code: js_code }           ││
│               │                                       ││
│               │  后端处理:                              ││
│               │  - Code2Session → openid              ││
│               │  - 查 openid → 登录 OR 创建 → 注册    ││
│               │  - UPDATE code → status=success        ││
│               │                                       ││
│               └───────────────────────────────────────┘│
│                                                        │
│  7. GET /api/wechat-mp/status?code=xx                  │
│     → status: "success", user_id: 42                   │
│     → setupLogin(user) → 写 session                    │
│     → window.location.href = '/'                       │
│                                                        │
└────────────────────────────────────────────────────────┘
```

---

## 配置项

> 管理后台路径：系统设置 → 认证 → OAuth 集成 → WeChat MP 标签页
>
> 或通过 API：`PUT /api/option/`

| Key | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `WeChatMpAuthEnabled` | bool | `false` | 是否启用 |
| `WeChatMpAppId` | string | `""` | 小程序 AppID（启用前必填） |
| `WeChatMpAppSecret` | string | `""` | 小程序 AppSecret |
| `WeChatMpPagePath` | string | `""` | 扫码后跳转的页面路径，空即首页（如 `pages/login/index`） |
| `WeChatMpMaxQrCodes` | int | `3` | 最大同时生效的小程序码数量，达到上限后回收复用 |

`GET /api/status` 响应中新增字段：

```json
{
  "wechat_mp_login": true,
  "wechat_mp_appid": "wx..."
}
```

`wechat_mp_login` 为 `true` 时前端登录页展示小程序登录按钮。

---

## 小程序端接入

你的小程序需要在扫码进入后完成以下步骤：

```javascript
// app.js - onLaunch 或页面的 onLoad
App({
  onLaunch(options) {
    // 1. 从小程序码的启动参数获取 scene
    const scene = decodeURIComponent(options.query.scene || '')

    if (scene) {
      // 2. 调用 wx.login 获取临时 code
      wx.login({
        success: (res) => {
          // 3. 发送到后端
          wx.request({
            url: 'https://your-api.com/api/wechat-mp/login',
            method: 'POST',
            data: {
              scene: scene,
              code: res.code
            },
            success: (result) => {
              if (result.data.success) {
                wx.showToast({ title: '登录成功', icon: 'success' })
                // 登录成功后小程序端无需额外操作
                // Web 前端轮询会检测到 success 并完成登录
              } else {
                wx.showToast({ title: '登录失败', icon: 'error' })
              }
            }
          })
        }
      })
    }
  }
})
```

> **注意**：小程序端 `wx.request` 需要在微信公众平台后台配置 request 合法域名。

---

## 小程序码轮转复用

### 复用策略

```
请求生成码
  │
  ├─ 1. 优先复用 已结束/已过期 的记录
  │     GetReusableWeChatMpScene()
  │     → 原地 UPDATE（重置 code、status、过期时间）
  │     → 复用缓存的 QR 图片（不调微信 API）
  │
  ├─ 2. 池满时 强制回收
  │     Count >= MaxQrCodes
  │     → 仅回收已结束/已过期的
  │     → 全部活跃时返回 "当前排队人数较多"
  │
  └─ 3. 都没命中 → 调 GetWXACodeUnlimit 生成新码
```

### 回收入选条件

| 状态 | 是否可回收 |
|------|-----------|
| `success` — 已扫码登录 | ✅ 优先回收 |
| `failed` — 登录失败 | ✅ |
| `expired` — 已过期 | ✅ |
| `pending` 且 `expires_at < now` — 超时未扫（用户关掉页面） | ✅ |
| `pending` 且 `expires_at >= now` — 正在等待扫码 | ❌ 不回收 |

### 数据模型

```
wechat_mp_login_codes 表
┌────┬──────────────────────────┬──────────────┬───────────────┬──────────┬─────────┐
│ id │ code (前端轮询)           │ scene (嵌在码里)│ qr_image     │ status  │ user_id │
├────┼──────────────────────────┼──────────────┼───────────────┼──────────┼─────────┤
│ 1  │ 550e8400-...-446655440000│ s_a1b2c3d4   │ <3KB PNG>     │ success │ 42      │
│ 2  │ 660f9501-...-557766551111│ s_b2c3d4e5   │ <3KB PNG>     │ pending │ 0       │
│ 3  │ 770fa602-...-668877662222│ s_c3d4e5f6   │ <3KB PNG>     │ expired │ 0       │
└────┴──────────────────────────┴──────────────┴───────────────┴──────────┴─────────┘

同一个 scene 在表中始终只有 1 条记录，每次复用时原地 UPDATE 而非 INSERT。
```

---

## 文件清单

| 文件 | 说明 |
|------|------|
| `controller/wechat_mp.go` | 3 个接口 Handler + `getMiniProgram()` + `generateScene()` |
| `model/wechat_mp.go` | `WeChatMpLoginCode` 模型 + 复用/回收/计数函数 |
| `model/user.go` | `WeChatMpOpenId` 字段 + `FillUserByWeChatMpOpenId()` + `IsWeChatMpOpenIdAlreadyTaken()` |
| `model/option.go` | `InitOptionMap` + `updateOptionMap` 新增 5 个配置项 |
| `model/main.go` | AutoMigrate 新增 `&WeChatMpLoginCode{}` |
| `common/constants.go` | 新增 5 个配置变量 |
| `controller/option.go` | 启用前校验 `WeChatMpAppId` |
| `controller/misc.go` | `GetStatus` 新增 `wechat_mp_login` + `wechat_mp_appid` |
| `router/api-router.go` | 注册 3 条路由 |
| `i18n/keys.go` | 新增 4 个 i18n key |
| `i18n/locales/{en,zh-CN,zh-TW}.yaml` | 新增翻译 |
| `web/default/src/features/` | 前端登录弹窗 + Admin OAuth 配置页（WeChat MP 标签） |
