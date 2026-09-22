package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wang4386/CDT-Monitor/internal/aliyun"
	"github.com/wang4386/CDT-Monitor/internal/domain"
	"github.com/wang4386/CDT-Monitor/internal/notify"
	"github.com/wang4386/CDT-Monitor/internal/security"
	"github.com/wang4386/CDT-Monitor/internal/store"
)

const (
	JobMonitorAccount  = "monitor_account"
	JobRefreshAccount  = "refresh_account"
	JobControlInstance = "control_instance"
	JobTestNotify      = "test_notification"
)

type Engine struct {
	store        *store.Store
	provider     aliyun.Provider
	notify       *notify.Service
	logger       *slog.Logger
	owner        string
	wake         chan struct{}
	workers      int
	started      sync.Once
	accountLocks sync.Map
	extraRegions map[string]string
}

var ErrMonitorBusy = errors.New("monitor scheduler lease is held by another process")

func New(st *store.Store, provider aliyun.Provider, notifier *notify.Service, logger *slog.Logger, workers int, extraRegions ...domain.Region) *Engine {
	if workers < 1 {
		workers = 4
	}
	owner, _ := security.NewToken(12)
	names := make(map[string]string, len(extraRegions))
	for _, region := range extraRegions {
		names[region.Value] = region.Label
	}
	return &Engine{store: st, provider: provider, notify: notifier, logger: logger, owner: owner, wake: make(chan struct{}, 1), workers: workers, extraRegions: names}
}

func (e *Engine) Start(ctx context.Context) {
	e.started.Do(func() {
		go e.scheduler(ctx)
		for index := 0; index < e.workers; index++ {
			go e.worker(ctx, index)
		}
		go e.notificationWorker(ctx)
	})
}

func (e *Engine) Enqueue(ctx context.Context, jobType string, accountID int64, payload, uniqueKey string) (domain.Job, error) {
	job, err := e.store.EnqueueJob(ctx, jobType, accountID, payload, uniqueKey, 3)
	if err == nil {
		e.signal()
	}
	return job, err
}

func (e *Engine) EnqueueRefreshAll(ctx context.Context) ([]domain.Job, error) {
	accounts, err := e.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	minute := time.Now().UTC().Format("200601021504")
	jobs := make([]domain.Job, 0, len(accounts))
	for _, account := range accounts {
		job, enqueueErr := e.Enqueue(ctx, JobRefreshAccount, account.ID, `{}`, JobUniqueKey(JobRefreshAccount, account.ID, minute))
		if enqueueErr != nil {
			return jobs, enqueueErr
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (e *Engine) RunOnce(ctx context.Context) error {
	acquired, err := e.store.AcquireLease(ctx, "monitor", e.owner, 75*time.Second)
	if err != nil {
		return err
	}
	if !acquired {
		return ErrMonitorBusy
	}
	return e.enqueueMonitorCycle(ctx, time.Now())
}

func (e *Engine) scheduler(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	_ = e.RunOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := e.RunOnce(ctx); err != nil && !errors.Is(err, ErrMonitorBusy) {
				e.logger.Warn("scheduler cycle skipped", "error", err)
			}
			if now.Minute()%30 == 0 && now.Second() < 15 {
				_ = e.store.Prune(ctx)
			}
		}
	}
}

func (e *Engine) enqueueMonitorCycle(ctx context.Context, now time.Time) error {
	accounts, err := e.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	minute := now.UTC().Format("200601021504")
	for _, account := range accounts {
		uniqueKey := fmt.Sprintf("monitor:%d:%s", account.ID, minute)
		if _, err = e.Enqueue(ctx, JobMonitorAccount, account.ID, `{}`, uniqueKey); err != nil {
			return err
		}
	}
	return e.store.SetLastMonitorRun(ctx, now.UTC())
}

func (e *Engine) worker(ctx context.Context, index int) {
	ticker := time.NewTicker(800 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.wake:
		case <-ticker.C:
		}
		for {
			job, err := e.store.ClaimJob(ctx)
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			if err != nil {
				e.logger.Error("claim job", "worker", index, "error", err)
				break
			}
			result, runErr := e.runJob(ctx, job)
			if runErr != nil {
				e.logger.Warn("job failed", "job_id", job.ID, "type", job.Type, "error", runErr)
				_ = e.store.FailJob(ctx, job, runErr)
				continue
			}
			_ = e.store.CompleteJob(ctx, job.ID, result)
		}
	}
}

