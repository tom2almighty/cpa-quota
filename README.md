# cpa-quota

[English](README.en.md)

CLIProxyAPI（CPA 8.0+）额度查询插件：通过 v8 `QuotaProvider` 暴露 Claude、Codex、Antigravity、Kimi、xAI、Devin、Meta 的统一额度数据，并内置额度监控页面。

## 项目简介

插件在服务端调用各厂商额度接口，把结果归一化为 v8 的 `subscription / summary / groups[].buckets[]` 结构；CPA 通过 `/v8/management/plugins/cpa-quota/quota` 对外提供查询与重置，管理面板只负责渲染，不再维护厂商适配。

## 开发环境配置

前置要求：

- Go 1.25+，需要 CGO（GCC 或 Clang）
- Node.js 24+ / pnpm 12+

```bash
cd web && pnpm install && pnpm run dev   # 单独调试前端
go test ./...                            # 后端测试
./build.sh                               # 构建前端并编译动态库
```

调试前端时可访问 `http://localhost:5173/?api_base=http://127.0.0.1:8317` 连接正在运行的 CPA。

## 构建与部署

### 插件商店安装

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

在管理中心插件商店搜索 `CPA Quota` 安装并启用。

### 手动安装

从 Releases 下载对应平台的压缩包，解压后把 `cpa-quota.so`（Linux）/ `.dylib`（macOS）/ `.dll`（Windows）放入 CPA 的 `plugins` 目录。

### 本地打包

```bash
./scripts/package-release.sh 1.0.0
```

产物写入 `release-dist/`，包含平台压缩包与 `checksums.txt`。

### 使用

插件启用后，在管理中心插件列表打开「额度监控」。页面通过 CPA v8 管理 API 读取凭据与额度，管理密钥优先复用 cpa-dashboard / 官方管理中心的本地保存值，也可以手动输入。

支持渠道：

- Claude（含 reset grants）
- Codex（含 reset credits）
- Antigravity
- Kimi / Kimi AI
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
