package cli

import (
	"os"
	"strings"

	envparse "github.com/caarlos0/env/v10"
)

type baseEnv struct {
	ConfigPath string `env:"CODEXCTL_CONFIG"`
	Env        string `env:"CODEXCTL_ENV"`
	Namespace  string `env:"CODEXCTL_NAMESPACE"`
	LogLevel   string `env:"CODEXCTL_LOG_LEVEL"`
}

type varsEnv struct {
	Vars    string `env:"CODEXCTL_VARS"`
	VarFile string `env:"CODEXCTL_VAR_FILE"`
}

type ciEnv struct {
	Slot          int    `env:"CODEXCTL_SLOT"`
	Issue         int    `env:"CODEXCTL_ISSUE_NUMBER"`
	PR            int    `env:"CODEXCTL_PR_NUMBER"`
	MaxSlots      int    `env:"CODEXCTL_DEV_SLOTS_MAX"`
	CodeRootBase  string `env:"CODEXCTL_CODE_ROOT_BASE"`
	Source        string `env:"CODEXCTL_SOURCE"`
	PrepareImages bool   `env:"CODEXCTL_PREPARE_IMAGES"`
	Apply         bool   `env:"CODEXCTL_APPLY"`
	ForceApply    bool   `env:"CODEXCTL_FORCE_APPLY"`
	WaitTimeout   string `env:"CODEXCTL_WAIT_TIMEOUT"`
	WaitSoftFail  bool   `env:"CODEXCTL_WAIT_SOFT_FAIL"`
	Preflight     bool   `env:"CODEXCTL_PREFLIGHT"`
	Wait          bool   `env:"CODEXCTL_WAIT"`
	ApplyRetries  int    `env:"CODEXCTL_APPLY_RETRIES"`
	WaitRetries   int    `env:"CODEXCTL_WAIT_RETRIES"`
	ApplyBackoff  string `env:"CODEXCTL_APPLY_BACKOFF"`
	WaitBackoff   string `env:"CODEXCTL_WAIT_BACKOFF"`
	RequestTime   string `env:"CODEXCTL_REQUEST_TIMEOUT"`
	OnlyServices  string `env:"CODEXCTL_ONLY_SERVICES"`
	SkipServices  string `env:"CODEXCTL_SKIP_SERVICES"`
	OnlyInfra     string `env:"CODEXCTL_ONLY_INFRA"`
	SkipInfra     string `env:"CODEXCTL_SKIP_INFRA"`
	MirrorImages  bool   `env:"CODEXCTL_MIRROR_IMAGES"`
	BuildImages   bool   `env:"CODEXCTL_BUILD_IMAGES"`
}

type promptEnv struct {
	Slot               int    `env:"CODEXCTL_SLOT"`
	Issue              int    `env:"CODEXCTL_ISSUE_NUMBER"`
	PR                 int    `env:"CODEXCTL_PR_NUMBER"`
	FocusIssue         int    `env:"CODEXCTL_FOCUS_ISSUE_NUMBER"`
	Namespace          string `env:"CODEXCTL_NAMESPACE"`
	Kind               string `env:"CODEXCTL_KIND"`
	Lang               string `env:"CODEXCTL_LANG"`
	InfraUnhealthy     bool   `env:"CODEXCTL_INFRA_UNHEALTHY"`
	Resume             bool   `env:"CODEXCTL_RESUME"`
	Model              string `env:"CODEXCTL_MODEL"`
	ReasoningEffort    string `env:"CODEXCTL_MODEL_REASONING_EFFORT"`
	PromptMode         string `env:"CODEXCTL_PROMPT_MODE"`
	PromptContinuation string `env:"CODEXCTL_PROMPT_CONTINUATION"`
}

type planEnv struct {
	Issue int    `env:"CODEXCTL_ISSUE_NUMBER"`
	Repo  string `env:"CODEXCTL_REPO"`
}

type manageEnvEnv struct {
	Slot          int    `env:"CODEXCTL_SLOT"`
	Issue         int    `env:"CODEXCTL_ISSUE_NUMBER"`
	PR            int    `env:"CODEXCTL_PR_NUMBER"`
	All           bool   `env:"CODEXCTL_ALL"`
	WithConfigMap bool   `env:"CODEXCTL_WITH_CONFIGMAP"`
	Lang          string `env:"CODEXCTL_LANG"`
}

type prEnv struct {
	Slot         int    `env:"CODEXCTL_SLOT"`
	PR           int    `env:"CODEXCTL_PR_NUMBER"`
	CodeRootBase string `env:"CODEXCTL_CODE_ROOT_BASE"`
	Lang         string `env:"CODEXCTL_LANG"`
}

func parseEnv(target interface{}) error {
	return envparse.Parse(target)
}

func envPresent(key string) bool {
	val, ok := os.LookupEnv(key)
	if !ok {
		return false
	}
	return strings.TrimSpace(val) != ""
}