func (e *Engine) runJob(ctx context.Context, job domain.Job) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	switch job.Type {
	case JobMonitorAccount:
		return e.processAccount(ctx, job.AccountID, false)
	case JobRefreshAccount:
		return e.processAccount(ctx, job.AccountID, true)
	case JobControlInstance:
		var payload struct {
			Action string `json:"action"`
			Source string `json:"source"`
		}
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return "", err
		}
		return e.control(ctx, job.AccountID, payload.Action, payload.Source)
	case JobTestNotify:
		var payload struct {
			Channel string `json:"channel"`
		}
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return "", err
		}
		config, err := e.store.GetConfig(ctx)
		if err != nil {
			return "", err
		}
		event := newEvent("test", "通知通道测试", "CDT Monitor 的通知配置工作正常。", 0, map[string]string{"发送时间": time.Now().Format("2006-01-02 15:04:05")})
		return "notification sent", e.notify.Send(ctx, payload.Channel, event, config)
	default:
		return "", fmt.Errorf("unknown job type %q", job.Type)
	}
}

func (e *Engine) processAccount(ctx context.Context, accountID int64, force bool) (string, error) {
	lock := e.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	config, err := e.store.GetConfig(ctx)
	if err != nil {
		return "", err
	}
	account, err := e.store.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	secret, err := e.store.AccountSecret(ctx, accountID)
	if err != nil {
		return "", err
	}
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		location = time.FixedZone("CST", 8*3600)
	}
	now := time.Now().In(location)

	actions := make([]string, 0, 2)
	statusChangedBySchedule := false
	if account.ScheduleEnabled {
		if dueWithin(now, account.StartTime, 10*time.Minute) {
			changed, runErr := e.executeScheduledAction(ctx, config, account, secret, "start", now)
			if runErr != nil {
				return "", runErr
			}
			if changed {
				actions = append(actions, "scheduled_start")
				account.InstanceStatus = domain.StatusStarting
				statusChangedBySchedule = true
			}
		}
		if dueWithin(now, account.StopTime, 10*time.Minute) {
			changed, runErr := e.executeScheduledAction(ctx, config, account, secret, "stop", now)
			if runErr != nil {
				return "", runErr
			}
			if changed {
				actions = append(actions, "scheduled_stop")
				account.InstanceStatus = domain.StatusStopping
				statusChangedBySchedule = true
			}
		}
	}

	interval := time.Duration(config.APIInterval) * time.Second
	if transient(account.InstanceStatus) {
		interval = time.Minute
	}
	due := force || account.UpdatedAt.IsZero() || time.Since(account.UpdatedAt) >= interval || now.Minute() == 0 || statusChangedBySchedule
	traffic, status := account.TrafficUsed, account.InstanceStatus
	if due {
		var trafficErr, statusErr error
		var wait sync.WaitGroup
		wait.Add(2)
		go func() {
			defer wait.Done()
			traffic, trafficErr = e.provider.GetTraffic(ctx, account, secret)
		}()
		go func() {
			defer wait.Done()
			status, statusErr = e.provider.GetInstanceStatus(ctx, account, secret)
		}()
		wait.Wait()
		if trafficErr != nil {
			traffic = account.TrafficUsed
			_ = e.store.AddLog(ctx, "error", fmt.Sprintf("流量查询失败 [%s]: %v", masked(account.AccessKeyID), trafficErr))
		}
		if statusErr != nil || status == "" {
			status = account.InstanceStatus
			_ = e.store.AddLog(ctx, "error", fmt.Sprintf("实例状态查询失败 [%s]: %v", masked(account.AccessKeyID), statusErr))
		}
		updatedAt := time.Now().UTC()
		if trafficErr != nil && statusErr != nil {
			updatedAt = account.UpdatedAt
		}
		if statusChangedBySchedule {
			if slices.Contains(actions, "scheduled_start") {
				status = domain.StatusStarting
			} else if slices.Contains(actions, "scheduled_stop") {
				status = domain.StatusStopping
			}
		}
		if err = e.store.UpdateRuntime(ctx, account.ID, traffic, status, updatedAt); err != nil {
			return "", err
		}
		if trafficErr == nil {
			_ = e.store.AddTrafficStats(ctx, account.ID, traffic, now)
		}
	}

	percentage := usagePercent(traffic, account.MaxTraffic)
	overThreshold := percentage >= float64(config.TrafficThreshold)
	thresholdKey := fmt.Sprintf("threshold:%d:active", account.ID)
	if !overThreshold {
		_ = e.store.DeleteActionEvent(ctx, thresholdKey)
	}
	if overThreshold && due {
		key := thresholdKey
		recorded, recordErr := e.store.RecordActionEvent(ctx, key, account.ID, "threshold", "detected", fmt.Sprintf("%.2f%%", percentage))
		if recordErr != nil {
			return "", recordErr
		}
		if recorded {
			if config.ThresholdAction == "stop_and_notify" && status != domain.StatusStopped && status != domain.StatusStopping {
				if err = e.provider.ControlInstance(ctx, account, secret, "stop", config.ShutdownMode); err != nil {
					_ = e.store.DeleteActionEvent(ctx, key)
					return "", err
				}
				status = domain.StatusStopping
				_ = e.store.UpdateRuntime(ctx, account.ID, traffic, status, time.Now().UTC())
				actions = append(actions, "threshold_stop")
			}
			event := newEvent("threshold", "流量阈值告警", fmt.Sprintf("账号 %s 的流量使用率达到 %.2f%%。", masked(account.AccessKeyID), percentage), account.ID, map[string]string{
				"当前流量": fmt.Sprintf("%.2f GB", traffic), "设定阈值": fmt.Sprintf("%d%%", config.TrafficThreshold), "实例状态": status,
			})
			_ = e.store.AddOutbox(ctx, event, notify.EnabledChannels(config))
			_ = e.store.AddLog(ctx, "warning", event.Summary)
		}
	}

	if config.KeepAlive && !overThreshold && !statusChangedBySchedule && status == domain.StatusStopped && (!account.ScheduleEnabled || inTimeRange(now.Format("15:04"), account.StartTime, account.StopTime)) {
		key := fmt.Sprintf("keepalive:%d:%s", account.ID, now.Format("200601021504"))
		fresh, recordErr := e.store.RecordActionEvent(ctx, key, account.ID, "keepalive", "attempting", "")
		if recordErr != nil {
			return "", recordErr
		}
		if fresh {
			if err = e.provider.ControlInstance(ctx, account, secret, "start", config.ShutdownMode); err != nil {
				_ = e.store.DeleteActionEvent(ctx, key)
				return "", err
			}
			status = domain.StatusStarting
			_ = e.store.UpdateRuntime(ctx, account.ID, traffic, status, time.Now().UTC())
			_ = e.store.UpdateKeepAliveAt(ctx, account.ID, time.Now().UTC())
			actions = append(actions, "keepalive_start")
			event := newEvent("keepalive", "实例保活启动", "检测到实例在允许运行时段意外停止，已发送启动指令。", account.ID, map[string]string{"账号": masked(account.AccessKeyID), "实例": account.InstanceID})
			_ = e.store.AddOutbox(ctx, event, notify.EnabledChannels(config))
		}
	}

	if config.EnableBilling {
		var balance aliyun.BillingBalance
		balanceCached, _ := e.store.BillingCache(ctx, account.ID, "balance", "", 6*time.Hour, &balance)
		billCached := true
		if account.InstanceID != "" {
			var bill aliyun.BillingBill
			billCached, _ = e.store.BillingCache(ctx, account.ID, "instance_bill", now.Format("2006-01"), 6*time.Hour, &bill)
		}
		if force || now.Hour()%6 == 0 || !balanceCached || !billCached {
			if billingErr := e.refreshBilling(ctx, account, secret, now); billingErr != nil {
				_ = e.store.AddLog(ctx, "error", fmt.Sprintf("账单查询失败 [%s]: %v", masked(account.AccessKeyID), billingErr))
			}
		}
	}
	message := fmt.Sprintf("[%s] 流量 %.2fGB / %.2fGB (%.2f%%) · 状态 %s", masked(account.AccessKeyID), traffic, account.MaxTraffic, percentage, status)
	if len(actions) > 0 {
		message += " · 动作 " + strings.Join(actions, ",")
	}
	_ = e.store.AddLog(ctx, "heartbeat", message)
	return message, nil
}

