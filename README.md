# net-tools-mcp

通过 MCP（stdio）调用两个网络查询：

- `ping`：执行系统 `ping`，解析主机名对应的 IP，并返回是否可达和往返时延。
- `ip_me`：以 `curl https://ip.me` 的方式获取本机出口公网 IP，再读取 [ip.me](https://ip.me/) 页面上的国家、城市、组织、ISP 和 ASN。传入 `ip` 时查询指定地址的归属。

请从 GitHub Release 下载对应系统的二进制，不要在本机执行 `go build`。

## 工具

### ping

| 参数 | 说明 |
| --- | --- |
| `host` | 主机名或 IP |
| `count` | 探测包数量，1–4，默认 1 |
| `timeout_sec` | 单包等待秒数，1–5，默认 2 |

主机名不可达时，只要 `ping` 输出里已经解析出地址，仍返回该 IP，并把 `reachable` 设为 false。

### ip_me

| 参数 | 说明 |
| --- | --- |
| `ip` | 可选。留空查询本机出口；填写则查询该公网地址 |

`curl https://ip.me` 只返回纯文本 IP。归属来自同一站点对浏览器返回的查询表（城市、国家、组织、ASN、ISP）。

## 在 Cursor 里启用

从 [Releases](https://github.com/Songxwn/net-tools-mcp/releases) 下载 Windows 可执行文件后，写入 MCP 配置：

```json
{
  "mcpServers": {
    "net-tools": {
      "command": "C:\\path\\to\\net-tools-mcp_v0.1.0_windows_amd64.exe"
    }
  }
}
```

标准输出只用于 MCP 协议。服务不会把日志写到 stdout。
