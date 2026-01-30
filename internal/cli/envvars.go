package cli

import (
	"os"
	"strconv"
	"strings"

	envparse "github.com/caarlos0/env/v10"
)

// baseEnv defines root CLI defaults sourced from CODEXCTL_* env vars.
type baseEnv struct {
	// ConfigPath is the services.yaml path from CODEXCTL_CONFIG.
	ConfigPath string `env:"CODEXCTL_CONFIG"`
	// Env is the environment name from CODEXCTL_ENV.
	Env string `env:"CODEXCTL_ENV"`
	// Namespace is the namespace override from CODEXCTL_NAMESPACE.
	Namespace string `env:"CODEXCTL_NAMESPACE"`
	// LogLevel is the logging level from CODEXCTL_LOG_LEVEL.
	LogLevel string `env:"CODEXCTL_LOG_LEVEL"`
}

// varsEnv describes inline vars and var files passed via env.
type varsEnv struct {
	// Vars is a k=v,k2=v2 list from CODEXCTL_VARS.
	Vars string `env:"CODEXCTL_VARS"`
	// VarFile is a YAML/ENV path from CODEXCTL_VAR_FILE.
	VarFile string `env:"CODEXCTL_VAR_FILE"`
}

// ciEnv captures CODEXCTL_* inputs for CI helpers.
type ciEnv struct {
	// Slot is the slot number from CODEXCTL_SLOT.
	Slot int `env:"CODEXCTL_SLOT"`
	// Issue is the issue number from CODEXCTL_ISSUE_NUMBER.
	Issue int `env:"CODEXCTL_ISSUE_NUMBER"`
	// PR is the PR number from CODEXCTL_PR_NUMBER.
	PR int `env:"CODEXCTL_PR_NUMBER"`
	// MaxSlots caps slots from CODEXCTL_DEV_SLOTS_MAX.
	MaxSlots int `env:"CODEXCTL_DEV_SLOTS_MAX"`
	// CodeRootBase is the workspace base from CODEXCTL_CODE_ROOT_BASE.
	CodeRootBase string `env:"CODEXCTL_CODE_ROOT_BASE"`
	// Source is the sync source path from CODEXCTL_SOURCE.
	Source string `env:"CODEXCTL_SOURCE"`
	// PrepareImages toggles image prep from CODEXCTL_PREPARE_IMAGES.
	PrepareImages bool `env:"CODEXCTL_PREPARE_IMAGES"`
	// Apply toggles apply from CODEXCTL_APPLY.
	Apply bool `env:"CODEXCTL_APPLY"`
	// ForceApply forces apply from CODEXCTL_FORCE_APPLY.
	ForceApply bool `env:"CODEXCTL_FORCE_APPLY"`
	// WaitTimeout is kubectl wait timeout from CODEXCTL_WAIT_TIMEOUT.
	WaitTimeout string `env:"CODEXCTL_WAIT_TIMEOUT"`
	// WaitSoftFail allows soft-fail from CODEXCTL_WAIT_SOFT_FAIL.
	WaitSoftFail bool `env:"CODEXCTL_WAIT_SOFT_FAIL"`
	// Preflight toggles preflight checks from CODEXCTL_PREFLIGHT.
	Preflight bool `env:"CODEXCTL_PREFLIGHT"`
	// Wait toggles wait from CODEXCTL_WAIT.
	Wait bool `env:"CODEXCTL_WAIT"`
	// ApplyRetries is apply retry count from CODEXCTL_APPLY_RETRIES.
	ApplyRetries int `env:"CODEXCTL_APPLY_RETRIES"`
	// WaitRetries is wait retry count from CODEXCTL_WAIT_RETRIES.
	WaitRetries int `env:"CODEXCTL_WAIT_RETRIES"`
	// ApplyBackoff is apply backoff duration from CODEXCTL_APPLY_BACKOFF.
	ApplyBackoff string `env:"CODEXCTL_APPLY_BACKOFF"`
	// WaitBackoff is wait backoff duration from CODEXCTL_WAIT_BACKOFF.
	WaitBackoff string `env:"CODEXCTL_WAIT_BACKOFF"`
	// RequestTime is request timeout from CODEXCTL_REQUEST_TIMEOUT.
	RequestTime string `env:"CODEXCTL_REQUEST_TIMEOUT"`
	// OnlyServices filters services from CODEXCTL_ONLY_SERVICES.
	OnlyServices string `env:"CODEXCTL_ONLY_SERVICES"`
	// SkipServices filters services from CODEXCTL_SKIP_SERVICES.
	SkipServices string `env:"CODEXCTL_SKIP_SERVICES"`
	// OnlyInfra filters infra from CODEXCTL_ONLY_INFRA.
	OnlyInfra string `env:"CODEXCTL_ONLY_INFRA"`
	// SkipInfra filters infra from CODEXCTL_SKIP_INFRA.
	SkipInfra string `env:"CODEXCTL_SKIP_INFRA"`
	// MirrorImages toggles mirroring from CODEXCTL_MIRROR_IMAGES.
	MirrorImages bool `env:"CODEXCTL_MIRROR_IMAGES"`
	// BuildImages toggles builds from CODEXCTL_BUILD_IMAGES.
	BuildImages bool `env:"CODEXCTL_BUILD_IMAGES"`
}

