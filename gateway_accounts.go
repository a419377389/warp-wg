package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type GatewayAccountManager struct {
	mu     sync.Mutex
	path   string
	log    *Logger
	onSwitch func(prev string, current *Account, reason string)

	accounts        []Account
	current         *Account
	totalVirtualUsed int
}

func NewGatewayAccountManager(path string, log *Logger, onSwitch func(prev string, current *Account, reason string)) *GatewayAccountManager {
	mgr := &GatewayAccountManager{
		path:    path,
		log:     log,
		onSwitch: onSwitch,
	}
	mgr.LoadAccounts(false)
	return mgr
}

func (m *GatewayAccountManager) LoadAccounts(forceReload bool) {
	if m.path == "" {
		return
	}
	snapshot, err := loadAccountsSnapshot(m.path)
	if err != nil && m.log != nil {
		m.log.Error("accounts load failed: " + err.Error())
	}
	m.loadSnapshot(snapshot, forceReload)
}

func (m *GatewayAccountManager) LoadSnapshot(snapshot AccountsSnapshot, forceReload bool) {
	m.loadSnapshot(snapshot, forceReload)
}

func (m *GatewayAccountManager) loadSnapshot(snapshot AccountsSnapshot, forceReload bool) {
	if snapshot.LocalAccounts == nil {
		snapshot.LocalAccounts = []Account{}
	}
	snapshot.LocalAccounts = mergeAccountsPreserveSecrets(snapshot.LocalAccounts, m.accounts)
	if snapshot.CurrentAccount != nil {
		cur := *snapshot.CurrentAccount
		cur = fillAccountSecretsFromList(cur, m.accounts)
		snapshot.CurrentAccount = &cur
	}

	m.mu.Lock()
	oldEmail := ""
	if m.current != nil {
		oldEmail = m.current.Email
	}
	m.accounts = snapshot.LocalAccounts
	m.totalVirtualUsed = snapshot.TotalVirtualUsed
	m.current = nil
	var notifyPrev string
	var notifyCurrent *Account

	if snapshot.CurrentAccount != nil && snapshot.CurrentAccount.Email != "" {
		target := snapshot.CurrentAccount.Email
		if idx := m.findAccountIndexByEmailLocked(target); idx >= 0 {
			m.current = &m.accounts[idx]
			m.current.LastUsed = float64(time.Now().Unix())
			if !strings.EqualFold(oldEmail, m.current.Email) {
				notifyPrev = oldEmail
				notifyCurrent = m.copyCurrentLocked()
			}
		} else {
			notifyPrev, notifyCurrent = m.selectAvailableLocked()
		}
	} else if forceReload && oldEmail != "" {
		if idx := m.findAccountIndexByEmailLocked(oldEmail); idx >= 0 {
			m.current = &m.accounts[idx]
		} else {
			notifyPrev, notifyCurrent = m.selectAvailableLocked()
		}
	} else {
		notifyPrev, notifyCurrent = m.selectAvailableLocked()
	}
	m.mu.Unlock()

	if notifyCurrent != nil {
		m.notifySwitch(notifyPrev, notifyCurrent, "switch")
	}
}

func (m *GatewayAccountManager) Current() *Account {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.copyCurrentLocked()
}

func (m *GatewayAccountManager) CurrentEmail() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		return ""
	}
	return m.current.Email
}

func (m *GatewayAccountManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.accounts)
}

func (m *GatewayAccountManager) SelectAvailableAccount() *Account {
	m.mu.Lock()
	prev, current := m.selectAvailableLocked()
	m.mu.Unlock()

	if current != nil {
		m.notifySwitch(prev, current, "switch")
	}
	return current
}

func (m *GatewayAccountManager) MarkCurrentLimitedSoft() {
	m.mu.Lock()
	if m.current != nil {
		m.current.Status = "limited"
		m.current.Used = m.current.Quota
		_ = m.saveAccountsLocked()
	}
	m.mu.Unlock()
}

func (m *GatewayAccountManager) MarkCurrentBanned() *Account {
	m.mu.Lock()
	var currentEmail string
	if m.current != nil {
		m.current.Status = "banned"
		m.current.Used = 0
		m.current.Quota = 0
		currentEmail = m.current.Email
		_ = m.saveAccountsLocked()
	}
	m.mu.Unlock()

	if currentEmail == "" {
		return nil
	}

	if m.log != nil {
		m.log.Error("account banned: " + currentEmail)
	}
	_ = resetMachineID()
	next := m.SelectAvailableAccount()
	if next != nil {
		_ = updateWarpCredentialsWithLog(*next, m.log, "banned")
	}
	return next
}

func (m *GatewayAccountManager) MarkCurrentLimited() *Account {
	m.mu.Lock()
	var currentEmail string
	if m.current != nil {
		m.current.Status = "limited"
		m.current.Used = m.current.Quota
		currentEmail = m.current.Email
		_ = m.saveAccountsLocked()
	}
	m.mu.Unlock()

	if currentEmail == "" {
		return nil
	}

	if m.log != nil {
		m.log.Warn("account limited: " + currentEmail)
	}
	_ = resetMachineID()
	next := m.SelectAvailableAccount()
	if next != nil {
		_ = updateWarpCredentialsWithLog(*next, m.log, "limited")
	}
	return next
}

func (m *GatewayAccountManager) MarkCurrentError() {
	m.mu.Lock()
	if m.current != nil {
		m.current.Status = "error"
		m.current.ErrorCount += 1
		_ = m.saveAccountsLocked()
	}
	m.mu.Unlock()
	m.SelectAvailableAccount()
}

