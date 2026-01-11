package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

type Account struct {
	ID           int     `json:"id"`
	Email        string  `json:"email"`
	APIKey       string  `json:"apiKey"`
	UID          string  `json:"uid"`
	RefreshToken string  `json:"refreshToken"`
	Quota        int     `json:"quota"`
	Used         int     `json:"used"`
	Status       string  `json:"status"`
	Type         string  `json:"type"`
	NextRefresh  string  `json:"nextRefreshTime"`
	LastUsed     float64 `json:"lastUsed"`
	ErrorCount   int     `json:"errorCount"`
	ExperimentID string  `json:"experimentId"`
	BindingUsed  bool    `json:"bindingUsed"`
}

const (
	defaultAccountQuota  = 150
	defaultAccountStatus = "available"
	defaultAccountType   = "FREE"
)

type accountWire struct {
	ID              int      `json:"id"`
	Email           string   `json:"email"`
	APIKey          string   `json:"apiKey"`
	APIKeyAlt       string   `json:"api_key"`
	UID             string   `json:"uid"`
	RefreshToken    string   `json:"refreshToken"`
	RefreshTokenAlt string   `json:"refresh_token"`
	Quota           *int     `json:"quota"`
	Used            *int     `json:"used"`
	Status          *string  `json:"status"`
	Type            *string  `json:"type"`
	NextRefresh     *string  `json:"nextRefreshTime"`
	NextRefreshAlt  *string  `json:"next_refresh_time"`
	LastUsed        *float64 `json:"lastUsed"`
	LastUsedAlt     *float64 `json:"last_used"`
	ErrorCount      *int     `json:"errorCount"`
	ErrorCountAlt   *int     `json:"error_count"`
	ExperimentID    string   `json:"experimentId"`
	ExperimentIDAlt string   `json:"experiment_id"`
	BindingUsed     *bool    `json:"bindingUsed"`
	BindingUsedAlt  *bool    `json:"binding_used"`
}

func (a *Account) UnmarshalJSON(data []byte) error {
	var w accountWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}

	a.ID = w.ID
	a.Email = w.Email
	a.APIKey = w.APIKey
	if a.APIKey == "" {
		a.APIKey = w.APIKeyAlt
	}
	a.UID = w.UID
	a.RefreshToken = w.RefreshToken
	if a.RefreshToken == "" {
		a.RefreshToken = w.RefreshTokenAlt
	}
	if w.Quota != nil {
		a.Quota = *w.Quota
	} else {
		a.Quota = defaultAccountQuota
	}
	if w.Used != nil {
		a.Used = *w.Used
	} else {
		a.Used = 0
	}
	if w.Status != nil {
		a.Status = *w.Status
	} else {
		a.Status = defaultAccountStatus
	}
	if w.Type != nil {
		a.Type = *w.Type
	} else {
		a.Type = defaultAccountType
	}
	if w.NextRefresh != nil {
		a.NextRefresh = *w.NextRefresh
	} else if w.NextRefreshAlt != nil {
		a.NextRefresh = *w.NextRefreshAlt
	}
	if w.LastUsed != nil {
		a.LastUsed = *w.LastUsed
	} else if w.LastUsedAlt != nil {
		a.LastUsed = *w.LastUsedAlt
	}
	if w.ErrorCount != nil {
		a.ErrorCount = *w.ErrorCount
	} else if w.ErrorCountAlt != nil {
		a.ErrorCount = *w.ErrorCountAlt
	}
	if w.ExperimentID != "" {
		a.ExperimentID = w.ExperimentID
	} else {
		a.ExperimentID = w.ExperimentIDAlt
	}
	if w.BindingUsed != nil {
		a.BindingUsed = *w.BindingUsed
	} else if w.BindingUsedAlt != nil {
		a.BindingUsed = *w.BindingUsedAlt
	}
	return nil
}

type AccountsSnapshot struct {
	LocalAccounts   []Account `json:"localAccounts"`
	TotalVirtualUsed int      `json:"totalVirtualUsed"`
	CurrentAccount  *Account  `json:"currentAccount"`
	LastUpdated     string    `json:"lastUpdated"`
	Source          string    `json:"source"`
}