// promptEnv provides CODEXCTL_* values for prompt runs.
type promptEnv struct {
	// Slot is the slot number from CODEXCTL_SLOT.
	Slot int `env:"CODEXCTL_SLOT"`
	// Issue is the issue number from CODEXCTL_ISSUE_NUMBER.
	Issue int `env:"CODEXCTL_ISSUE_NUMBER"`
	// PR is the PR number from CODEXCTL_PR_NUMBER.
	PR int `env:"CODEXCTL_PR_NUMBER"`
	// FocusIssue is the focus issue number from CODEXCTL_FOCUS_ISSUE_NUMBER.
	FocusIssue int `env:"CODEXCTL_FOCUS_ISSUE_NUMBER"`
	// Namespace is the namespace override from CODEXCTL_NAMESPACE.
	Namespace string `env:"CODEXCTL_NAMESPACE"`
	// Kind is the prompt kind from CODEXCTL_KIND.
	Kind string `env:"CODEXCTL_KIND"`
	// Lang is the prompt language from CODEXCTL_LANG.
	Lang string `env:"CODEXCTL_LANG"`
	// GHUsername is the git author name from CODEXCTL_GH_USERNAME.
	GHUsername string `env:"CODEXCTL_GH_USERNAME"`
	// GHEmail is the git author email from CODEXCTL_GH_EMAIL.
	GHEmail string `env:"CODEXCTL_GH_EMAIL"`
	// InfraUnhealthy flags degraded infra from CODEXCTL_INFRA_UNHEALTHY.
	InfraUnhealthy bool `env:"CODEXCTL_INFRA_UNHEALTHY"`
	// Resume toggles resume mode from CODEXCTL_RESUME.
	Resume bool `env:"CODEXCTL_RESUME"`
	// Model is the model override from CODEXCTL_MODEL.
	Model string `env:"CODEXCTL_MODEL"`
	// ReasoningEffort is the reasoning effort from CODEXCTL_MODEL_REASONING_EFFORT.
	ReasoningEffort string `env:"CODEXCTL_MODEL_REASONING_EFFORT"`
	// PromptMode is the prompt mode from CODEXCTL_PROMPT_MODE.
	PromptMode string `env:"CODEXCTL_PROMPT_MODE"`
	// PromptContinuation toggles continuation from CODEXCTL_PROMPT_CONTINUATION.
	PromptContinuation string `env:"CODEXCTL_PROMPT_CONTINUATION"`
}

// planEnv captures vars for plan resolve helpers.
type planEnv struct {
	// Issue is the issue number from CODEXCTL_ISSUE_NUMBER.
	Issue int `env:"CODEXCTL_ISSUE_NUMBER"`
	// Repo is the repository slug from CODEXCTL_REPO.
	Repo string `env:"CODEXCTL_REPO"`
}

