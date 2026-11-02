[English](CHANGELOG.md) | 简体中文

# 变更日志

本库所有值得记录的变更都写在这里。格式遵循
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/)；版本策略见消费方仓库
（`Version = "0.0.0-dev"` 期间为 pre-1.0）。

## [Unreleased]

### 新增

- 装配层：`Set`、`Select`、`Assembly`、`Module[C]`，可选
  `Versioned`/`Dependent`/`Starter`/`Stopper`、`Static`、`Spec[C]`/`From`。
- 宿主层：`Plugin[H]`、`Registry[H]`，含确定性启动顺序、启动失败回滚、级联停止与状态上报。
- 生命周期钩子：`State`（`StateRegistered`/`StateEnabled`/`StateStopped`）、`Status`、
  `Event`、`Observer`（`WithObserver` 注册）；`Snapshot()` 无副作用地报告每个插件的状态与
  最近一次失败。
- 单品控制：`Start`/`Stop`（依赖闭包、级联停止、幂等）与 `StartAll`/`StopAll`；
  `Enabled()` 列出正在运行的插件。
- `examples/lifecycle`：可运行的状态表，灯是灰（已注册）/绿（运行中）/红（已停止）——颜色在应用侧。

### 变更

- **破坏性（尚无消费方）**：`Plugin.Load`/`Unload` 改为 `Start(H)`/`Stop()`；
  `Registry.Load`/`Unload`/`Loaded` 改为 `Start`/`Stop`/`Enabled`，并新增 `StartAll`/`StopAll`。
  启动就是装载、停止就是卸载，从库到运维只用一套词。
- `Registry` 在构造时绑定宿主（`NewRegistry(host, opts...)`）：插件可能在开机很久之后才启动，
  必须拿到同一个宿主。
