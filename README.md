# MosDNS

✨ 基于 [`sbwml/luci-app-mosdns`](https://github.com/sbwml/luci-app-mosdns) `v5` 的个性化版本。

### 🖇️ 自定义修改

1. 同步上游代码后应用 MosDNS 自定义内容
   > **Custom patches**: `maint/patches/`  
   > **Maintenance scripts**: `maint/scripts/`
2. 去掉 `v2ray-geoip` / `v2ray-geosite` / `v2dat` / `geo2txt` 依赖与打包
3. 启动时不再执行 `v2dat_dump`
4. 修复 ImageBuilder 安装阶段 init 顶层调用 `uci` 的问题
5. 为 mosdns 内核增加 `query_set` 插件：一个由查询自动生长、并自动持久化的域名集合
   > **Kernel patch**: `mosdns/patches/000-add-query_set-domain-set-plugin.patch`
   > **用法**: `- exec: $tag` 记录当前请求域名，`- matches: qname $tag` 直接当域名集合用（精确匹配，不含子域）
6. 修复空响应 SOA owner，并按 RFC 2308 为 `reject 0` / `reject 3` 添加 300 秒负缓存 SOA；错误类返回码保持原行为
   > **Patch**: `mosdns/patches/900-fix-negative-response-soa.patch`

### 🙏 致谢

感谢 [`sbwml/luci-app-mosdns`](https://github.com/sbwml/luci-app-mosdns) 与 [`IrineSistiana/mosdns`](https://github.com/IrineSistiana/mosdns)。