// manageEnvEnv captures env inputs for manage-env commands.
type manageEnvEnv struct {
	// Slot is the slot number from CODEXCTL_SLOT.
	Slot int `env:"CODEXCTL_SLOT"`
	// Issue is the issue number from CODEXCTL_ISSUE_NUMBER.
	Issue int `env:"CODEXCTL_ISSUE_NUMBER"`
	// PR is the PR number from CODEXCTL_PR_NUMBER.
	PR int `env:"CODEXCTL_PR_NUMBER"`
	// All toggles cleanup-all from CODEXCTL_ALL.
	All bool `env:"CODEXCTL_ALL"`
	// WithConfigMap toggles configmap cleanup from CODEXCTL_WITH_CONFIGMAP.
	WithConfigMap bool `env:"CODEXCTL_WITH_CONFIGMAP"`
	// Lang is the comment language from CODEXCTL_LANG.
	Lang string `env:"CODEXCTL_LANG"`
}

// prEnv captures inputs for PR workflows.
type prEnv struct {
	// Slot is the slot number from CODEXCTL_SLOT.
	Slot int `env:"CODEXCTL_SLOT"`
	// PR is the PR number from CODEXCTL_PR_NUMBER.
	PR int `env:"CODEXCTL_PR_NUMBER"`
	// CodeRootBase is the workspace base from CODEXCTL_CODE_ROOT_BASE.
	CodeRootBase string `env:"CODEXCTL_CODE_ROOT_BASE"`
	// Lang is the language from CODEXCTL_LANG.
	Lang string `env:"CODEXCTL_LANG"`
}

// commentPREnv holds env inputs for PR comments.
type commentPREnv struct {
	// Slot is the slot number from CODEXCTL_SLOT.
	Slot int `env:"CODEXCTL_SLOT"`
	// PR is the PR number from CODEXCTL_PR_NUMBER.
	PR int `env:"CODEXCTL_PR_NUMBER"`
	// Repo is the repository slug from CODEXCTL_REPO.
	Repo string `env:"CODEXCTL_REPO"`
	// Lang is the language from CODEXCTL_LANG.
	Lang string `env:"CODEXCTL_LANG"`
}

// cleanupGHEnv captures cleanup inputs for GitHub workflows.
type cleanupGHEnv struct {
	// Issue is the issue number from CODEXCTL_ISSUE_NUMBER.
	Issue int `env:"CODEXCTL_ISSUE_NUMBER"`
	// PR is the PR number from CODEXCTL_PR_NUMBER.
	PR int `env:"CODEXCTL_PR_NUMBER"`
	// Branch is the branch name from CODEXCTL_BRANCH.
	Branch string `env:"CODEXCTL_BRANCH"`
	// Repo is the repository slug from CODEXCTL_REPO.
	Repo string `env:"CODEXCTL_REPO"`
	// WithConfigMap toggles configmap cleanup from CODEXCTL_WITH_CONFIGMAP.
	WithConfigMap bool `env:"CODEXCTL_WITH_CONFIGMAP"`
	// DeleteBranch toggles branch deletion from CODEXCTL_DELETE_BRANCH.
	DeleteBranch bool `env:"CODEXCTL_DELETE_BRANCH"`
	// CloseIssue toggles linked issue close from CODEXCTL_CLOSE_ISSUE.
	CloseIssue bool `env:"CODEXCTL_CLOSE_ISSUE"`
	// IncludeAIRepair toggles ai-repair cleanup from CODEXCTL_INCLUDE_AI_REPAIR.
	IncludeAIRepair bool `env:"CODEXCTL_INCLUDE_AI_REPAIR"`
}

// parseEnv fills target from CODEXCTL_* env vars via caarlos0/env.
func parseEnv(target interface{}) error {
	return envparse.Parse(target)
}

// envPresent reports whether a non-empty env var exists.
func envPresent(key string) bool {
	val, ok := os.LookupEnv(key)
	if !ok {
		return false
	}
	return strings.TrimSpace(val) != ""
}

// parseEnvBool parses a boolean string and reports if it was present and valid.
func parseEnvBool(value string) (bool, bool) {
	if strings.TrimSpace(value) == "" {
		return false, false
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, false
	}
	return parsed, true
}
