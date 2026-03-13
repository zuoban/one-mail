# 邮件同步机制优化设计

**创建日期**: 2026-03-13  
**目标**: 1-2周内完成核心性能优化  
**适用场景**: 个人使用（1-3个账户）

---

## 背景

当前同步机制存在以下性能问题：
1. 每次同步都新建IMAP连接，握手开销大
2. 去重查询效率低（查询所有MessageID）
3. 固定分批策略，不够智能
4. 同步状态仅存内存，重启后丢失

---

## 优化目标

- 同步速度提升 40-60%
- 查询性能提升 50-70%
- 内存占用减少 30%
- 提升系统稳定性

---

## 阶段 1：连接管理优化（3-4天）

### 1.1 IMAP 连接池

**现状问题**:
```go
// worker.go:24-27
client := imap.NewClient(&account)
if err := client.Connect(); err != nil {
    return 0, err
}
defer client.Disconnect()
```

**优化方案**:
- 创建全局连接池管理器
- 每个账户维护一个长连接
- 连接复用，避免频繁握手

**实现要点**:
```go
type ConnectionPool struct {
    connections map[uint]*ConnectionWrapper
    mu          sync.RWMutex
}

type ConnectionWrapper struct {
    client     *imap.Client
    lastUsed   time.Time
    accountID  uint
}
```

**文件变更**:
- 新建: `backend/internal/services/imap/pool.go`
- 修改: `backend/internal/services/sync/worker.go`

---

### 1.2 断线自动重连

**现状问题**: 连接失败直接返回错误，无重试机制

**优化方案**:
- 实现指数退避重连策略
- 最大重试次数：3次
- 重试间隔：1s → 2s → 4s

**实现要点**:
```go
func (p *ConnectionPool) getOrConnect(accountID uint) (*imap.Client, error) {
    for retry := 0; retry < 3; retry++ {
        if client, err := p.connect(accountID); err == nil {
            return client, nil
        }
        time.Sleep(time.Second * time.Duration(math.Pow(2, float64(retry))))
    }
    return nil, errors.New("max retries exceeded")
}
```

---

### 1.3 连接健康检查

**现状问题**: 无法检测连接是否有效

**优化方案**:
- 定期发送 NOOP 命令保活
- 检测到断线自动重连
- 清理长时间未使用的连接

**实现要点**:
```go
func (p *ConnectionPool) healthCheck() {
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        p.checkAllConnections()
    }
}
```

---

## 阶段 2：查询性能优化（2-3天）

### 2.1 布隆过滤器去重

**现状问题**:
```go
// worker.go:90-98
var existingEmails []models.Email
db.Where("account_id = ? AND message_id IN ?", account.ID, messageIDs).
    Select("message_id").
    Find(&existingEmails)
```

**优化方案**:
- 使用布隆过滤器快速判断 MessageID 是否存在
- 减少数据库查询次数
- 允许少量误判（可接受）

**实现要点**:
```go
type SyncCache struct {
    bloomFilter *bloom.BloomFilter
    mu          sync.RWMutex
}

func (c *SyncCache) MightContain(messageID string) bool {
    return c.bloomFilter.TestString(messageID)
}
```

**依赖**: `github.com/bits-and-blooms/bloom/v3`

---

### 2.2 批量插入优化

**现状问题**:
```go
// worker.go:124
if err := db.Create(&newEmails).Error; err != nil {
    return 0, err
}
```

**优化方案**:
- 使用 GORM 的批量创建（已在用，确认批次大小合理）
- 添加批次大小配置（默认100）
- 大批次时自动分片插入

---

### 2.3 索引优化

**现状问题**: 需确认关键字段有索引

**优化方案**:
```go
type Email struct {
    // ...
    MessageID string `gorm:"index:idx_account_message,unique"`
    AccountID uint   `gorm:"index:idx_account_message"`
    UID       uint   `gorm:"index"`
    Folder    string `gorm:"index"`
}
```

**验证**: 运行 `PRAGMA index_list(emails)` 检查现有索引

---

## 阶段 3：同步策略优化（3-4天）

### 3.1 智能分批

**现状问题**:
```go
// worker.go:75
emails, maxUID, err := client.FetchEmailsIncremental(folder, syncState.LastUID, 100)
```

**优化方案**:
- 首次同步：大批次（500）
- 增量同步：小批次（100）
- 根据网络延迟动态调整

**实现要点**:
```go
func getBatchSize(isFirstSync bool, avgLatency time.Duration) int {
    if isFirstSync {
        return 500
    }
    if avgLatency > 2*time.Second {
        return 50
    }
    return 100
}
```

---

### 3.2 FLAGS 变更同步

**现状问题**: 只同步新邮件，不检测已读/星标状态变更

**优化方案**:
- 每次同步时检测 FLAGS 变更
- 更新本地邮件状态
- 支持配置是否启用

**实现要点**:
```go
func syncFlags(db *gorm.DB, account *models.EmailAccount, client *imap.Client, folder string) error {
    // 获取本地邮件 UID 列表
    var localEmails []models.Email
    db.Where("account_id = ? AND folder = ?", account.ID, folder).Find(&localEmails)
    
    // 批量获取远程 FLAGS
    // 对比并更新
}
```

---

### 3.3 同步状态持久化

**现状问题**: `SyncStatus` 只存内存，重启后丢失

**优化方案**:
- 将运行时状态存入数据库
- 重启后从数据库恢复状态
- 避免重复同步

**数据库变更**:
```sql
ALTER TABLE sync_states ADD COLUMN is_syncing BOOLEAN DEFAULT FALSE;
ALTER TABLE sync_states ADD COLUMN error TEXT;
```

---

## 实施计划

### Week 1
- Day 1-2: 连接池实现
- Day 3: 断线重连 + 健康检查
- Day 4: 测试 + 验证

### Week 2
- Day 1-2: 布隆过滤器 + 查询优化
- Day 3: 智能分批 + FLAGS 同步
- Day 4: 状态持久化 + 集成测试

---

## 测试计划

### 单元测试
- 连接池获取/释放/健康检查
- 布隆过滤器准确性
- 批次大小计算逻辑

### 集成测试
- 模拟断线场景
- 高并发同步
- 大量邮件同步

### 性能测试
- 同步速度对比（优化前后）
- 内存占用对比
- 数据库查询次数对比

---

## 风险评估

### 低风险
- ✅ 连接池：独立模块，不影响现有逻辑
- ✅ 布隆过滤器：仅优化查询，不影响正确性

### 中风险
- ⚠️ FLAGS 同步：需要修改同步逻辑，需充分测试
- ⚠️ 状态持久化：需要数据库迁移

### 应对策略
- 每个优化独立提交，可快速回滚
- 保留旧逻辑作为降级方案
- 充分的单元测试和集成测试

---

## 成功指标

- [ ] 同步速度提升 > 40%
- [ ] 数据库查询次数减少 > 50%
- [ ] 连接成功率 > 99%
- [ ] 无内存泄漏
- [ ] 所有测试通过

---

## 后续优化（可选）

- IDLE 推送支持（实时性）
- 邮件删除检测
- 性能监控指标（Prometheus）
- 并发控制（worker pool）