func (e *Engine) executeScheduledAction(ctx context.Context, config domain.Config, account domain.Account, secret, action string, now time.Time) (bool, error) {
	key := fmt.Sprintf("schedule:%d:%s:%s", account.ID, now.Format("20060102"), action)
	fresh, err := e.store.RecordActionEvent(ctx, key, account.ID, "schedule_"+action, "attempting", "")
	if err != nil || !fresh {
		return false, err
	}
	if err = e.provider.ControlInstance(ctx, account, secret, action, config.ShutdownMode); err != nil {
		_ = e.store.DeleteActionEvent(ctx, key)
		return false, err
	}
	status := domain.StatusStarting
	if action == "stop" {
		status = domain.StatusStopping
	}
	_ = e.store.UpdateRuntime(ctx, account.ID, account.TrafficUsed, status, time.Now().UTC())
	_ = e.store.AddLog(ctx, "info", fmt.Sprintf("执行定时%s [%s]", map[string]string{"start": "开机", "stop": "关机"}[action], masked(account.AccessKeyID)))
	if config.EnableScheduleMail {
		event := newEvent("schedule", "定时任务已执行", fmt.Sprintf("实例定时%s指令已发送。", map[string]string{"start": "开机", "stop": "关机"}[action]), account.ID, map[string]string{"账号": masked(account.AccessKeyID), "实例": account.InstanceID})
		_ = e.store.AddOutbox(ctx, event, notify.EnabledChannels(config))
	}
	return true, nil
}

