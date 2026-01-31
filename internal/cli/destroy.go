package cli

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/engine"
	"github.com/codex-k8s/codexctl/internal/hooks"
	"github.com/codex-k8s/codexctl/internal/kube"
)

// destroyStack runs the core destroy logic shared by higher-level helpers
// such as "manage-env cleanup".
func destroyStack(
	ctx context.Context,
	logger *slog.Logger,
	stackCfg *config.StackConfig,
	ctxData config.TemplateContext,
	envCfg config.Environment,
	envName string,
) error {
	eng := engine.NewEngine()
	manifests, err := eng.RenderStack(stackCfg, ctxData)
	if err != nil {
		return err
	}
	if envName == "ai-repair" {
		manifests, err = filterManifestKinds(manifests, []string{"Namespace"})
		if err != nil {
			return err
		}
	}

	kubeClient := kube.NewClient()

	hookExec := hooks.NewExecutor(logger)
	hookCtx := hooks.StepContext{
		Stack:      stackCfg,
		Template:   ctxData,
		EnvName:    envName,
		KubeClient: kubeClient,
	}

	// Stack-level and infrastructure/service hooks before destroy.
	if err := hookExec.RunSteps(ctx, stackCfg.Hooks.BeforeAll, hookCtx); err != nil {
		return err
	}
	stageCtx := hookStageContext{stackCfg: stackCfg, ctxData: ctxData, hookCtx: hookCtx}
	beforeStage := hookStage{infra: infraBeforeDestroy, services: serviceBeforeDestroy}
	if err := runHookStage(ctx, hookExec, stageCtx, beforeStage); err != nil {
		return err
	}

	logger.Info("deleting manifests", "env", envName, "namespace", ctxData.Namespace)
	if err := kubeClient.Delete(ctx, manifests, true); err != nil {
		return err
	}

	// Infrastructure/service hooks and stack-level hooks after destroy.
	afterStage := hookStage{infra: infraAfterDestroy, services: serviceAfterDestroy}
	if err := runHookStage(ctx, hookExec, stageCtx, afterStage); err != nil {
		return err
	}
	if err := hookExec.RunSteps(ctx, stackCfg.Hooks.AfterAll, hookCtx); err != nil {
		return err
	}

	return nil
}

// filterManifestKinds removes documents with the specified kinds from a multi-document YAML stream.
func filterManifestKinds(manifests []byte, dropKinds []string) ([]byte, error) {
	if len(manifests) == 0 || len(dropKinds) == 0 {
		return manifests, nil
	}

	drop := make(map[string]struct{}, len(dropKinds))
	for _, kind := range dropKinds {
		k := strings.ToLower(strings.TrimSpace(kind))
		if k != "" {
			drop[k] = struct{}{}
		}
	}

	dec := yaml.NewDecoder(bytes.NewReader(manifests))
	var docs []map[string]any
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if len(doc) == 0 {
			continue
		}
		kind, _ := doc["kind"].(string)
		if kind != "" {
			if _, ok := drop[strings.ToLower(kind)]; ok {
				continue
			}
		}
		docs = append(docs, doc)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			_ = enc.Close()
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
