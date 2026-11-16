# 添加到桌面 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给电脑和手机提供“添加到桌面”入口及相应的原生安装或手动指引。

**Architecture:** 原生页面加载独立 install.js；manifest 和 PNG 图标进入 Go embed 与 HTTP 静态白名单。安装判断只在浏览器完成，不影响业务接口，不增加缓存或依赖。

**Tech Stack:** Go、goja、HTML/CSS/JavaScript、Web App Manifest。

**Spec:** ../specs/2026-11-16-add-to-desktop-design.md

## Global Constraints

- 在当前分支修改，不提交、推送、部署；不读取 .env 或生产数据。
- 相对资源路径，兼容根路径及反向代理子目录。
- 仅用户点击触发原生安装，不宣称取消或失败为成功；不新增 service worker/离线缓存。

## Review Focus

- iPad 桌面 UA 需结合触控点识别；内置浏览器需优先提示外部打开。
- HTTP/file 场景提供可操作说明。
- 安装提示仅使用一次，等待期间禁止重复点击。
- appinstalled 或 display-mode 在异步结果前改变时不能复活入口。
- manifest、图标路径和 GET/HEAD MIME 在子目录代理后仍正确。

### Task 1: 安装资产与服务

**Files:** lottery/manifest.webmanifest、lottery/icon-{180,192,512}.png、lottery/embed.go、internal/httpapi/http.go、internal/httpapi/install_test.go。

**Interfaces:** GET/HEAD 安装资产，manifest.start_url/scope 为 ./，省略 id 以使用实际启动地址作为身份，PNG 尺寸对应文件名。

- [x] 先测试实际嵌入资源的路由、MIME、HEAD、manifest 相对路径与图标尺寸，运行 `go test ./internal/httpapi -run TestInstall -count=1`，确认缺少资产失败。
- [x] 添加 manifest，等比例导出现有 SVG 为 PNG，加入 embed/精确白名单；显式提供 application/manifest+json。
- [x] 重跑上面的命令，预期全部通过。

### Task 2: 页面交互与交付

**Files:** lottery/index.html、lottery/install.js、lottery/styles.css、lottery/install_test.go、lottery/README.md。

**Interfaces:** #install-button、#install-dialog、#install-description、#install-steps、#close-install；beforeinstallprompt/appinstalled/display-mode 事件。

- [x] 先用 goja 执行真实 install.js，覆盖多设备指引、安装提示消费、取消/异常、异步安装事件与独立窗口检测；运行 `go test ./lottery -run TestInstall -count=1`，确认缺少脚本失败。
- [x] 实现按钮和可访问 dialog，保持已有颜色、窄屏布局与焦点行为；同步静态部署及 HTTPS 使用说明。
- [x] 重跑上述回归测试并运行 `go run ./tools/check`，预期全部通过。
- [x] 使用本机临时数据、关闭开奖调度，浏览器检查桌面/手机、子目录和安装交互；清理本次临时进程，记录真实设备/容器未测边界。

## 验证记录（2026-11-16）

- `go test -p 2 ./internal/httpapi ./lottery -run TestInstall -count=1`：初次因安装资源/脚本缺失失败，完成实现后通过；交互覆盖 17 个场景。
- 只读代码复核发现相对 id 的解析基址是 origin，不能区分子目录。按实际解析规则补充断言后，`go test ./internal/httpapi -run TestInstallManifest -count=1` 先失败，省略 id 后完整检查通过。
- `go run ./tools/check`：最终通过 gofmt、vet、全部测试及全部包构建。
- 本机 Go 服务临时 SQLite，`CHECK_ENABLED=false`：桌面与 iPhone/安卓 UA、390/320 像素宽度，无横向溢出；指引内容正确；关闭按钮/Esc 可关闭并恢复入口焦点；浏览器无错误/警告。
- 本机反向代理 `/lottery/`：manifest、192/512 图标、相对启动/scope 地址及只读记录接口正常；应用身份使用子目录启动地址。
- 未执行真实 iOS/安卓/macOS 安装、CI race 和容器门禁；本机无 docker 命令。桌面移动模拟不能代替真机系统安装验收。未提交、推送或部署。