func (e *Engine) control(ctx context.Context, accountID int64, action, source string) (string, error) {
	lock := e.accountLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	account, err := e.store.GetAccount(ctx, accountID)
	if err != nil {
		return "", err
	}
	config, err := e.store.GetConfig(ctx)
	if err != nil {
		return "", err
	}
	action = strings.ToLower(action)
	if action != "start" && action != "stop" {
		return "", errors.New("action must be start or stop")
	}
	if transient(account.InstanceStatus) {
		return "", fmt.Errorf("instance is currently %s", account.InstanceStatus)
	}
	if config.KeepAlive && action == "stop" {
		return "", errors.New("manual shutdown is disabled while keep-alive is enabled")
	}
	secret, err := e.store.AccountSecret(ctx, accountID)
	if err != nil {
		return "", err
	}
	if err = e.provider.ControlInstance(ctx, account, secret, action, config.ShutdownMode); err != nil {
		return "", err
	}
	status := domain.StatusStarting
	if action == "stop" {
		status = domain.StatusStopping
	}
	if err = e.store.UpdateRuntime(ctx, account.ID, account.TrafficUsed, status, time.Now().UTC()); err != nil {
		return "", err
	}
	message := fmt.Sprintf("%s控制实例 [%s]：%s", source, masked(account.AccessKeyID), action)
	_ = e.store.AddLog(ctx, "audit", message)
	return message, nil
}

func (e *Engine) accountLock(accountID int64) *sync.Mutex {
	value, _ := e.accountLocks.LoadOrStore(accountID, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (e *Engine) refreshBilling(ctx context.Context, account domain.Account, secret string, now time.Time) error {
	cycle := now.Format("2006-01")
	setBillingError := func(err error) {
		_ = e.store.SetBillingCache(ctx, account.ID, "error", "", map[string]string{"message": err.Error()})
	}
	var balance aliyun.BillingBalance
	cached, _ := e.store.BillingCache(ctx, account.ID, "balance", "", 6*time.Hour, &balance)
	if !cached {
		value, err := e.provider.GetAccountBalance(ctx, account, secret)
		if err != nil {
			setBillingError(err)
			return err
		}
		balance = value
		_ = e.store.SetBillingCache(ctx, account.ID, "balance", "", balance)
	}
	if account.InstanceID != "" {
		var bill aliyun.BillingBill
		cached, _ = e.store.BillingCache(ctx, account.ID, "instance_bill", cycle, 6*time.Hour, &bill)
		if !cached {
			value, err := e.provider.GetInstanceBill(ctx, account, secret, cycle)
			if err != nil {
				setBillingError(err)
				return err
			}
			_ = e.store.SetBillingCache(ctx, account.ID, "instance_bill", cycle, value)
		}
	}
	_ = e.store.SetBillingCache(ctx, account.ID, "error", "", map[string]string{"message": ""})
	return nil
}

func (e *Engine) notificationWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for {
				item, err := e.store.ClaimOutbox(ctx)
				if errors.Is(err, sql.ErrNoRows) {
					break
				}
				if err != nil {
					e.logger.Error("claim notification", "error", err)
					break
				}
				var event domain.NotificationEvent
				if err = json.Unmarshal([]byte(item.Payload), &event); err == nil {
					var config domain.Config
					config, err = e.store.GetConfig(ctx)
					if err == nil {
						sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
						err = e.notify.Send(sendCtx, item.Channel, event, config)
						cancel()
					}
				}
				if err != nil {
					_ = e.store.FailOutbox(ctx, item, err)
					continue
				}
				_ = e.store.CompleteOutbox(ctx, item.ID)
			}
		}
	}
}

func (e *Engine) signal() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func dueWithin(now time.Time, hhmm string, window time.Duration) bool {
	parsed, err := time.Parse("15:04", hhmm)
	if err != nil {
		return false
	}
	target := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
	delta := now.Sub(target)
	return delta >= 0 && delta <= window
}

func inTimeRange(current, start, end string) bool {
	if start == "" || end == "" {
		return false
	}
	if start < end {
		return current >= start && current < end
	}
	return current >= start || current < end
}

