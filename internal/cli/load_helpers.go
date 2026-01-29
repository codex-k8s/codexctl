package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/env"
)

func parseInlineVarsAndFiles(cmd *cobra.Command) (env.Vars, []string, error) {
	inlineVars, err := env.ParseInlineVars(cmd.Flag("vars").Value.String())
	if err != nil {
		return nil, nil, err
	}

	varFile := cmd.Flag("var-file").Value.String()
	var varFiles []string
	if varFile != "" {
		varFiles = append(varFiles, varFile)
	}
	return inlineVars, varFiles, nil
}

func loadStackConfigFromCmd(opts *Options, cmd *cobra.Command, slot int) (*config.StackConfig, config.TemplateContext, env.Vars, []string, error) {
	inlineVars, varFiles, err := parseInlineVarsAndFiles(cmd)
	if err != nil {
		return nil, config.TemplateContext{}, nil, nil, err
	}

	loadOpts := config.LoadOptions{
		Env:       opts.Env,
		Namespace: opts.Namespace,
		Slot:      slot,
		UserVars:  inlineVars,
		VarFiles:  varFiles,
	}

	stackCfg, ctxData, err := config.LoadStackConfig(opts.ConfigPath, loadOpts)
	if err != nil {
		return nil, config.TemplateContext{}, nil, nil, err
	}

	return stackCfg, ctxData, inlineVars, varFiles, nil
}

func addVarsFlags(cmd *cobra.Command) {
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")
}

func addRenderFilterFlags(cmd *cobra.Command, onlyServices, skipServices, onlyInfra, skipInfra *string, actionOnly, actionSkip string) {
	cmd.Flags().StringVar(onlyServices, "only-services", "", fmt.Sprintf("%s only selected services (comma-separated names)", actionOnly))
	cmd.Flags().StringVar(skipServices, "skip-services", "", fmt.Sprintf("%s selected services (comma-separated names)", actionSkip))
	cmd.Flags().StringVar(onlyInfra, "only-infra", "", fmt.Sprintf("%s only selected infra blocks (comma-separated names)", actionOnly))
	cmd.Flags().StringVar(skipInfra, "skip-infra", "", fmt.Sprintf("%s selected infra blocks (comma-separated names)", actionSkip))
}
