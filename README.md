# cpa-quota

[English](README.en.md)

CLIProxyAPI（CPA 8.0+）额度插件：实现 v8 `QuotaProvider`，统一查询 Claude、Codex、Antigravity、Kimi、xAI、Devin、Meta 的额度与重置窗口，并内置额度看板。

## 项目简介

插件在服务端查询各厂商额度，归一化为 v8 的 `subscription / summary / groups[].buckets[]` 结构。CPA 通过 `/v8/management/plugins/cpa-quota/quota` 提供查询与重置，面板只负责渲染，不再维护厂商适配。

看板提供卡片与额度时间线视图；时间线支持月、14 天和 5 小时三种跨度，并支持渠道筛选、搜索和额度重置。

## 开发环境配置

前置要求：

- Go 1.25+，需要 CGO（GCC 或 Clang）
- Node.js 24+ / pnpm 12+

```bash
(cd web && pnpm install && pnpm run dev) # 单独调试前端
(cd web && pnpm run test)                # 密钥解析测试
go test ./...                            # 后端测试
./build.sh                               # 构建前端并编译动态库
```

调试前端时可访问 `http://localhost:5173/?api_base=http://127.0.0.1:8317` 连接正在运行的 CPA。

## 构建与部署

### 插件商店安装

插件收录到官方插件商店后，在管理中心搜索 `CPA Quota` 安装。也可以在 CPA 中添加本仓库的商店源：

```yaml
plugins:
  enabled: true
  dir: "plugins"
  store-sources:
    - "https://raw.githubusercontent.com/tom2almighty/cpa-quota/main/plugin-store/registry.json"
  configs:
    cpa-quota:
      enabled: true
```

### 手动安装

从 [Releases](https://github.com/tom2almighty/cpa-quota/releases) 下载对应平台的压缩包，解压后把 `cpa-quota.so`（Linux）/ `.dylib`（macOS）/ `.dll`（Windows）放入 CPA 的 `plugins` 目录。

### 本地打包

```bash
./scripts/package-release.sh 0.3.0
```

产物写入 `release-dist/`，包含平台压缩包与 `checksums.txt`。

### 使用

官方管理中心仅复用当前 v2 格式中已勾选记住的密钥；旧格式需在官方管理中心重新登录并保存，或在插件页面手动输入。

在管理中心插件列表打开「额度监控」。页面通过 CPA v8 管理 API 读取凭据与额度，管理密钥优先复用 cpa-dashboard / 官方管理中心保存的值，也可以手动输入。

支持渠道：

- Claude（含 reset grants；按组织区分 Max 5x、Max 20x 与生效中的 Team 套餐）
- Codex（含 reset credits；启用 `use_siwc` 时按 SIWC 凭据分区查询）
- Antigravity
- Kimi / Kimi AI（套餐名称取自官方账号资料；资料查询失败不影响额度显示）
- xAI
- Devin
- Meta

### Docker

CPA 的插件目录需要持久化或挂载：

```yaml
services:
  cli-proxy-api:
    image: ghcr.io/router-for-me/cliproxyapi:latest
    volumes:
      - ./plugins:/app/plugins
      - ./config.yaml:/app/config.yaml
    ports:
      - "8317:8317"
```

把动态库放到 `./plugins` 后重启容器即可。

## 开源许可证

[AGPL-3.0](LICENSE)
