// Package state defines the backend used to persist environment and slot state.
package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/kube"
)

// Store manages environment and slot records using a chosen backend.
type Store struct {
	client    *kube.Client
	logger    *slog.Logger
	namespace string
	prefix    string
}

// EnvRecord represents a single allocated environment slot.
type EnvRecord struct {
	// Slot is the allocated slot number.
	Slot int
	// Env is the environment name (e.g. ai, dev).
	Env string
	// Namespace is the Kubernetes namespace for the slot.
	Namespace string
	// Issue is the associated GitHub issue number, if any.
	Issue int
	// PR is the associated GitHub pull request number, if any.
	PR int
	// CreatedAt is the timestamp when the slot was allocated.
	CreatedAt time.Time
	// ConfigName is the backing ConfigMap name used for state.
	ConfigName string
}

// NoFreeSlotError indicates that there are no free slots available.
type NoFreeSlotError struct {
	// Max is the maximum slot number that was searched.
	Max int
}

func (e *NoFreeSlotError) Error() string {
	if e == nil {
		return "no free slot"
	}
	if e.Max > 0 {
		return fmt.Sprintf("no free slot found in range 1..%d", e.Max)
	}
	return "no free slot found up to allocation limit"
}

// IsNoFreeSlotError reports whether err indicates that no free slot is available.
func IsNoFreeSlotError(err error) bool {
	var target *NoFreeSlotError
	return errors.As(err, &target)
}

// NewStore constructs a new Store instance for the given stack state configuration.
// Currently only a ConfigMap-based backend is supported.
func NewStore(stackCfg *config.StackConfig, client *kube.Client, logger *slog.Logger) (*Store, error) {
	if stackCfg == nil {
		return nil, fmt.Errorf("stack config is nil")
	}

	stateCfg := stackCfg.State
	if stateCfg.Backend != "" && stateCfg.Backend != "configmap" {
		return nil, fmt.Errorf("unsupported state backend %q", stateCfg.Backend)
	}
	if strings.TrimSpace(stateCfg.ConfigMapNamespace) == "" {
		return nil, fmt.Errorf("state.configmapNamespace must be set for configmap backend")
	}

	prefix := stateCfg.ConfigMapPrefix
	if strings.TrimSpace(prefix) == "" {
		prefix = "codex-env-"
	}

	if client == nil {
		return nil, fmt.Errorf("state store requires a Kubernetes client")
	}

	if logger == nil {
		logger = slog.New(slog.NewTextHandler(nil, nil))
	}

	return &Store{
		client:    client,
		logger:    logger,
		namespace: stateCfg.ConfigMapNamespace,
		prefix:    prefix,
	}, nil
}

// List returns all stored environment records.
func (s *Store) List(ctx context.Context) ([]EnvRecord, error) {
	if err := s.ensureNamespace(ctx); err != nil {
		return nil, err
	}
	args := []string{"-n", s.namespace, "get", "configmap", "-o", "json"}
	out, err := s.client.RunAndCapture(ctx, nil, args...)
	if err != nil {
		return nil, fmt.Errorf("list configmaps for state: %w", err)
	}

	var raw cmList
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("decode configmap list: %w", err)
	}

	var res []EnvRecord
	for _, item := range raw.Items {
		if item.Metadata.Name == "" || !strings.HasPrefix(item.Metadata.Name, s.prefix) {
			continue
		}
		rec := EnvRecord{
			ConfigName: item.Metadata.Name,
			Env:        strings.TrimSpace(item.Data["env"]),
			Namespace:  strings.TrimSpace(item.Data["namespace"]),
		}
		if slotStr := strings.TrimSpace(item.Data["slot"]); slotStr != "" {
			rec.Slot, _ = strconv.Atoi(slotStr)
		}
		if issueStr := strings.TrimSpace(item.Data["issue"]); issueStr != "" {
			rec.Issue, _ = strconv.Atoi(issueStr)
		}
		if prStr := strings.TrimSpace(item.Data["pr"]); prStr != "" {
			rec.PR, _ = strconv.Atoi(prStr)
		}
		if ts := strings.TrimSpace(item.Data["createdAt"]); ts != "" {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				rec.CreatedAt = t
			}
		}
		res = append(res, rec)
	}
	return res, nil
}