func (m *GatewayAccountManager) SyncUsage(realUsed, realQuota int) (int, int, bool) {
	m.mu.Lock()
	needSwitch := false
	if m.current != nil {
		m.current.Used = realUsed
		m.current.Quota = realQuota
		if realQuota > 0 && realUsed >= realQuota {
			m.current.Status = "limited"
			needSwitch = true
		} else if m.current.Status != "banned" {
			m.current.Status = "normal"
		}
		_ = m.saveAccountsLocked()
	}

	totalQuota := 0
	totalUsed := 0
	for i := range m.accounts {
		acc := &m.accounts[i]
		if accountIsBanned(acc) {
			continue
		}
		totalQuota += acc.Quota
		totalUsed += acc.Used
	}
	m.totalVirtualUsed = totalUsed
	m.mu.Unlock()

	if needSwitch {
		_ = resetMachineID()
		next := m.SelectAvailableAccount()
		if next != nil {
			_ = updateWarpCredentialsWithLog(*next, m.log, "quota")
		}
	}

	return totalUsed, totalQuota, needSwitch
}

func (m *GatewayAccountManager) saveAccountsLocked() error {
	if m.path == "" {
		return errors.New("accounts path not set")
	}
	snapshot := AccountsSnapshot{
		LocalAccounts:   make([]Account, len(m.accounts)),
		TotalVirtualUsed: m.totalVirtualUsed,
		Source:          "local",
	}
	copy(snapshot.LocalAccounts, m.accounts)
	if m.current != nil {
		cur := *m.current
		snapshot.CurrentAccount = &cur
	}
	return saveAccountsSnapshot(m.path, snapshot)
}

func (m *GatewayAccountManager) findAccountIndexByEmailLocked(email string) int {
	for i := range m.accounts {
		if strings.EqualFold(m.accounts[i].Email, email) {
			return i
		}
	}
	return -1
}

func mergeAccountsPreserveSecrets(base []Account, secrets []Account) []Account {
	if len(secrets) == 0 {
		return base
	}
	byEmail := map[string]Account{}
	byID := map[int]Account{}
	for _, acc := range secrets {
		if acc.Email != "" {
			byEmail[strings.ToLower(acc.Email)] = acc
		}
		if acc.ID > 0 {
			byID[acc.ID] = acc
		}
	}
	for i := range base {
		acc := base[i]
		secret, ok := byID[acc.ID]
		if !ok && acc.Email != "" {
			secret, ok = byEmail[strings.ToLower(acc.Email)]
		}
		if ok {
			base[i] = fillAccountSecrets(acc, secret)
		}
	}
	return base
}

func fillAccountSecretsFromList(target Account, secrets []Account) Account {
	if len(secrets) == 0 {
		return target
	}
	for _, acc := range secrets {
		if acc.ID > 0 && target.ID > 0 && acc.ID == target.ID {
			return fillAccountSecrets(target, acc)
		}
		if acc.Email != "" && target.Email != "" && strings.EqualFold(acc.Email, target.Email) {
			return fillAccountSecrets(target, acc)
		}
	}
	return target
}

func (m *GatewayAccountManager) selectAvailableLocked() (string, *Account) {
	prevEmail := ""
	if m.current != nil {
		prevEmail = m.current.Email
	}

	for i := range m.accounts {
		acc := &m.accounts[i]
		if accountIsBanned(acc) {
			continue
		}
		if accountIsAvailable(acc) {
			m.current = acc
			m.current.LastUsed = float64(time.Now().Unix())
			_ = m.saveAccountsLocked()
			return prevEmail, m.copyCurrentLocked()
		}
	}

	if m.log != nil {
		m.log.Warn(fmt.Sprintf("no available accounts: total=%d", len(m.accounts)))
	}
	return prevEmail, nil
}

func (m *GatewayAccountManager) copyCurrentLocked() *Account {
	if m.current == nil {
		return nil
	}
	cur := *m.current
	return &cur
}

func (m *GatewayAccountManager) notifySwitch(prev string, current *Account, reason string) {
	if current == nil {
		return
	}
	if m.onSwitch != nil && !strings.EqualFold(prev, current.Email) {
		m.onSwitch(prev, current, reason)
	}
}

func accountIsBanned(acc *Account) bool {
	if acc == nil {
		return true
	}
	return strings.EqualFold(acc.Status, "banned")
}

func accountRemaining(acc *Account) int {
	if acc == nil {
		return 0
	}
	remaining := acc.Quota - acc.Used
	if remaining < 0 {
		return 0
	}
	return remaining
}

func accountIsAvailable(acc *Account) bool {
	if acc == nil {
		return false
	}
	if accountIsBanned(acc) {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(acc.Status))
	if status == "" {
		status = "normal"
	}
	if status != "normal" && status != "available" {
		return false
	}
	return accountRemaining(acc) > 0
}

// ReplaceAccounts 替换账号列表（用于无限额度激活码换号后更新）
func (m *GatewayAccountManager) ReplaceAccounts(accounts []Account) {
	m.mu.Lock()
	oldEmail := ""
	if m.current != nil {
		oldEmail = m.current.Email
	}
	m.accounts = accounts
	m.current = nil
	var notifyPrev string
	var notifyCurrent *Account
	if len(accounts) > 0 {
		m.current = &m.accounts[0]
		m.current.LastUsed = float64(time.Now().Unix())
		if !strings.EqualFold(oldEmail, m.current.Email) {
			notifyPrev = oldEmail
			notifyCurrent = m.copyCurrentLocked()
		}
	}
	_ = m.saveAccountsLocked()
	m.mu.Unlock()

	if notifyCurrent != nil {
		m.notifySwitch(notifyPrev, notifyCurrent, "rotate")
	}
}
