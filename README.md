# 锻造台 ForgeBoard

OpenClaw 的模型服务管理面板。Windows 单文件，免安装。

## 功能

- 模型服务起停与档位切换：双显卡分配、显存实测、启动前探测
- 每个 OpenClaw 对话窗口单独指定模型；定时任务、心跳、上下文压缩可各自指定模型
- OpenClaw 用量面板：今日／7 日／30 日用量与花费，按模型分列，后台自采，打开页面不等待网关
- 服务体检：网关、路由器、换模型器、各模型服务，可起停与重启
- 开机计划任务与开机启动项展示，可开关、可删除
- 中英双语界面
- 配置外置，换机自动寻径

## 安装

下载 `forgeboard-v2.5.0-windows.zip`，解压后双击 `forgeboard.exe`（界面）或 `forgectl.exe`（命令行）。
未经数字签名，SmartScreen 提示「未知发布者」时：右键 → 属性 → 解除锁定。

## 命令行

```
forgectl scan | list | start | stop | status | calib | doctor | plan | reload | usage | version
```

## 从源码构建

```
go build -tags "production,swapembed,wv2runtime.browser" -trimpath -ldflags "-s -w -H=windowsgui" -o bin/forgeboard.exe ./cmd/forgeboard
go build -trimpath -ldflags "-s -w" -o bin/forgectl.exe ./cmd/forgectl
```

界面资源在 `cmd/forgeboard/frontend/dist/`，由 Go 打进二进制。内置 llama-swap（MIT）。

## 许可

MIT
