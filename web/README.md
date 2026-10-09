# 静态号码生成页面

整个 `web` 目录可以直接部署，运行时只需要 `index.html`、`styles.css`、`lottery-core.js` 和 `app.js`。不需要 Node.js、安装依赖、构建、后端接口或外部 CDN，也可以双击 `index.html` 本地使用。

支持大乐透与双色球、单式/复式、多组、共享号码、倍数、连续期数和大乐透追加。默认值与项目根目录的 `lottery.js` 一致。切换彩种会恢复该彩种的选号数量并清空共享数量；切换至双色球时取消追加。修改设置后，点击生成才更新号码和金额，复制的也是已生成结果。

浏览器使用 `crypto.getRandomValues` 和拒绝采样生成均匀随机数。共享号码是最低交集；其他位置也可能偶然重复，各组分别计费。页面每次最多生成 1,000 组，超出 JavaScript 安全整数精度的金额会被拒绝。仅供选号和金额计算，不包含预测、购买或验奖功能。

## Nginx 部署

例如，将此目录的全部内容复制到 `/var/www/z-lottery/`，使用以下配置（替换域名）：

```nginx
server {
    listen 80;
    server_name lottery.example.com;
    root /var/www/z-lottery;
    index index.html;

    charset utf-8;

    location / {
        try_files $uri $uri/ =404;
    }
}
```

确保 Nginx 的 `http` 配置中包含 `include mime.types;`，然后运行 `nginx -t` 并重载 Nginx。

也可以复制到现有站点根目录下的 `lottery/` 文件夹，通过 `/lottery/` 访问。页面所有资源都使用相对路径，无需修改代码或配置前端路由回退。

复制结果仅包含彩种、选号规格、倍数、期数和各组号码，不包含追加标记、共享数量、注数或金额。号码按两位数字显示，以空格分隔，各组之间留一个空行。复制按钮优先使用安全上下文的 Clipboard API，普通 HTTP 或本地文件尝试浏览器的兼容复制方式；若浏览器禁止复制，会提示手动选择复制。

## 验证

在项目根目录运行：

```bash
node --test lottery.test.js web.test.js
```
