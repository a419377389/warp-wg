package main

import (
	"context"
	"errors"
	"strings"
)

func (a *App) setMemorySnapshot(snapshot AccountsSnapshot) {
	if a == nil {
		return
	}
	copySnap := copyAccountsSnapshot(snapshot)
	a.accountsMu.Lock()
	a.memorySnapshot = &copySnap
	a.accountsMu.Unlock()
}

func (a *App) getMemorySnapshot() (AccountsSnapshot, bool) {
	if a == nil {
		return AccountsSnapshot{}, false
	}
	a.accountsMu.Lock()
	defer a.accountsMu.Unlock()
	if a.memorySnapshot == nil {
		return AccountsSnapshot{}, false
	}
	return copyAccountsSnapshot(*a.memorySnapshot), true
}

func (a *App) mergeSnapshotWithMemory(snapshot AccountsSnapshot) AccountsSnapshot {
	mem, ok := a.getMemorySnapshot()
	if !ok {
		return snapshot
	}
	return mergeSnapshotWithSecrets(snapshot, mem)
}

func (a *App) refreshRemoteAccounts(ctx context.Context, preferLocal bool) (AccountsSnapshot, error) {
	if a == nil {
		return AccountsSnapshot{}, errors.New("app not ready")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cfg := a.getConfig()
	if cfg.Token == "" || cfg.DeviceID == "" {
		return AccountsSnapshot{}, errors.New("activation required")
	}

	remoteAccounts, err := a.remote.Accounts(ctx, cfg.Token, cfg.DeviceID)
	if err != nil {
		return AccountsSnapshot{}, err
	}
	localSnapshot, _ := loadAccountsSnapshot(a.paths.AccountsFile)
	merged := mergeRemoteAccounts(remoteAccounts, localSnapshot, preferLocal)
	a.setMemorySnapshot(merged)
	_ = saveAccountsSnapshot(a.paths.AccountsFile, merged)
	return merged, nil
}

func (a *App) loadSnapshotWithSecrets(ctx context.Context, preferLocal bool) (AccountsSnapshot, error) {
	snapshot, err := loadAccountsSnapshot(a.paths.AccountsFile)
	if err != nil {
		snapshot = AccountsSnapshot{LocalAccounts: []Account{}}
	}
	snapshot = a.mergeSnapshotWithMemory(snapshot)
	if snapshotHasSecrets(snapshot) {
		return snapshot, nil
	}
	merged, err := a.refreshRemoteAccounts(ctx, preferLocal)
	if err != nil {
		return snapshot, err
	}
	snapshot = mergeSnapshotWithSecrets(snapshot, merged)
	return snapshot, nil
}

func copyAccountsSnapshot(snapshot AccountsSnapshot) AccountsSnapshot {
	out := snapshot
	if snapshot.LocalAccounts != nil {
		out.LocalAccounts = make([]Account, len(snapshot.LocalAccounts))
		copy(out.LocalAccounts, snapshot.LocalAccounts)
	}
	if snapshot.CurrentAccount != nil {
		cur := *snapshot.CurrentAccount
		out.CurrentAccount = &cur
	}
	return out
}

func snapshotHasSecrets(snapshot AccountsSnapshot) bool {
	if snapshot.CurrentAccount != nil {
		if strings.TrimSpace(snapshot.CurrentAccount.APIKey) != "" {
			return true
		}
		if strings.TrimSpace(snapshot.CurrentAccount.RefreshToken) != "" {
			return true
		}
	}
	for _, acc := range snapshot.LocalAccounts {
		if strings.TrimSpace(acc.APIKey) != "" {
			return true
		}
		if strings.TrimSpace(acc.RefreshToken) != "" {
			return true
		}
	}
	return false
}