// UpdateAttributes updates issue/pr fields for a stored slot.
func (s *Store) UpdateAttributes(ctx context.Context, slot int, issue int, pr int) error {
	if err := s.ensureNamespace(ctx); err != nil {
		return err
	}
	name := fmt.Sprintf("%s%d", s.prefix, slot)
	patchData := map[string]string{}
	if issue > 0 {
		patchData["issue"] = strconv.Itoa(issue)
	}
	if pr > 0 {
		patchData["pr"] = strconv.Itoa(pr)
	}
	if len(patchData) == 0 {
		return nil
	}
	patch := map[string]any{"data": patchData}
	patchBytes, _ := json.Marshal(patch)

	args := []string{"-n", s.namespace, "patch", "configmap", name, "--type", "merge", "-p", string(patchBytes)}
	if _, err := s.client.RunAndCapture(ctx, nil, args...); err != nil {
		return fmt.Errorf("patch configmap %s: %w", name, err)
	}
	return nil
}

// UpdateNamespace updates the namespace field for a stored slot.
func (s *Store) UpdateNamespace(ctx context.Context, slot int, namespace string) error {
	if err := s.ensureNamespace(ctx); err != nil {
		return err
	}
	if slot <= 0 {
		return nil
	}
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		return nil
	}

	name := fmt.Sprintf("%s%d", s.prefix, slot)
	patch := map[string]any{"data": map[string]string{"namespace": ns}}
	patchBytes, _ := json.Marshal(patch)
	args := []string{"-n", s.namespace, "patch", "configmap", name, "--type", "merge", "-p", string(patchBytes)}
	if _, err := s.client.RunAndCapture(ctx, nil, args...); err != nil {
		return fmt.Errorf("patch configmap %s: %w", name, err)
	}
	return nil
}

// AllocateSlot reserves a new slot and returns its metadata.
// max == 0 means unlimited slots; prefer >0 is attempted first.
func (s *Store) AllocateSlot(
	ctx context.Context,
	stackCfg *config.StackConfig,
	baseCtx config.TemplateContext,
	envName string,
	max int,
	prefer int,
	issue int,
	pr int,
) (EnvRecord, error) {
	var zero EnvRecord

	if max < 0 {
		max = 0
	}

	if err := s.ensureNamespace(ctx); err != nil {
		return zero, err
	}

	owner := strings.TrimSpace(baseCtx.EnvMap["GITHUB_RUN_ID"])
	if owner == "" {
		owner = "manual"
	}

	order := buildSlotOrder(max, prefer)
	now := time.Now().UTC()

	for _, slot := range order {
		ctxSlot := baseCtx
		ctxSlot.Slot = slot
		ctxSlot.Namespace = ""

		ns, err := config.ResolveNamespace(stackCfg, ctxSlot, envName)
		if err != nil {
			return zero, err
		}

		s.logger.Info("attempting to allocate slot", "slot", slot, "env", envName, "namespace", ns)

		name := fmt.Sprintf("%s%d", s.prefix, slot)

		args := []string{
			"-n", s.namespace,
			"create", "configmap", name,
			"--from-literal=slot=" + strconv.Itoa(slot),
			"--from-literal=env=" + envName,
			"--from-literal=namespace=" + ns,
			"--from-literal=owner=" + owner,
			"--from-literal=issue=" + strconv.Itoa(issue),
			"--from-literal=pr=" + strconv.Itoa(pr),
			"--from-literal=createdAt=" + now.Format(time.RFC3339),
		}

		_, err = s.client.RunAndCapture(ctx, nil, args...)
		if err == nil {
			return EnvRecord{
				Slot:       slot,
				Env:        envName,
				Namespace:  ns,
				Issue:      issue,
				PR:         pr,
				CreatedAt:  now,
				ConfigName: name,
			}, nil
		}

		// Any error is treated as "slot not available"; proceed to the next candidate.
		s.logger.Debug("slot allocation attempt failed", "slot", slot, "error", err)
	}

	if max > 0 {
		return zero, &NoFreeSlotError{Max: max}
	}
	return zero, &NoFreeSlotError{Max: 0}
}