func loadAccountsSnapshot(path string) (AccountsSnapshot, error) {
	if path == "" {
		return AccountsSnapshot{LocalAccounts: []Account{}}, errors.New("accounts path not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AccountsSnapshot{LocalAccounts: []Account{}}, nil
		}
		return AccountsSnapshot{LocalAccounts: []Account{}}, err
	}
	if len(raw) == 0 {
		return AccountsSnapshot{LocalAccounts: []Account{}}, nil
	}
	var snapshot AccountsSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return AccountsSnapshot{LocalAccounts: []Account{}}, err
	}
	if snapshot.LocalAccounts == nil {
		snapshot.LocalAccounts = []Account{}
	}
	return snapshot, nil
}

func saveAccountsSnapshot(path string, snapshot AccountsSnapshot) error {
	if path == "" {
		return errors.New("accounts path not set")
	}
	snapshot.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	sanitized := sanitizeSnapshotForDisk(snapshot)
	raw, err := json.MarshalIndent(sanitized, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func sanitizeAccountSecrets(acc Account) Account {
	acc.APIKey = ""
	acc.RefreshToken = ""
	acc.UID = ""
	return acc
}

func sanitizeSnapshotForDisk(snapshot AccountsSnapshot) AccountsSnapshot {
	sanitized := snapshot
	sanitized.LocalAccounts = make([]Account, len(snapshot.LocalAccounts))
	for i, acc := range snapshot.LocalAccounts {
		sanitized.LocalAccounts[i] = sanitizeAccountSecrets(acc)
	}
	if snapshot.CurrentAccount != nil {
		cur := sanitizeAccountSecrets(*snapshot.CurrentAccount)
		sanitized.CurrentAccount = &cur
	}
	return sanitized
}

func mergeSnapshotWithSecrets(base AccountsSnapshot, secrets AccountsSnapshot) AccountsSnapshot {
	if len(base.LocalAccounts) == 0 && len(secrets.LocalAccounts) > 0 {
		return secrets
	}
	if len(secrets.LocalAccounts) == 0 {
		return base
	}

	byEmail := map[string]Account{}
	byID := map[int]Account{}
	for _, acc := range secrets.LocalAccounts {
		if acc.Email != "" {
			byEmail[strings.ToLower(acc.Email)] = acc
		}
		if acc.ID > 0 {
			byID[acc.ID] = acc
		}
	}

	for i := range base.LocalAccounts {
		acc := base.LocalAccounts[i]
		secret, ok := byID[acc.ID]
		if !ok && acc.Email != "" {
			secret, ok = byEmail[strings.ToLower(acc.Email)]
		}
		if ok {
			acc = fillAccountSecrets(acc, secret)
			base.LocalAccounts[i] = acc
		}
	}

	if base.CurrentAccount != nil {
		cur := *base.CurrentAccount
		secret, ok := byID[cur.ID]
		if !ok && cur.Email != "" {
			secret, ok = byEmail[strings.ToLower(cur.Email)]
		}
		if ok {
			cur = fillAccountSecrets(cur, secret)
			base.CurrentAccount = &cur
		}
	}
	return base
}

func fillAccountSecrets(target Account, source Account) Account {
	if target.APIKey == "" {
		target.APIKey = source.APIKey
	}
	if target.RefreshToken == "" {
		target.RefreshToken = source.RefreshToken
	}
	if target.UID == "" {
		target.UID = source.UID
	}
	if target.ExperimentID == "" {
		target.ExperimentID = source.ExperimentID
	}
	return target
}

func mergeRemoteAccounts(remote []RemoteAccount, local AccountsSnapshot, preferLocal bool) AccountsSnapshot {
	byEmail := map[string]Account{}
	byAPIKey := map[string]Account{}
	for _, acc := range local.LocalAccounts {
		if acc.Email != "" {
			byEmail[strings.ToLower(acc.Email)] = acc
		}
		if acc.APIKey != "" {
			byAPIKey[acc.APIKey] = acc
		}
	}

	merged := make([]Account, 0, len(remote))
	for _, remoteAcc := range remote {
		acc := accountFromRemote(remoteAcc)
		var matched *Account
		if acc.Email != "" {
			if localAcc, ok := byEmail[strings.ToLower(acc.Email)]; ok {
				matched = &localAcc
			}
		}
		if matched == nil && acc.APIKey != "" {
			if localAcc, ok := byAPIKey[acc.APIKey]; ok {
				matched = &localAcc
			}
		}
		if matched != nil {
			if preferLocal {
				if matched.Used > 0 {
					acc.Used = matched.Used
				}
				if matched.Quota > 0 {
					acc.Quota = matched.Quota
				}
				if matched.Status != "" {
					acc.Status = matched.Status
				}
				if matched.Type != "" {
					acc.Type = matched.Type
				}
				if matched.NextRefresh != "" {
					acc.NextRefresh = matched.NextRefresh
				}
				if matched.ErrorCount > 0 {
					acc.ErrorCount = matched.ErrorCount
				}
			}
			if matched.LastUsed > 0 {
				acc.LastUsed = matched.LastUsed
			}
			if matched.ExperimentID != "" {
				acc.ExperimentID = matched.ExperimentID
			}
			if matched.BindingUsed {
				acc.BindingUsed = matched.BindingUsed
			}
		}
		merged = append(merged, acc)
	}

	var current *Account
	if local.CurrentAccount != nil {
		targetEmail := strings.ToLower(local.CurrentAccount.Email)
		for i := range merged {
			if merged[i].Email != "" && strings.ToLower(merged[i].Email) == targetEmail {
				current = &merged[i]
				break
			}
			if current == nil && local.CurrentAccount.APIKey != "" && merged[i].APIKey == local.CurrentAccount.APIKey {
				current = &merged[i]
			}
		}
	}

	return AccountsSnapshot{
		LocalAccounts:   merged,
		TotalVirtualUsed: local.TotalVirtualUsed,
		CurrentAccount:  current,
		Source:          "remote",
		LastUpdated:     time.Now().UTC().Format(time.RFC3339),
	}
}

func updateAccountSnapshot(snapshot *AccountsSnapshot, updated Account) {
	if snapshot == nil {
		return
	}
	for i, acc := range snapshot.LocalAccounts {
		if acc.Email != "" && updated.Email != "" && strings.EqualFold(acc.Email, updated.Email) {
			snapshot.LocalAccounts[i] = updated
			return
		}
		if acc.APIKey != "" && updated.APIKey != "" && acc.APIKey == updated.APIKey {
			snapshot.LocalAccounts[i] = updated
			return
		}
	}
	snapshot.LocalAccounts = append(snapshot.LocalAccounts, updated)
}

func findAccountByEmail(snapshot AccountsSnapshot, email string) *Account {
	if email == "" {
		return nil
	}
	for _, acc := range snapshot.LocalAccounts {
		if strings.EqualFold(acc.Email, email) {
			cpy := acc
			return &cpy
		}
	}
	return nil
}

func accountFromRemote(remote RemoteAccount) Account {
	return Account{
		ID:           remote.ID,
		Email:        remote.Email,
		APIKey:       remote.APIKey,
		UID:          remote.UID,
		RefreshToken: remote.RefreshToken,
		Quota:        remote.Quota,
		Used:         remote.Used,
		Status:       remote.Status,
		Type:         remote.Type,
		NextRefresh:  remote.NextRefresh,
		LastUsed:     remote.LastUsed,
		ErrorCount:   remote.ErrorCount,
		ExperimentID: remote.ExperimentID,
		BindingUsed:  remote.BindingUsed,
	}
}

func normalizeAccountStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		status = "normal"
	}
	return status
}

