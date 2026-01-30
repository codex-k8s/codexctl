package cli

import (
	"fmt"
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
	Slot          int     `env:"CODEXCTL_SLOT"`
	Issue         int     `env:"CODEXCTL_ISSUE_NUMBER"`
	PR            int     `env:"CODEXCTL_PR_NUMBER"`
	MaxSlots      int     `env:"CODEXCTL_DEV_SLOTS_MAX"`
	CodeRootBase  string  `env:"CODEXCTL_CODE_ROOT_BASE"`
	Source        string  `env:"CODEXCTL_SOURCE"`
	PrepareImages envBool `env:"CODEXCTL_PREPARE_IMAGES"`
	Apply         envBool `env:"CODEXCTL_APPLY"`
	ForceApply    envBool `env:"CODEXCTL_FORCE_APPLY"`
	WaitTimeout   string  `env:"CODEXCTL_WAIT_TIMEOUT"`
	WaitSoftFail  envBool `env:"CODEXCTL_WAIT_SOFT_FAIL"`
	Preflight     envBool `env:"CODEXCTL_PREFLIGHT"`
	Wait          envBool `env:"CODEXCTL_WAIT"`
	ApplyRetries  int     `env:"CODEXCTL_APPLY_RETRIES"`
	WaitRetries   int     `env:"CODEXCTL_WAIT_RETRIES"`
	ApplyBackoff  string  `env:"CODEXCTL_APPLY_BACKOFF"`
	WaitBackoff   string  `env:"CODEXCTL_WAIT_BACKOFF"`
	RequestTime   string  `env:"CODEXCTL_REQUEST_TIMEOUT"`
	OnlyServices  string  `env:"CODEXCTL_ONLY_SERVICES"`
	SkipServices  string  `env:"CODEXCTL_SKIP_SERVICES"`
	OnlyInfra     string  `env:"CODEXCTL_ONLY_INFRA"`
	SkipInfra     string  `env:"CODEXCTL_SKIP_INFRA"`
	MirrorImages  envBool `env:"CODEXCTL_MIRROR_IMAGES"`
	BuildImages   envBool `env:"CODEXCTL_BUILD_IMAGES"`
}

type promptEnv struct {
	Slot               int     `env:"CODEXCTL_SLOT"`
	Issue              int     `env:"CODEXCTL_ISSUE_NUMBER"`
	PR                 int     `env:"CODEXCTL_PR_NUMBER"`
	FocusIssue         int     `env:"CODEXCTL_FOCUS_ISSUE_NUMBER"`
	Namespace          string  `env:"CODEXCTL_NAMESPACE"`
	Kind               string  `env:"CODEXCTL_KIND"`
	Lang               string  `env:"CODEXCTL_LANG"`
	InfraUnhealthy     envBool `env:"CODEXCTL_INFRA_UNHEALTHY"`
	Resume             envBool `env:"CODEXCTL_RESUME"`
	Model              string  `env:"CODEXCTL_MODEL"`
	ReasoningEffort    string  `env:"CODEXCTL_MODEL_REASONING_EFFORT"`
	PromptMode         string  `env:"CODEXCTL_PROMPT_MODE"`
	PromptContinuation string  `env:"CODEXCTL_PROMPT_CONTINUATION"`
}

type planEnv struct {
	Issue int    `env:"CODEXCTL_ISSUE_NUMBER"`
	Repo  string `env:"CODEXCTL_REPO"`
}

type manageEnvEnv struct {
	Slot          int     `env:"CODEXCTL_SLOT"`
	Issue         int     `env:"CODEXCTL_ISSUE_NUMBER"`
	PR            int     `env:"CODEXCTL_PR_NUMBER"`
	All           envBool `env:"CODEXCTL_ALL"`
	WithConfigMap envBool `env:"CODEXCTL_WITH_CONFIGMAP"`
	Lang          string  `env:"CODEXCTL_LANG"`
}

type prEnv struct {
	Slot         int    `env:"CODEXCTL_SLOT"`
	PR           int    `env:"CODEXCTL_PR_NUMBER"`
	CodeRootBase string `env:"CODEXCTL_CODE_ROOT_BASE"`
	Lang         string `env:"CODEXCTL_LANG"`
}

type cleanupGHEnv struct {
	Issue           int     `env:"CODEXCTL_ISSUE_NUMBER"`
	PR              int     `env:"CODEXCTL_PR_NUMBER"`
	Branch          string  `env:"CODEXCTL_BRANCH"`
	Repo            string  `env:"CODEXCTL_REPO"`
	WithConfigMap   envBool `env:"CODEXCTL_WITH_CONFIGMAP"`
	DeleteBranch    envBool `env:"CODEXCTL_DELETE_BRANCH"`
	CloseIssue      envBool `env:"CODEXCTL_CLOSE_ISSUE"`
	IncludeAIRepair envBool `env:"CODEXCTL_INCLUDE_AI_REPAIR"`
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

type envBool bool

func (b *envBool) UnmarshalText(text []byte) error {
	value := strings.TrimSpace(strings.ToLower(string(text)))
	switch value {
	case "", "0", "false", "f", "no", "n", "off":
		*b = false
		return nil
	case "1", "true", "t", "yes", "y", "on":
		*b = true
		return nil
	default:
		return fmt.Errorf("invalid boolean %q", value)
	}
}

func (b *envBool) Bool() bool {
	return bool(*b)
}

func parseEnvBool(value string) (bool, bool) {
	if strings.TrimSpace(value) == "" {
		return false, false
	}
	var b envBool
	if err := b.UnmarshalText([]byte(value)); err != nil {
		return false, false
	}
	return b.Bool(), true
}
