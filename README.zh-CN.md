[English](README.md) | 简体中文

# gplugin

框架层与协议库共用的插件机制——两层解决两个不同的问题，零第三方依赖。

```go
type Module[C any] interface {  // 装配：注册进容器 C 的能力
    ID() string
    Register(C) error
}

type Plugin[H any] interface {  // 宿主扩展：挂到活的宿主 H 上的扩展
    Name() string
    Load(H) error
    Unload() error
}
```

`C` 与 `H` 永远是调用方自己声明的类型。本包不认识任何协议、框架或容器——正因如此，
应用框架与 MQTT broker 才能共用同一套机制而不共享任何语义。

## 1. 装配层：`Set` / `Select` / `Assembly`

应用声明要哪些能力，集合解析依赖闭包、按拓扑排序，并给出清单。

```go
set := gplugin.NewSet[*gable.Container]()
set.Add(logCapability)                                                     // id "log"
set.Add(gplugin.Static("cache", "1.0.0", cacheCapability))                  // DependsOn: log
set.Add(gplugin.Static("web", "1.0.0", webCapability))                      // DependsOn: cache

assembly, err := set.Select("web")   // 解析 log -> cache -> web
if err != nil {
    return err                       // 未知能力、缺依赖、成环
}
defer assembly.Stop(ctx)             // 逆序关停，错误合并返回
if err := assembly.Start(ctx, container); err != nil {
    return err                       // 已启动的能力已被回滚
}
for _, entry := range assembly.Manifest() {
    log.Printf("capability %s@%s needs %v", entry.ID, entry.Version, entry.DependsOn)
}
```

保证：

- **顺序确定**：依赖在前，同层按 id 排序，因此每次进程启动的装配顺序一致，滚动重启不会换序。
- **失败即启动错误**：未知能力、未被选中的依赖、成环都报错，绝不悄悄缩小装配结果。
- **无包级注册表、不 panic**：装配是显式的、可独立测试的；重名是调用方看到的 error。
- **生命周期可选**：纯接线能力不必实现 `Start`/`Stop`，它不会被记作已启动，回滚也不会停它。

`From(Spec[C])` 用来适配"`Register` 收自己具体容器类型、或自带生命周期接口"的模块，
不需要改它仓库里的任何签名。

## 2. 宿主扩展层：`Plugin` / `Registry`

库持有宿主对象，让具名扩展挂上去。

```go
reg := gplugin.NewRegistry[*Broker]()
reg.Add(authPlugin)                 // Name() "auth"
reg.Add(metricsPlugin)              // Name() "metrics", DependsOn: []string{"auth"}

entries, err := reg.Manifest()      // 只算装载顺序，不真的装载
if err := reg.Load(broker); err != nil {
    return err                      // 失败前已装载的插件已被卸载
}
defer reg.Unload()                  // 逆序卸载，错误合并返回
```

保证：重名报错；`Load` 失败会卸载已装载者；`Unload` 从最新装载的开始并合并所有失败；
重复 `Load` 被拒绝（否则钩子会注册两遍）。

## 3. 本包不做的事

- **不做动态插件**：没有 `.so`、没有运行时下载、没有服务发现。插件编译进二进制，
  本包决定装配什么、以什么顺序装配，而不是运行时存在什么。
- **不含协议语义**：钩子、事件与生命周期留在拥有它们的库里。
- **没有全局状态**：包初始化阶段不注册任何东西。

## 4. 依赖位置

`gplugin` 位于框架层与协议库之下：谁都可以 import 它，它不 import 任何一方。
依赖规则见消费方仓库的说明。

## 版本

`Version = "0.0.0-dev"`——pre-1.0，暂不承诺 API 稳定。

## 许可

MIT，见 [LICENSE](LICENSE)。
