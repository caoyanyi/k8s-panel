package helmadapter

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/kubernetes"
	"github.com/caoyanyi/k8s-panel/internal/outbound"
	"github.com/caoyanyi/k8s-panel/internal/platform"
)

type Adapter struct {
	timeout time.Duration
	policy  *outbound.Policy
	roots   *x509.CertPool
}

func New(timeout time.Duration, policy *outbound.Policy, roots *x509.CertPool) *Adapter {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	return &Adapter{timeout: timeout, policy: policy, roots: roots}
}

func (a *Adapter) List(
	ctx context.Context,
	connection kubernetes.Connection,
	namespace string,
) ([]domain.HelmRelease, error) {
	if err := a.validateConnection(ctx, connection); err != nil {
		return nil, err
	}
	configuration, _, cleanup, err := actionConfiguration(connection, namespace, a.policy, a.timeout)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := action.NewList(configuration)
	client.All = true
	client.StateMask = action.ListAll
	client.AllNamespaces = namespace == ""
	releases, err := client.Run()
	if err != nil {
		return nil, classifyHelmError(err)
	}
	result := make([]domain.HelmRelease, 0, len(releases))
	for _, item := range releases {
		mapped := domain.HelmRelease{
			Name:      item.Name,
			Namespace: item.Namespace,
			Revision:  item.Version,
		}
		if item.Info != nil {
			mapped.Status = item.Info.Status.String()
			mapped.UpdatedAt = item.Info.LastDeployed.Time
		}
		if item.Chart != nil && item.Chart.Metadata != nil {
			mapped.Chart = item.Chart.Metadata.Name
			if item.Chart.Metadata.Version != "" {
				mapped.Chart += "-" + item.Chart.Metadata.Version
			}
			mapped.AppVersion = item.Chart.Metadata.AppVersion
		}
		result = append(result, mapped)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Namespace != result[j].Namespace {
			return result[i].Namespace < result[j].Namespace
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (a *Adapter) Execute(ctx context.Context, kind domain.OperationKind, request platform.HelmRequest) error {
	if err := a.validateConnection(ctx, request.Connection); err != nil {
		return err
	}
	configuration, settings, cleanup, err := actionConfiguration(
		request.Connection,
		request.Input.Namespace,
		a.policy,
		a.timeout,
	)
	if err != nil {
		return err
	}
	defer cleanup()

	switch kind {
	case domain.OperationHelmInstall:
		return a.install(ctx, configuration, settings, request)
	case domain.OperationHelmUpgrade:
		return a.upgrade(ctx, configuration, settings, request)
	case domain.OperationHelmRollback:
		client := action.NewRollback(configuration)
		client.Version = request.Input.Revision
		client.Timeout = a.timeout
		client.Wait = true
		client.WaitForJobs = true
		client.CleanupOnFail = true
		if err := client.Run(request.Input.ReleaseName); err != nil {
			return classifyHelmError(err)
		}
		return nil
	case domain.OperationHelmUninstall:
		client := action.NewUninstall(configuration)
		client.Timeout = a.timeout
		client.Wait = true
		client.DeletionPropagation = "foreground"
		if _, err := client.Run(request.Input.ReleaseName); err != nil {
			return classifyHelmError(err)
		}
		return nil
	default:
		return domain.Invalid("kind", "unsupported Helm operation")
	}
}

func (a *Adapter) install(
	ctx context.Context,
	configuration *action.Configuration,
	settings *cli.EnvSettings,
	request platform.HelmRequest,
) error {
	values, err := parseValues(request.Input.Values)
	if err != nil {
		return err
	}
	chartOptions, err := chartOptions(request.Input.Chart, request.Input.Version, request.Repository)
	if err != nil {
		return err
	}
	loadedChart, err := locateAndLoadChart(ctx, request.Input.Chart, chartOptions, settings, a.policy, a.roots, a.timeout)
	if err != nil {
		return classifyHelmError(err)
	}
	client := action.NewInstall(configuration)
	client.ChartPathOptions = chartOptions
	client.ReleaseName = request.Input.ReleaseName
	client.Namespace = request.Input.Namespace
	client.Timeout = a.timeout
	client.Atomic = true
	client.Wait = true
	client.WaitForJobs = true
	client.CreateNamespace = false
	client.DependencyUpdate = false
	client.EnableDNS = false
	client.HideNotes = true
	if _, err := client.RunWithContext(ctx, loadedChart, values); err != nil {
		return classifyHelmError(err)
	}
	return nil
}

func (a *Adapter) upgrade(
	ctx context.Context,
	configuration *action.Configuration,
	settings *cli.EnvSettings,
	request platform.HelmRequest,
) error {
	values, err := parseValues(request.Input.Values)
	if err != nil {
		return err
	}
	chartOptions, err := chartOptions(request.Input.Chart, request.Input.Version, request.Repository)
	if err != nil {
		return err
	}
	loadedChart, err := locateAndLoadChart(ctx, request.Input.Chart, chartOptions, settings, a.policy, a.roots, a.timeout)
	if err != nil {
		return classifyHelmError(err)
	}
	client := action.NewUpgrade(configuration)
	client.ChartPathOptions = chartOptions
	client.Namespace = request.Input.Namespace
	client.Timeout = a.timeout
	client.Atomic = true
	client.CleanupOnFail = true
	client.Wait = true
	client.WaitForJobs = true
	client.DependencyUpdate = false
	client.EnableDNS = false
	client.MaxHistory = 10
	client.HideNotes = true
	if _, err := client.RunWithContext(ctx, request.Input.ReleaseName, loadedChart, values); err != nil {
		return classifyHelmError(err)
	}
	return nil
}

func parseValues(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	values, err := chartutil.ReadValues([]byte(raw))
	if err != nil {
		return nil, domain.Invalid("values", "must be valid YAML with a mapping root")
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, domain.Invalid("values", "contains unsupported values")
	}
	var normalized map[string]any
	if err := json.Unmarshal(encoded, &normalized); err != nil || normalized == nil {
		return nil, domain.Invalid("values", "must have a mapping root")
	}
	return normalized, nil
}

func chartOptions(
	chartReference string,
	version string,
	repository *platform.RepositoryConnection,
) (action.ChartPathOptions, error) {
	chartReference = strings.TrimSpace(chartReference)
	options := action.ChartPathOptions{Version: version}
	if strings.HasPrefix(chartReference, "oci://") {
		parsed, err := url.Parse(chartReference)
		if err != nil || parsed.Host == "" || parsed.Path == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return action.ChartPathOptions{}, domain.Invalid("chart", "invalid OCI reference")
		}
		return options, nil
	}
	if repository == nil || repository.URL == "" || strings.HasPrefix(chartReference, "/") ||
		strings.Contains(chartReference, "..") || strings.Contains(chartReference, `\`) || strings.Contains(chartReference, "://") {
		return action.ChartPathOptions{}, domain.Invalid("chart", "local and unconfigured chart references are not allowed")
	}
	options.RepoURL = repository.URL
	options.Username = repository.Username
	options.Password = repository.Password
	options.PassCredentialsAll = false
	options.InsecureSkipTLSverify = false
	options.PlainHTTP = false
	return options, nil
}

func locateAndLoadChart(
	ctx context.Context,
	reference string,
	options action.ChartPathOptions,
	settings *cli.EnvSettings,
	policy *outbound.Policy,
	roots *x509.CertPool,
	timeout time.Duration,
) (*chart.Chart, error) {
	if strings.HasPrefix(reference, "oci://") {
		return loadOCIChart(ctx, reference, options.Version, policy, roots, timeout)
	}
	chartGetter, err := newSecureHTTPGetter(ctx, policy, roots, options.RepoURL, options.Username, options.Password, timeout)
	if err != nil {
		return nil, err
	}
	providers := getter.Providers{{
		Schemes: []string{"https"},
		New: func(...getter.Option) (getter.Getter, error) {
			return chartGetter, nil
		},
	}}
	chartURL, err := repo.FindChartInAuthAndTLSAndPassRepoURL(
		options.RepoURL,
		options.Username,
		options.Password,
		reference,
		options.Version,
		"",
		"",
		"",
		false,
		false,
		providers,
	)
	if err != nil {
		return nil, err
	}
	archive, err := chartGetter.Get(chartURL)
	if err != nil {
		return nil, err
	}
	return loader.LoadArchive(bytes.NewReader(archive.Bytes()))
}

func actionConfiguration(
	connection kubernetes.Connection,
	namespace string,
	policy *outbound.Policy,
	timeout time.Duration,
) (*action.Configuration, *cli.EnvSettings, func(), error) {
	if policy == nil {
		return nil, nil, nil, errors.New("outbound policy is required")
	}
	workspace, err := os.MkdirTemp("", "k8s-panel-helm-")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create Helm workspace: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(workspace) }
	for _, directory := range []string{"cache", "plugins"} {
		if err := os.Mkdir(filepath.Join(workspace, directory), 0o700); err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("create Helm workspace directory: %w", err)
		}
	}
	settings := cli.New()
	settings.KubeAPIServer = connection.Server
	settings.KubeToken = connection.BearerToken
	settings.KubeInsecureSkipTLSVerify = false
	if parsed, err := url.Parse(connection.Server); err == nil {
		settings.KubeTLSServerName = parsed.Hostname()
	}
	settings.RepositoryConfig = filepath.Join(workspace, "repositories.yaml")
	settings.RepositoryCache = filepath.Join(workspace, "cache")
	settings.RegistryConfig = filepath.Join(workspace, "registry.json")
	settings.PluginsDirectory = filepath.Join(workspace, "plugins")
	if namespace == "" {
		namespace = "default"
	}
	settings.SetNamespace(namespace)
	if connection.CACert != "" {
		caPath := filepath.Join(workspace, "cluster-ca.pem")
		if err := os.WriteFile(caPath, []byte(connection.CACert), 0o600); err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("write Helm CA file: %w", err)
		}
		settings.KubeCaFile = caPath
	}
	restGetter, ok := settings.RESTClientGetter().(*genericclioptions.ConfigFlags)
	if !ok {
		cleanup()
		return nil, nil, nil, errors.New("unsupported Kubernetes REST client configuration")
	}
	restGetter.WrapConfigFn = func(input *rest.Config) *rest.Config {
		secured := rest.CopyConfig(input)
		secured.Dial = policy.DialContext
		secured.Proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
		secured.Timeout = timeout
		return secured
	}
	configuration := new(action.Configuration)
	if err := configuration.Init(restGetter, namespace, "secret", func(string, ...any) {}); err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("initialize Helm client: %w", err)
	}
	return configuration, settings, cleanup, nil
}

func (a *Adapter) validateConnection(ctx context.Context, connection kubernetes.Connection) error {
	if a.policy == nil {
		return errors.New("outbound policy is required")
	}
	validationContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := a.policy.ValidateHTTPSURL(validationContext, connection.Server); err != nil {
		return domain.ErrUpstream
	}
	return nil
}

func classifyHelmError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "cannot re-use a name") || strings.Contains(message, "another operation") {
		return domain.ErrConflict
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return domain.ErrTimeout
	}
	return domain.ErrUpstream
}