func accountRemainingForSwitch(acc Account) int {
	if acc.Quota <= 0 {
		return 0
	}
	remaining := acc.Quota - acc.Used
	if remaining < 0 {
		return 0
	}
	return remaining
}

func accountSelectable(acc Account) bool {
	status := normalizeAccountStatus(acc.Status)
	if status != "normal" && status != "available" {
		return false
	}
	return accountRemainingForSwitch(acc) > 0
}

func accountSwitchable(acc Account) bool {
	if !accountSelectable(acc) {
		return false
	}
	return strings.TrimSpace(acc.APIKey) != ""
}

func selectNextAvailableAccount(snapshot AccountsSnapshot) *Account {
	if len(snapshot.LocalAccounts) == 0 {
		return nil
	}
	currentEmail := ""
	if snapshot.CurrentAccount != nil {
		currentEmail = snapshot.CurrentAccount.Email
	}

	start := 0
	if currentEmail != "" {
		for i, acc := range snapshot.LocalAccounts {
			if strings.EqualFold(acc.Email, currentEmail) {
				start = i + 1
				break
			}
		}
	}

	for i := 0; i < len(snapshot.LocalAccounts); i++ {
		idx := (start + i) % len(snapshot.LocalAccounts)
		acc := snapshot.LocalAccounts[idx]
		if currentEmail != "" && strings.EqualFold(acc.Email, currentEmail) {
			continue
		}
		if accountSelectable(acc) {
			chosen := acc
			return &chosen
		}
	}
	return nil
}
