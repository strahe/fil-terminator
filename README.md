# fil-terminator

Filecoin 矿工扇区终结费用计算工具，支持历史查询、未来预估和批量计算。

## 安装

```bash
git clone https://github.com/strahe/fil-terminator.git
cd fil-terminator
make
```

## 基本使用

### 计算终结费用

```bash
# 计算指定扇区
./fil-terminator calc --miner f01234 --sectors 1,2,3

# 计算所有扇区
./fil-terminator calc --miner f01234 --all

# 计算指定高度的费用
./fil-terminator calc --miner f01234 --all --epoch 2000000

# 显示详细信息
./fil-terminator calc --miner f01234 --sectors 1-10 --verbose
```

### 批量计算

```bash
# 从 CSV 文件批量计算
./fil-terminator batch --input miners.csv --output results.csv
```

CSV 格式：
```csv
minerid,epoch
f01234,2000000
f05678,2100000
```

### 工具功能

```bash
# epoch 转时间
./fil-terminator tools epoch-to-time --epoch 2000000
./fil-terminator tools e2t --epoch 2000000

# 时间转 epoch
./fil-terminator tools time-to-epoch --time "2024-01-01 12:00:00"
./fil-terminator tools t2e --time "2024-01-01 12:00:00"

# 离线模式（无需连接节点）
./fil-terminator tools e2t --epoch 2000000 --offline

# 指定时区
./fil-terminator tools e2t --epoch 2000000 --timezone UTC
```

**支持的时间格式：**
- `2024-01-01 12:00:00` (本地时间)
- `2024-01-01T12:00:00Z` (UTC 时间)
- `2024-01-01T12:00:00+08:00` (指定时区)
- `2024-01-01` (日期)
- `01/01/2024 12:00:00` (US 格式)

## 环境要求

- Go 1.24.3+
- Lotus 节点 (已同步)
- 支持网络版本 16+

## 时间换算

- 1 天 = 2880 epochs
- 1 周 = 20160 epochs
- 1 月 = 86400 epochs

## 许可证

MIT
