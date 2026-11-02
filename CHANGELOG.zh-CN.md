[English](CHANGELOG.md) | 简体中文

# 变更日志

本库所有值得记录的变更都写在这里。格式遵循
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/)；版本策略见消费方仓库
（`Version = "0.0.0-dev"` 期间为 pre-1.0）。

## [Unreleased]

### 新增

- 装配层：`Set`、`Select`、`Assembly`、`Module[C]`，可选
  `Versioned`/`Dependent`/`Starter`/`Stopper`、`Static`、`Spec[C]`/`From`。
- 宿主层：`Plugin[H]`、`Registry[H]`，含确定性装载顺序、装载失败回滚、逆序卸载与清单上报。