// GarbageCollect removes stale environment records based on TTL and returns the list of removed slots.
// When envName is non-empty, only records for that environment are considered.
func (s *Store) GarbageCollect(ctx context.Context, envName string, ttl time.Duration) ([]EnvRecord, error) {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	if err := s.ensureNamespace(ctx); err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	list, err := s.listConfigMaps(ctx)
	if err != nil {
		return nil, err
	}

	var removed []EnvRecord
	for _, item := range list.Items {
		if !strings.HasPrefix(item.Metadata.Name, s.prefix) {
			continue
		}

		slot, err := strconv.Atoi(item.Data["slot"])
		if err != nil || slot <= 0 {
			continue
		}
		envVal := item.Data["env"]
		if envName != "" && envVal != envName {
			continue
		}

		ns := item.Data["namespace"]
		createdStr := item.Data["createdAt"]
		createdAt, err := time.Parse(time.RFC3339, createdStr)
		if err != nil {
			createdAt = now
		}
		if now.Sub(createdAt) < ttl {
			continue
		}

		issueVal := 0
		if v := item.Data["issue"]; v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				issueVal = n
			}
		}
		prVal := 0
		if v := item.Data["pr"]; v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				prVal = n
			}
		}

		record := EnvRecord{
			Slot:       slot,
			Env:        envVal,
			Namespace:  ns,
			Issue:      issueVal,
			PR:         prVal,
			CreatedAt:  createdAt,
			ConfigName: item.Metadata.Name,
		}

		s.logger.Info("garbage-collecting slot", "slot", slot, "env", envVal, "namespace", ns, "configmap", item.Metadata.Name)

		delArgs := []string{"delete", "configmap", item.Metadata.Name, "-n", s.namespace, "--ignore-not-found"}
		if err := s.client.RunRaw(ctx, nil, delArgs...); err != nil {
			s.logger.Error("failed to delete state configmap during gc", "name", item.Metadata.Name, "error", err)
			continue
		}

		removed = append(removed, record)
	}

	return removed, nil
}

// ensureNamespace verifies the state namespace exists, creating it when missing.
func (s *Store) ensureNamespace(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("state store is not initialized")
	}
	if strings.TrimSpace(s.namespace) == "" {
		return fmt.Errorf("state namespace is empty")
	}
	if _, err := s.client.RunAndCapture(ctx, nil, "get", "ns", s.namespace); err == nil {
		return nil
	}
	if _, err := s.client.RunAndCapture(ctx, nil, "create", "ns", s.namespace); err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "alreadyexists") || strings.Contains(msg, "already exists") {
			return nil
		}
		return fmt.Errorf("ensure state namespace %q: %w", s.namespace, err)
	}
	return nil
}

// buildSlotOrder builds the allocation search order given limits and preference.
func buildSlotOrder(max int, prefer int) []int {
	const maxSlotsCap = 10000

	var order []int
	seen := make(map[int]struct{})

	add := func(n int) {
		if n <= 0 {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		order = append(order, n)
	}

	if prefer > 0 {
		add(prefer)
	}

	if max > 0 {
		for i := 1; i <= max; i++ {
			add(i)
		}
	} else {
		for i := 1; i <= maxSlotsCap; i++ {
			add(i)
		}
	}

	return order
}

type cmList struct {
	Items []cmItem `json:"items"`
}

type cmItem struct {
	Metadata struct {
		Name              string `json:"name"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Data map[string]string `json:"data"`
}

// listConfigMaps returns the raw ConfigMap list used for state storage.
func (s *Store) listConfigMaps(ctx context.Context) (*cmList, error) {
	args := []string{"-n", s.namespace, "get", "configmap", "-o", "json"}
	out, err := s.client.RunAndCapture(ctx, nil, args...)
	if err != nil {
		return nil, fmt.Errorf("list state configmaps: %w", err)
	}

	var list cmList
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("parse state configmaps: %w", err)
	}
	return &list, nil
}
