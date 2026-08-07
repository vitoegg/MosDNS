# OpenMosDNS

✨ 基于 [`sbwml/luci-app-mosdns`](https://github.com/sbwml/luci-app-mosdns) `v5` 的个性化版本。

### 🖇️ 自定义修改

1. 同步上游代码后应用 OpenMosDNS 自定义内容
   > **Custom patches**: `maint/patches/`  
   > **Maintenance scripts**: `maint/scripts/`
2. 去掉 `v2ray-geoip` / `v2ray-geosite` / `v2dat` 依赖与打包
3. 启动时不再执行 `v2dat_dump`
4. 修复 ImageBuilder 安装阶段 init 顶层调用 `uci` 的问题

### 🙏 致谢

感谢 [`sbwml/luci-app-mosdns`](https://github.com/sbwml/luci-app-mosdns) 与 [`IrineSistiana/mosdns`](https://github.com/IrineSistiana/mosdns)。