func transient(status string) bool {
	return status == domain.StatusStarting || status == domain.StatusStopping || status == "Pending" || status == domain.StatusUnknown
}

func usagePercent(traffic, maxTraffic float64) float64 {
	if maxTraffic <= 0 {
		return 0
	}
	return math.Round((traffic/maxTraffic)*10000) / 100
}

func masked(accessKeyID string) string {
	if len(accessKeyID) <= 7 {
		return accessKeyID + "***"
	}
	return accessKeyID[:7] + "***"
}

func newEvent(eventType, title, summary string, accountID int64, fields map[string]string) domain.NotificationEvent {
	id, _ := security.NewToken(18)
	return domain.NotificationEvent{ID: id, Type: eventType, Title: title, Summary: summary, AccountID: accountID, Fields: fields, CreatedAt: time.Now().UTC()}
}

func (e *Engine) Summary(ctx context.Context) ([]domain.AccountSummary, time.Time, error) {
	config, err := e.store.GetConfig(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	lastRun, err := e.store.LastMonitorRun(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	result := make([]domain.AccountSummary, 0, len(config.Accounts))
	for _, account := range config.Accounts {
		percentage := usagePercent(account.TrafficUsed, account.MaxTraffic)
		item := domain.AccountSummary{
			ID: account.ID, Account: masked(account.AccessKeyID), Remark: account.Remark, Region: account.RegionID, RegionName: e.regionName(account.RegionID),
			FlowTotal: account.MaxTraffic, FlowUsed: math.Round(account.TrafficUsed*100) / 100, Percentage: percentage, Threshold: config.TrafficThreshold,
			OverThreshold: percentage >= float64(config.TrafficThreshold), InstanceStatus: account.InstanceStatus, LastUpdated: account.UpdatedAt,
			Stale: account.UpdatedAt.IsZero() || time.Since(account.UpdatedAt) > time.Duration(max(config.APIInterval*2, 180))*time.Second,
		}
		if config.EnableBilling {
			var billingError struct {
				Message string `json:"message"`
			}
			if ok, _ := e.store.BillingCache(ctx, account.ID, "error", "", 7*24*time.Hour, &billingError); ok {
				item.BillingError = strings.TrimSpace(billingError.Message)
			}
			var balance aliyun.BillingBalance
			if ok, _ := e.store.BillingCache(ctx, account.ID, "balance", "", 7*24*time.Hour, &balance); ok {
				item.Balance, item.Currency = &balance.Amount, balance.Currency
			}
			var bill aliyun.BillingBill
			if ok, _ := e.store.BillingCache(ctx, account.ID, "instance_bill", time.Now().Format("2006-01"), 7*24*time.Hour, &bill); ok {
				item.MonthlyCost = &bill.TotalCost
			}
		}
		result = append(result, item)
	}
	return result, lastRun, nil
}

func RegionName(region string) string {
	names := map[string]string{
		"cn-hongkong": "中国香港", "ap-southeast-1": "新加坡", "us-west-1": "美国（硅谷）", "us-east-1": "美国（弗吉尼亚）",
		"cn-hangzhou": "华东 1（杭州）", "cn-shanghai": "华东 2（上海）", "cn-qingdao": "华北 1（青岛）", "cn-beijing": "华北 2（北京）",
		"cn-zhangjiakou": "华北 3（张家口）", "cn-huhehaote": "华北 5（呼和浩特）", "cn-wulanchabu": "华北 6（乌兰察布）",
		"cn-shenzhen": "华南 1（深圳）", "cn-heyuan": "华南 2（河源）", "cn-guangzhou": "华南 3（广州）", "cn-chengdu": "西南 1（成都）", "ap-northeast-1": "日本（东京）", "ap-northeast-2": "韩国（首尔）",
	}
	if name := names[region]; name != "" {
		return name
	}
	return region
}

// regionName resolves a region to its display name, falling back to extra
// regions loaded from regions.json when the built-in table has no entry.
func (e *Engine) regionName(region string) string {
	if name := RegionName(region); name != region {
		return name
	}
	if name := e.extraRegions[region]; name != "" {
		return name
	}
	return region
}

func ParseControlPayload(action, source string) string {
	payload, _ := json.Marshal(map[string]string{"action": strings.ToLower(action), "source": source})
	return string(payload)
}

func ParseNotifyPayload(channel string) string {
	payload, _ := json.Marshal(map[string]string{"channel": channel})
	return string(payload)
}

func JobUniqueKey(jobType string, accountID int64, suffix string) string {
	return jobType + ":" + strconv.FormatInt(accountID, 10) + ":" + suffix
}
