# F8：对帐差异与修复

[English](phase-f8-reconciliation.md) | [繁體中文](phase-f8-reconciliation.zh-TW.md) | **简体中文**


## 范围

`RunReconciliation` 读取本地订阅、权益、assignment、final invoice line、付款与 outbox，再读 fake provider capture。每次保存 run ID、比较时间及差异证据。相同种类与对象沿用差异 ID，后续比较能更新证据或标记 resolved；旧纪录保留供稽核。

CLI 主菜单「对帐、修复与人工决议」可运行比较、查差异、依差异 ID 修复或记录人工决议；「状态查找」也能列出差异。每个修复必须提供稳定 request key。相同 key 重播回原操作；不同差异共用 key 会冲突。

## 分类与动作

| 差异 | 分类 | 动作 |
| --- | --- | --- |
| 缺失或旧版权益投影、待处理权益 outbox | `SAFE_AUTO_REPAIR` | 依订阅与帐单来源重建投影，重新比较。 |
| 待处理 capture outbox | `RETRY_REQUIRED`；若付款已 submitted／unknown，为 `EXTERNAL_LOOKUP_REQUIRED` | 指定原 payment operation 重送；未知结果只查原 provider key。 |
| provider 成功但本地未观测、付款结果未知 | `EXTERNAL_LOOKUP_REQUIRED` | 查原 key 并套用金额／币别检查。 |
| 未知 provider capture、金额／币别不符、缺合约后续价 | `MANUAL_REVIEW` | 阻止自动修复，保存人工决议与审查者。 |
| assignment 与订阅价不一致、final invoice line 金额不一致 | `UNSAFE_TO_REPAIR` | 阻止自动修复，保存人工决议。 |

修复计划保存 expected、actual、来源 revision。运行前重新对帐；来源证据、分类或 revision 改变便 blocked，必须用新 request key。运行后再次对帐，原差异消失才标 verified；仍存在则保持 executed／waiting，供后续调查。若 provider 金额不符，付款修复会 blocked。人工决议只记录审查，不会改写付款、发票或价格事实。

## 验证与限制

`go test ./lab -run TestS12 -count=1` 覆盖缺权益重建、重播、来源 revision 改变、指定 capture 重送、UNKNOWN 原 key 查找，以及陌生或金额不符的 provider capture。`go test ./...` 覆盖既有商务情境。

比较是本地与 fake provider 的当前观测，不提供任意历史时间点的快照。`cutoff_at` 是此次比较的水位标记；`local_observed_at` 与 `provider_observed_at` 分别记录实际读取时间。待处理 outbox 会出现在差异中；目前没有年龄门槛，因此它可表示正常排队工作，操作员需按原流程或指定修复运行。此实作不连接真实支付系统，也不取代财务总帐对帐。
