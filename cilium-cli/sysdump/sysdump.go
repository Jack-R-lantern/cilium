// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package sysdump

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cilium/workerpool"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/kubectl/pkg/util/podutils"

	"github.com/cilium/cilium/cilium-cli/defaults"
	"github.com/cilium/cilium/cilium-cli/k8s"
	"github.com/cilium/cilium/cilium-cli/utils/features"
	ciliumdef "github.com/cilium/cilium/pkg/defaults"
)

const sysdumpLogFile = "cilium-sysdump.log"
const ciliumHelmReleaseName = "cilium"
const tetragonHelmReleaseName = "tetragon"

// Options groups together the set of options required to collect a sysdump.
type Options struct {
	// The labels used to target Cilium pods.
	CiliumLabelSelector string
	// The namespace Cilium is running in.
	CiliumNamespace string
	// The namespace Cilium operator is running in.
	CiliumOperatorNamespace string
	// The namespace Cilium SPIRE installation is running in.
	CiliumSPIRENamespace string
	// The labels used to target Cilium daemon set. Usually, this label is same as CiliumLabelSelector.
	CiliumDaemonSetSelector string
	// The labels used to target Cilium Envoy pods.
	CiliumEnvoyLabelSelector string
	// The release name of Cilium Helm chart.
	CiliumHelmReleaseName string
	// The release name of Tetragon Helm chart.
	TetragonHelmReleaseName string
	// The labels used to target Cilium Node Init daemon set. Usually, this label is same as CiliumNodeInitLabelSelector.
	CiliumNodeInitDaemonSetSelector string
	// The labels used to target Cilium Node Init pods.
	CiliumNodeInitLabelSelector string
	// The labels used to target Cilium operator pods.
	CiliumOperatorLabelSelector string
	// The labels used to target 'clustermesh-apiserver' pods.
	ClustermeshApiserverLabelSelector string
	// The labels used to target Cilium SPIRE server pods.
	CiliumSPIREServerLabelSelector string
	// The labels used to target Cilium SPIRE agent pods.
	CiliumSPIREAgentLabelSelector string
	// Whether to collect logs from cilium-agent pods that are crashing.
	CollectLogsFromNotReadyAgents bool
	// Whether to enable debug logging.
	Debug bool
	// Whether to enable scraping profiling data.
	Profiling bool
	// Whether to enable scraping tracing data.
	Tracing bool
	// The labels used to target additional pods
	ExtraLabelSelectors []string
	// The labels used to target Hubble pods.
	HubbleLabelSelector string
	// Number of Hubble flows to collect.
	HubbleFlowsCount int64
	// Timeout for collecting Hubble flows.
	HubbleFlowsTimeout time.Duration
	// The labels used to target Hubble Relay pods.
	HubbleRelayLabelSelector string
	// The labels used to target Hubble UI pods.
	HubbleUILabelSelector string
	// The labels used to target Hubble generate certs pods.
	HubbleGenerateCertsLabelSelector string
	// The amount of time to wait for the user to cancel the sysdump on a large cluster.
	LargeSysdumpAbortTimeout time.Duration
	// The threshold on the number of nodes present in the cluster that triggers a warning message.
	LargeSysdumpThreshold int
	// The limit on the number of bytes to retrieve when collecting logs
	LogsLimitBytes int64
	// How far back in time to go when collecting logs.
	LogsSinceTime time.Duration
	// Comma-separated list of node IPs or names to filter pods for which to collect gops and logs.
	NodeList string
	// The name of the resulting file (without extension)\n'<ts>' can be used as the placeholder for the timestamp.
	OutputFileName string
	// Whether to enable quick mode (i.e. skip collection of 'cilium-bugtool' output and logs).
	Quick bool
	// A 'RESTClientGetter' that can be used to create REST clients for the Kubernetes API.
	// Required at least for getting the proper output of 'kubectl get pod -o wide' without actually using 'kubectl'.
	RESTClientGetter genericclioptions.RESTClientGetter
	// The number of workers to use.
	WorkerCount int
	// The writer used for logging.
	Writer io.Writer
	// Flags to pass to cilium-bugtool command
	CiliumBugtoolFlags []string
	// Whether to automatically detect the gops agent PID
	DetectGopsPID bool
	// Directory where CNI configs are located
	CNIConfigDirectory string
	// The name of the CNI config map
	CNIConfigMapName string
	// The labels used to target Tetragon pods.
	TetragonLabelSelector string
	// The labels used to target Tetragon oeprator pods.
	TetragonOperatorLabelSelector string
	// The namespace Namespace is running in.
	TetragonNamespace string
	// Retry limit for copying files from pods
	CopyRetryLimit int
}

// Task defines a task for the sysdump collector to execute.
type Task struct {
	// MUST be set to true if the task submits additional tasks to the worker pool.
	CreatesSubtasks bool
	// The description of the task.
	Description string
	// Whether this task runs when running in quick mode.
	Quick bool
	// The task itself.
	Task func(context.Context) error
}

// Collector knows how to collect information required to troubleshoot issues with Cilium and Hubble.
type Collector struct {
	Client  KubernetesClient
	Options Options
	Pool    *workerpool.WorkerPool
	// logFile is the log file for the sydump log messages.
	logFile *os.File
	// logWriter is the io.Writer used for logging.
	logWriter io.Writer
	// subtasksWg is used to wait for subtasks to be submitted to the pool before calling 'Drain'.
	// It is required since we don't know beforehand how many sub-tasks will be created, as they depend on the number of Cilium/Hubble/... pods found by "main" tasks.
	subtasksWg sync.WaitGroup
	// startTime keeps track of the time this sysdump collector got initialized. This timestamp
	// is used to substitute '<ts>' in filenames.
	startTime time.Time
	// Directory to collect sysdump in.
	sysdumpDir string
	// allNodes is a list of all the node names in the cluster.
	allNodes *corev1.NodeList
	// NodeList is a list of nodes to collect sysdump information from.
	NodeList []string
	// CiliumPods is a list of Cilium agent pods running on nodes in NodeList.
	CiliumPods []*corev1.Pod
	// CiliumNotReadyPods is a list of additional Cilium agent pods to collect logs from
	// regardless of node filter.
	CiliumNotReadyPods []*corev1.Pod
	// CiliumOperatorPods is the list of Cilium operator pods.
	CiliumOperatorPods []*corev1.Pod
	// CiliumConfigMap is a pointer to cilium-config ConfigMap.
	CiliumConfigMap *corev1.ConfigMap
	// additionalTasks keeps track of additional tasks added via AddTasks.
	additionalTasks []Task
	// FeatureSet is a map of enabled / disabled features based on the contents of cilium-config ConfigMap.
	FeatureSet features.Set
}

// NewCollector returns a new sysdump collector.
func NewCollector(
	k KubernetesClient,
	o Options,
	hooks Hooks,
	startTime time.Time,
) (*Collector, error) {
	c := &Collector{
		Client:     k,
		Options:    o,
		startTime:  startTime,
		FeatureSet: features.Set{},
	}
	tmp, err := os.MkdirTemp("", "*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}
	c.sysdumpDir = filepath.Join(tmp, c.replaceTimestamp(c.Options.OutputFileName))
	if err = os.MkdirAll(c.sysdumpDir, dirMode); err != nil {
		return nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}
	if err = c.setupLogging(o.Writer); err != nil {
		return nil, err
	}
	c.logDebug("Using %v as a temporary directory", c.sysdumpDir)
	c.logTask("Collecting sysdump with cilium-cli version: %s, args: %s", defaults.CLIVersion, os.Args[1:])

	if c.Options.CiliumNamespace == "" {
		ns, err := detectCiliumNamespace(k)
		if err == nil {
			c.log("🔮 Detected Cilium installation in namespace: %q", ns)
			c.Options.CiliumNamespace = ns
		} else {
			c.logWarn("Failed to detect Cilium installation")
		}
	} else {
		c.log("ℹ️  Cilium namespace: %s", c.Options.CiliumNamespace)
	}

	if c.Options.CiliumOperatorNamespace == "" {
		ns, err := detectCiliumOperatorNamespace(k)
		if err == nil {
			c.log("🔮 Detected Cilium operator in namespace: %q", ns)
			c.Options.CiliumOperatorNamespace = ns
		} else {
			c.logWarn("Failed to detect Cilium operator")
		}
	} else {
		c.log("ℹ️  Cilium operator namespace: %s", c.Options.CiliumOperatorNamespace)
	}

	if c.Options.CiliumHelmReleaseName == "" {
		c.log("ℹ️ Using default Cilium Helm release name: %q", ciliumHelmReleaseName)
		c.Options.CiliumHelmReleaseName = ciliumHelmReleaseName
	} else {
		c.log("ℹ️ Cilium Helm release name: %q", c.Options.CiliumHelmReleaseName)
	}

	if c.Options.TetragonHelmReleaseName == "" {
		c.log("ℹ️ Using default Tetragon Helm release name: %q", tetragonHelmReleaseName)
		c.Options.TetragonHelmReleaseName = tetragonHelmReleaseName
	} else {
		c.log("ℹ️ Tetragon Helm release name: %q", c.Options.TetragonHelmReleaseName)
	}

	if c.Options.CiliumSPIRENamespace == "" {
		if ns, err := detectCiliumSPIRENamespace(k); err != nil {
			c.logDebug("Failed to detect Cilium SPIRE installation: %v", err)
			if c.Options.CiliumOperatorNamespace != "" {
				c.log("ℹ️ Failed to detect Cilium SPIRE installation - using Cilium namespace as Cilium SPIRE namespace: %q", c.Options.CiliumOperatorNamespace)
				c.Options.CiliumSPIRENamespace = c.Options.CiliumOperatorNamespace
			}
		} else {
			c.log("🔮 Detected Cilium SPIRE installation in namespace: %q", ns)
			c.Options.CiliumSPIRENamespace = ns
		}
	} else {
		c.log("ℹ️  Cilium SPIRE namespace: %q", c.Options.CiliumSPIRENamespace)
	}

	// Grab the Kubernetes nodes for the target cluster.
	c.logTask("Collecting Kubernetes nodes")
	c.allNodes, err = c.Client.ListNodes(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to collect Kubernetes nodes: %w", err)
	}
	c.logDebug("Finished collecting Kubernetes nodes")

	// Exit if there are no nodes, as there's nothing to do.
	if len(c.allNodes.Items) == 0 {
		return nil, fmt.Errorf("no nodes found in the current cluster")
	}
	// If there are many nodes and no filters are specified, issue a warning and wait for a while before proceeding so the user can cancel the process.
	if len(c.allNodes.Items) > c.Options.LargeSysdumpThreshold && (c.Options.NodeList == DefaultNodeList && c.Options.LogsLimitBytes == DefaultLogsLimitBytes && c.Options.LogsSinceTime == DefaultLogsSinceTime) {
		c.logWarn("Detected a large cluster (%d nodes, threshold is %d)", len(c.allNodes.Items), c.Options.LargeSysdumpThreshold)
		c.logWarn("Consider using a node filter (--node-list option, default=\"\"),")
		c.logWarn("a custom log size limit (--logs-limit-bytes option, default=1GiB)")
		c.logWarn("and/or a custom log time range (--logs-since-time option, default=1y)")
		c.logWarn("to decrease the size of the sysdump")
		c.logWarn("Waiting for %s before continuing, press control+c to abort and adjust your options", c.Options.LargeSysdumpAbortTimeout)
		t := time.NewTicker(c.Options.LargeSysdumpAbortTimeout)
		defer t.Stop()
		<-t.C
	}

	// Build the list of node names in which the user is interested.
	c.NodeList = buildNodeNameList(c.allNodes, c.Options.NodeList)
	c.logDebug("Restricting bugtool and logs collection to pods in %v", c.NodeList)

	if c.Options.CiliumNamespace != "" {
		ciliumPods, err := c.Client.ListPods(context.Background(), c.Options.CiliumNamespace, metav1.ListOptions{
			LabelSelector: c.Options.CiliumLabelSelector,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get Cilium pods: %w", err)
		}
		c.CiliumPods = FilterPods(ciliumPods, c.NodeList)
		if c.Options.CollectLogsFromNotReadyAgents {
			crashedPods := filterCrashedPods(ciliumPods, 5)
			notReady := filterRunningNotReadyPods(ciliumPods, 5)
			restartedPods := filterRestartedContainersPods(ciliumPods, 5)
			allPods := slices.Concat(crashedPods, notReady, restartedPods)

			for _, pod := range allPods {
				if !slices.ContainsFunc(c.CiliumNotReadyPods, func(p *corev1.Pod) bool {
					return p.Name == pod.Name
				}) {
					c.CiliumNotReadyPods = append(c.CiliumNotReadyPods, pod)
				}
			}
			if len(c.CiliumNotReadyPods) > 0 {
				c.logWarn("Collecting additional logs from %d not ready/crashing/restarted Cilium agent pods", len(c.CiliumNotReadyPods))
			}
		}

		c.CiliumConfigMap, err = c.Client.GetConfigMap(context.Background(), c.Options.CiliumNamespace, ciliumConfigMapName, metav1.GetOptions{})
		if err != nil {
			if !k8sErrors.IsNotFound(err) {
				return nil, fmt.Errorf("failed to get %s ConfigMap: %w", ciliumConfigMapName, err)
			}
			c.log("ℹ️  %s ConfigMap not found in %s namespace", ciliumConfigMapName, c.Options.CiliumNamespace)
		}
		if c.CiliumConfigMap != nil {
			c.FeatureSet.ExtractFromConfigMap(c.CiliumConfigMap)
			c.log("🔮 Detected Cilium features: %v", c.FeatureSet)
		}
	}

	if c.Options.CiliumOperatorNamespace != "" {
		pods, err := c.Client.ListPods(context.Background(), c.Options.CiliumOperatorNamespace, metav1.ListOptions{
			LabelSelector: c.Options.CiliumOperatorLabelSelector,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get Cilium operator pods: %w", err)
		}
		c.CiliumOperatorPods = AllPods(pods)
	}

	if err := hooks.AddSysdumpTasks(c); err != nil {
		return nil, fmt.Errorf("failed to add custom sysdump tasks: %w", err)
	}

	return c, nil
}

// GatherResourceUnstructured queries resources with the given GroupVersionResource, storing them in the file specified by fname.
// If keep is non-empty; then it will filter the items returned, keeping only those with names listed in keep.
// If keep is empty, it will not filter the resources returned.
func (c *Collector) GatherResourceUnstructured(ctx context.Context, r schema.GroupVersionResource, fname string, keep ...string) error {
	n := corev1.NamespaceAll
	v, err := c.Client.ListUnstructured(ctx, r, &n, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to collect %s (%s): %w", r.Resource, r.Version, err)
	}

	filtered := &unstructured.UnstructuredList{
		Object: v.Object,
	}
	// keep everything if keep is empty
	if len(keep) == 0 {
		filtered.Items = v.Items
	} else {
		// only save the resources which are specified by keep
		for _, elem := range v.Items {
			for _, name := range keep {
				if name == elem.GetName() {
					filtered.Items = append(filtered.Items, elem)
				}
			}
		}
	}
	if err := c.WriteYAML(fname, filtered); err != nil {
		return fmt.Errorf("failed to write %s YAML: %w", r.Resource, err)
	}
	return nil
}

// setupLogging sets up sysdump collector loggging.
func (c *Collector) setupLogging(w io.Writer) error {
	var err error
	c.logFile, err = os.Create(filepath.Join(c.sysdumpDir, sysdumpLogFile))
	if err != nil {
		return fmt.Errorf("failed to create sysdump log file: %w", err)
	}
	// Log to stdout and to a file in the sysdump itself
	c.logWriter = io.MultiWriter(w, c.logFile)
	return nil
}

// teardownLogging flushes writes to the sysdump collector log file and closes it.
func (c *Collector) teardownLogging() {
	// flush writes to log file
	c.logFile.Sync()
	// ...and close the log file
	c.logFile.Close()
	// rewire logger for the remaining log messages which won't make it into to log file
	c.logWriter = os.Stdout
}

// replaceTimestamp can be used to replace the special timestamp placeholder in file and directory names.
func (c *Collector) replaceTimestamp(f string) string {
	return strings.ReplaceAll(f, timestampPlaceholderFileName, c.startTime.Format(timeFormat))
}

// AbsoluteTempPath returns the absolute path where to store the specified filename temporarily.
func (c *Collector) AbsoluteTempPath(f string) string {
	return path.Join(c.sysdumpDir, c.replaceTimestamp(f))
}

func (c *Collector) WithFileSink(filename string, fn func(io.Writer) error) error {
	path := c.AbsoluteTempPath(filename)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return err
	}

	return errors.Join(
		fn(file),
		file.Close(),
	)
}

// WriteYAML writes a kubernetes object to a file as YAML.
func (c *Collector) WriteYAML(filename string, o runtime.Object) error {
	return c.WithFileSink(filename, func(w io.Writer) error {
		return writeYAML(o, w)
	})
}

// WriteString writes a string to a file.
func (c *Collector) WriteString(filename string, value string) error {
	return c.WithFileSink(filename, func(out io.Writer) error {
		_, err := fmt.Fprint(out, value)
		return err
	})
}

// WriteTable writes a kubernetes table to a file.
func (c *Collector) WriteTable(filename string, value *metav1.Table) error {
	return c.WithFileSink(filename, func(out io.Writer) error {
		return writeTable(value, out)
	})
}

// WriteEventTable writes writes a html summary of cluster events to a file.
func (c *Collector) WriteEventTable(filename string, events []corev1.Event) error {
	return c.WithFileSink(filename, func(out io.Writer) error {
		return writeEventTable(events, out)
	})
}

// Run performs the actual sysdump collection.
func (c *Collector) Run() error {
	// tasks is the list of base tasks to be run.
	tasks := []Task{

		{
			Description: "Collect Kubernetes nodes",
			Quick:       true,
			Task: func(_ context.Context) error {
				if err := c.WriteYAML(kubernetesNodesFileName, c.allNodes); err != nil {
					return fmt.Errorf("failed to collect Kubernetes nodes: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collect Kubernetes version",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetVersion(ctx)
				if err != nil {
					return fmt.Errorf("failed to collect Kubernetes version: %w", err)
				}
				if err := c.WriteString(kubernetesVersionInfoFileName, v); err != nil {
					return fmt.Errorf("failed to dump Kubernetes version: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Kubernetes events",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListEvents(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Kubernetes events: %w", err)
				}
				if err := c.WriteYAML(kubernetesEventsFileName, v); err != nil {
					return fmt.Errorf("failed to collect Kubernetes events: %w", err)
				}
				if err := c.WriteEventTable(kubernetesEventsTableFileName, v.Items); err != nil {
					return fmt.Errorf("failed to write event table: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Kubernetes namespaces",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListNamespaces(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Kubernetes namespaces: %w", err)
				}
				if err := c.WriteYAML(kubernetesNamespacesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Kubernetes namespaces: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Kubernetes pods",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListPods(ctx, corev1.NamespaceAll, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Kubernetes pods: %w", err)
				}
				if err := c.WriteYAML(kubernetesPodsFileName, v); err != nil {
					return fmt.Errorf("failed to collect Kubernetes pods: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Kubernetes pods summary",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetPodsTable(ctx)
				if err != nil {
					return fmt.Errorf("failed to collect Kubernetes pods summary: %w", err)
				}
				if err := c.WriteTable(kubernetesPodsSummaryFileName, v); err != nil {
					return fmt.Errorf("failed to collect Kubernetes pods summary: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Kubernetes services",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListServices(ctx, corev1.NamespaceAll, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Kubernetes services: %w", err)
				}
				if err := c.WriteYAML(kubernetesServicesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Kubernetes services: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Kubernetes network policies",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListNetworkPolicies(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Kubernetes network policies: %w", err)
				}
				if err := c.WriteYAML(kubernetesNetworkPoliciesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Kubernetes network policies: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Kubernetes endpoints",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListEndpoints(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Kubernetes endpoints: %w", err)
				}
				if err := c.WriteYAML(kubernetesEndpointsFileName, v); err != nil {
					return fmt.Errorf("failed to collect Kubernetes endpoints: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Kubernetes endpointslices",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListEndpointSlices(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Kubernetes endpointslices: %w", err)
				}
				if err := c.WriteYAML(kubernetesEndpointSlicesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Kubernetes endpointslices: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Kubernetes leases",
			Quick:       true,
			Task: func(ctx context.Context) error {
				n := corev1.NamespaceAll
				v, err := c.Client.ListUnstructured(ctx, k8sLeases, &n, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Kubernetes leases: %w", err)
				}
				if err := c.WriteYAML(kubernetesLeasesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Kubernetes leases: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Kubernetes metrics",
			Quick:       true,
			Task: func(ctx context.Context) error {
				result, err := c.Client.GetRaw(ctx, "/metrics")
				if err != nil {
					return fmt.Errorf("failed to collect Kubernetes metrics: %w", err)
				}
				if err := c.WriteString(kubernetesMetricsFileName, result); err != nil {
					return fmt.Errorf("failed to collect Kubernetes metrics: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting crashed test pod logs",
			Quick:       false,
			Task: func(ctx context.Context) error {
				namespaces, err := c.Client.ListNamespaces(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to get namespaces")
				}
				for _, namespace := range namespaces.Items {
					if !strings.HasPrefix(namespace.Name, defaults.ConnectivityCheckNamespace) {
						continue
					}

					p, err := c.Client.ListPods(ctx, namespace.Name, metav1.ListOptions{})
					if err != nil {
						return fmt.Errorf("failed to get logs from crashloop/restarted pods: %w", err)
					}
					if err := c.SubmitLogsTasks(append(
						filterCrashedPods(p, 0),
						filterRestartedContainersPods(p, 0)...), c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
						return fmt.Errorf("failed to collect logs from crashloop/restarted pods: %w", err)
					}
				}
				return nil
			},
		},
	}

	// task that needs to be executed "serially" (i.e: not concurrently with other tasks).
	var serialTasks []Task

	ciliumTasks := []Task{
		{
			Description: "Collecting Cilium network policies",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumNetworkPolicies(ctx, corev1.NamespaceAll, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Cilium network policies: %w", err)
				}
				if err := c.WriteYAML(ciliumNetworkPoliciesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Cilium network policies: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Cilium cluster-wide network policies",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumClusterwideNetworkPolicies(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Cilium cluster-wide network policies: %w", err)
				}
				if err := c.WriteYAML(ciliumClusterWideNetworkPoliciesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Cilium cluster-wide network policies: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Cilium Egress Gateway policies",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumEgressGatewayPolicies(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Cilium Egress Gateway policies: %w", err)
				}
				if err := c.WriteYAML(ciliumEgressGatewayPoliciesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Cilium Egress Gateway policies: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Cilium CIDR Groups",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumCIDRGroups(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Cilium CIDR Groups: %w", err)
				}
				if err := c.WriteYAML(ciliumCIDRGroupsFileName, v); err != nil {
					return fmt.Errorf("failed to write Cilium CIDR Groups to file: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Cilium local redirect policies",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumLocalRedirectPolicies(ctx, corev1.NamespaceAll, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Cilium local redirect policies: %w", err)
				}
				if err := c.WriteYAML(ciliumLocalRedirectPoliciesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Cilium local redirect policies: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Cilium endpoints",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumEndpoints(ctx, corev1.NamespaceAll, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Cilium endpoints: %w", err)
				}
				if err := c.WriteYAML(ciliumEndpointsFileName, v); err != nil {
					return fmt.Errorf("failed to collect Cilium endpoints: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Cilium endpoint slices",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumEndpointSlices(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Cilium endpoint slices: %w", err)
				}
				if err := c.WriteYAML(ciliumEndpointSlicesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Cilium endpoint slices: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Cilium identities",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumIdentities(ctx)
				if err != nil {
					return fmt.Errorf("failed to collect Cilium identities: %w", err)
				}
				if err := c.WriteYAML(ciliumIdentitiesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Cilium identities: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Cilium nodes",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumNodes(ctx)
				if err != nil {
					return fmt.Errorf("failed to collect Cilium nodes: %w", err)
				}
				if err := c.WriteYAML(ciliumNodesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Cilium nodes: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Cilium Node Configs",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumNodeConfigs(ctx, corev1.NamespaceAll, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Cilium Node configs: %w", err)
				}
				if err := c.WriteYAML(ciliumNodeConfigsFileName, v); err != nil {
					return fmt.Errorf("failed to collect Cilium Node configs: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Ingresses",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListIngresses(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Ingresses: %w", err)
				}
				if err := c.WriteYAML(ciliumIngressesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Ingresses: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting IngressClasses",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListIngressClasses(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect IngressClasses: %w", err)
				}
				if err := c.WriteYAML(ingressClassesFileName, v); err != nil {
					return fmt.Errorf("failed to collect IngressClasses: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Cilium Pod IP Pools",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumPodIPPools(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Cilium Pod IP pools: %w", err)
				}
				if err := c.WriteYAML(ciliumPodIPPoolsFileName, v); err != nil {
					return fmt.Errorf("failed to collect Cilium Pod IP pools: %w", err)
				}
				return nil
			},
		},
		{
			Description: fmt.Sprintf("Checking if %s exists in %s namespace", ciliumEtcdSecretsSecretName, c.Options.CiliumNamespace),
			Quick:       true,
			Task: func(ctx context.Context) error {
				_, err := c.Client.GetSecret(ctx, c.Options.CiliumNamespace, ciliumEtcdSecretsSecretName, metav1.GetOptions{})
				if err != nil {
					if k8sErrors.IsNotFound(err) {
						c.log("Secret %q not found in namespace %q - this is expected when using the CRD KVStore", ciliumEtcdSecretsSecretName, c.Options.CiliumNamespace)
						return nil
					}
					return fmt.Errorf("failed to collect Cilium etcd secret: %w", err)
				}
				c.log("Secret %q found in namespace %q", ciliumEtcdSecretsSecretName, c.Options.CiliumNamespace)
				return nil
			},
		},
		{
			Description: "Collecting the Cilium configuration",
			Quick:       true,
			Task: func(_ context.Context) error {
				if c.CiliumConfigMap == nil {
					return nil
				}
				if err := c.WriteYAML(ciliumConfigMapFileName, c.CiliumConfigMap); err != nil {
					return fmt.Errorf("failed to collect the Cilium configuration: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the Cilium daemonset(s)",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListDaemonSet(ctx, c.Options.CiliumNamespace, metav1.ListOptions{
					LabelSelector: c.Options.CiliumDaemonSetSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to list Cilium daemonsets: %w", err)
				}
				if len(v.Items) == 0 {
					return fmt.Errorf("failed to find Cilium daemonsets with label %q in namespace %q", c.Options.CiliumDaemonSetSelector, c.Options.CiliumNamespace)
				}
				if err = c.WriteYAML(ciliumDaemonSetFileName, v); err != nil {
					return fmt.Errorf("failed to collect the Cilium daemonsets: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the Cilium Node Init daemonset",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetDaemonSet(ctx, c.Options.CiliumNamespace, ciliumNodeInitDaemonSetName, metav1.GetOptions{})
				if err != nil {
					if k8sErrors.IsNotFound(err) {
						c.logWarn("Daemonset %q not found in namespace %q - this is expected if Node Init DaemonSet is not enabled", ciliumNodeInitDaemonSetName, c.Options.CiliumNamespace)
						return nil
					}
					return fmt.Errorf("failed to collect the Cilium Node Init daemonset: %w", err)
				}
				if err := c.WriteYAML(ciliumNodeInitDaemonsetFileName, v); err != nil {
					return fmt.Errorf("could not write Cilium Node Init daemonset YAML to file: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the Cilium Envoy configuration",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetConfigMap(ctx, c.Options.CiliumNamespace, ciliumEnvoyConfigMapName, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect the Cilium Envoy configuration: %w", err)
				}
				if err := c.WriteYAML(ciliumEnvoyConfigMapFileName, v); err != nil {
					return fmt.Errorf("failed to collect the Cilium Envoy configuration: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the Cilium Envoy daemonset",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetDaemonSet(ctx, c.Options.CiliumNamespace, ciliumEnvoyDaemonSetName, metav1.GetOptions{})
				if err != nil {
					if k8sErrors.IsNotFound(err) {
						c.logWarn("Daemonset %q not found in namespace %q - this is expected if Envoy DaemonSet is not enabled", ciliumEnvoyDaemonSetName, c.Options.CiliumNamespace)
						return nil
					}
					return fmt.Errorf("failed to collect the Cilium Envoy daemonset: %w", err)
				}
				if err := c.WriteYAML(ciliumEnvoyDaemonsetFileName, v); err != nil {
					return fmt.Errorf("failed to collect the Cilium Envoy daemonset: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the Hubble daemonset",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetDaemonSet(ctx, c.Options.CiliumNamespace, hubbleDaemonSetName, metav1.GetOptions{})
				if err != nil {
					if k8sErrors.IsNotFound(err) {
						c.logDebug("Daemonset %q not found in namespace %q - this is expected in recent versions of Cilium", hubbleDaemonSetName, c.Options.CiliumNamespace)
						return nil
					}
					return fmt.Errorf("failed to collect the Hubble daemonset: %w", err)
				}
				if err := c.WriteYAML(hubbleDaemonsetFileName, v); err != nil {
					return fmt.Errorf("failed to collect the Hubble daemonset: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the Hubble Relay configuration",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetConfigMap(ctx, c.Options.CiliumNamespace, hubbleRelayConfigMapName, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect the Hubble Relay configuration: %w", err)
				}
				if err := c.WriteYAML(hubbleRelayConfigMapFileName, v); err != nil {
					return fmt.Errorf("failed to collect the Hubble Relay configuration: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the Hubble Relay deployment",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetDeployment(ctx, c.Options.CiliumNamespace, hubbleRelayDeploymentName, metav1.GetOptions{})
				if err != nil {
					if k8sErrors.IsNotFound(err) {
						c.logWarn("Deployment %q not found in namespace %q - this is expected if Hubble is not enabled", hubbleRelayDeploymentName, c.Options.CiliumNamespace)
						return nil
					}
					return fmt.Errorf("failed to collect the Hubble Relay deployment: %w", err)
				}
				if err := c.WriteYAML(hubbleRelayDeploymentFileName, v); err != nil {
					return fmt.Errorf("failed to collect the Hubble Relay deployment: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the Hubble UI deployment",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetDeployment(ctx, c.Options.CiliumNamespace, hubbleUIDeploymentName, metav1.GetOptions{})
				if err != nil {
					if k8sErrors.IsNotFound(err) {
						c.logWarn("Deployment %q not found in namespace %q - this is expected if Hubble UI is not enabled", hubbleUIDeploymentName, c.Options.CiliumNamespace)
						return nil
					}
					return fmt.Errorf("failed to collect the Hubble UI deployment: %w", err)
				}
				if err := c.WriteYAML(hubbleUIDeploymentFileName, v); err != nil {
					return fmt.Errorf("failed to collect the Hubble UI deployment: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the Hubble generate certs cronjob",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetCronJob(ctx, c.Options.CiliumNamespace, hubbleGenerateCertsCronJob, metav1.GetOptions{})
				if err != nil {
					if k8sErrors.IsNotFound(err) {
						c.logWarn("cronjob %q not found in namespace %q - this is expected if auto TLS is not enabled or if not using hubble.auto.tls.method=cronjob", hubbleGenerateCertsCronJob, c.Options.CiliumNamespace)
						return nil
					}
					return fmt.Errorf("failed to collect the Hubble generate certs cronjob: %w", err)
				}
				if err := c.WriteYAML(hubbleGenerateCertsCronJobFileName, v); err != nil {
					return fmt.Errorf("failed to collect the Hubble generate certs cronjob: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the Hubble generate certs pod logs",
			Quick:       false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.CiliumNamespace, metav1.ListOptions{
					LabelSelector: c.Options.HubbleGenerateCertsLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get logs from Hubble certgen pods")
				}
				if err := c.SubmitLogsTasks(FilterPods(p, c.NodeList), c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from Hubble certgen pods")
				}
				return nil
			},
		},
		{
			Description: "Collecting the Hubble cert-manager certificates",
			Quick:       true,
			Task: func(ctx context.Context) error {
				return c.GatherResourceUnstructured(
					ctx,
					certificate,
					hubbleCertificatesFileName,
					"hubble-relay-client-certs",
					"hubble-relay-server-certs",
					"hubble-server-certs",
				)
			},
		},
		{
			Description: "Collecting the Cilium operator deployment",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetDeployment(ctx, c.Options.CiliumOperatorNamespace, ciliumOperatorDeploymentName, metav1.GetOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect the Cilium operator deployment: %w", err)
				}
				if err := c.WriteYAML(ciliumOperatorDeploymentFileName, v); err != nil {
					return fmt.Errorf("failed to collect the Cilium operator deployment: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting the Cilium operator metrics",
			Quick:           false,
			Task: func(_ context.Context) error {
				err := c.SubmitMetricsSubtask(c.CiliumOperatorPods, defaults.OperatorContainerName, defaults.OperatorMetricsPortName)
				if err != nil {
					return fmt.Errorf("failed to collect the Cilium operator metrics: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting the clustermesh debug information, metrics and gops stats",
			Quick:           false,
			Task: func(ctx context.Context) error {
				// clustermesh-apiserver runs in the same namespace as operator
				p, err := c.Client.ListPods(ctx, c.Options.CiliumOperatorNamespace, metav1.ListOptions{LabelSelector: defaults.ClusterMeshPodSelector})
				if err != nil {
					return fmt.Errorf("failed to get the Cilium clustermesh pods: %w", err)
				}

				pods := AllPods(p)

				err = c.submitClusterMeshAPIServerDbgTasks(pods)
				if err != nil {
					return fmt.Errorf("failed to collect the Cilium clustermesh debug information: %w", err)
				}

				err = c.SubmitMetricsSubtask(pods, defaults.ClusterMeshContainerName, defaults.ClusterMeshMetricsPortName)
				if err != nil {
					return fmt.Errorf("failed to collect the Cilium clustermesh metrics: %w", err)
				}
				err = c.SubmitMetricsSubtask(pods, defaults.ClusterMeshKVStoreMeshContainerName, defaults.ClusterMeshKVStoreMeshMetricsPortName)
				if err != nil {
					return fmt.Errorf("failed to collect the Cilium clustermesh metrics: %w", err)
				}
				err = c.SubmitMetricsSubtask(pods, defaults.ClusterMeshEtcdContainerName, defaults.ClusterMeshEtcdMetricsPortName)
				if err != nil {
					return fmt.Errorf("failed to collect the Cilium clustermesh metrics: %w", err)
				}

				for container, port := range map[string]uint16{
					defaults.ClusterMeshContainerName:            ciliumdef.GopsPortApiserver,
					defaults.ClusterMeshKVStoreMeshContainerName: ciliumdef.GopsPortKVStoreMesh,
				} {
					err = c.SubmitGopsSubtasks(pods, container)
					if err != nil {
						return fmt.Errorf("failed to collect the Cilium clustermesh gops stats: %w", err)
					}

					if c.Options.Profiling {
						err = c.SubmitStreamProfilingGopsSubtasks(pods, container, port)
						if err != nil {
							return fmt.Errorf("failed to collect the Cilium clustermesh profiles: %w", err)
						}
					}
				}

				return nil
			},
		},
		{
			Description: "Collecting the 'clustermesh-apiserver' deployment",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetDeployment(ctx, c.Options.CiliumNamespace, clustermeshApiserverDeploymentName, metav1.GetOptions{})
				if err != nil {
					if k8sErrors.IsNotFound(err) {
						c.logWarn("Deployment %q not found in namespace %q - this is expected if 'clustermesh-apiserver' isn't enabled", clustermeshApiserverDeploymentName, c.Options.CiliumNamespace)
						return nil
					}
					return fmt.Errorf("failed to collect the 'clustermesh-apiserver' deployment: %w", err)
				}
				if err := c.WriteYAML(clustermeshApiserverDeploymentFileName, v); err != nil {
					return fmt.Errorf("failed to collect the 'clustermesh-apiserver' deployment: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting the CNI configuration files from Cilium pods",
			Quick:           true,
			Task: func(_ context.Context) error {
				if err := c.SubmitCniConflistSubtask(c.CiliumPods, ciliumAgentContainerName); err != nil {
					return fmt.Errorf("failed to collect CNI configuration files: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the CNI configmap",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetConfigMap(ctx, c.Options.CiliumNamespace, c.Options.CNIConfigMapName, metav1.GetOptions{})
				if err != nil && k8sErrors.IsNotFound(err) {
					c.logDebug("CNI configmap %s not found: %w", c.Options.CNIConfigMapName, err)
					return nil
				}
				if err != nil {
					return fmt.Errorf("failed to collect the CNI configuration configmap: %w", err)
				}
				if err := c.WriteYAML(cniConfigMapFileName, v); err != nil {
					return fmt.Errorf("failed to write CNI configuration configmap: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting gops stats from Cilium pods",
			Quick:           true,
			Task: func(_ context.Context) error {
				if err := c.SubmitGopsSubtasks(c.CiliumPods, ciliumAgentContainerName); err != nil {
					return fmt.Errorf("failed to collect Cilium gops: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting gops stats from Cilium-operator pods",
			Quick:           true,
			Task: func(ctx context.Context) error {
				if err := c.SubmitGopsSubtasks(c.CiliumOperatorPods, ciliumOperatorContainerName); err != nil {
					return fmt.Errorf("failed to collect cilium-operator gops stats: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting gops stats from Hubble pods",
			Quick:           true,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.CiliumNamespace, metav1.ListOptions{
					LabelSelector: c.Options.HubbleLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get Hubble pods: %w", err)
				}
				if err := c.SubmitGopsSubtasks(FilterPods(p, c.NodeList), hubbleContainerName); err != nil {
					return fmt.Errorf("failed to collect Hubble gops: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting gops stats from Hubble Relay pods",
			Quick:           true,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.CiliumNamespace, metav1.ListOptions{
					LabelSelector: c.Options.HubbleRelayLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get Hubble Relay pods: %w", err)
				}
				if err := c.SubmitGopsSubtasks(AllPods(p), hubbleRelayContainerName); err != nil {
					return fmt.Errorf("failed to collect Hubble Relay gops: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting bugtool output from Cilium pods",
			Quick:           false,
			Task: func(_ context.Context) error {
				if err := c.submitCiliumBugtoolTasks(c.CiliumPods); err != nil {
					return fmt.Errorf("failed to collect 'cilium-bugtool': %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting profiling data from Cilium Operator pods",
			Quick:           false,
			Task: func(_ context.Context) error {
				if !c.Options.Profiling {
					return nil
				}

				err := c.SubmitStreamProfilingGopsSubtasks(c.CiliumOperatorPods, ciliumOperatorContainerName, ciliumdef.GopsPortOperator)
				if err != nil {
					return fmt.Errorf("failed to collect cilium-operator profiles: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting logs from Cilium pods",
			Quick:           false,
			Task: func(_ context.Context) error {
				if err := c.SubmitLogsTasks(c.CiliumPods, c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from Cilium pods")
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting logs from crashing Cilium pods",
			Quick:           false,
			Task: func(_ context.Context) error {
				if err := c.SubmitLogsTasks(c.CiliumNotReadyPods, c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from not ready Cilium pods")
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting logs from Cilium Envoy pods",
			Quick:           false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.CiliumNamespace, metav1.ListOptions{
					LabelSelector: c.Options.CiliumEnvoyLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get logs from Cilium Envoy pods")
				}
				if err := c.SubmitLogsTasks(FilterPods(p, c.NodeList), c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from Cilium Envoy pods")
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting logs from Cilium Node Init pods",
			Quick:           false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.CiliumNamespace, metav1.ListOptions{
					LabelSelector: c.Options.CiliumNodeInitLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to list Cilium Node Init pods")
				}
				if err := c.SubmitLogsTasks(FilterPods(p, c.NodeList), c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from Cilium Node Init pods")
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting logs from Cilium operator pods",
			Quick:           false,
			Task: func(ctx context.Context) error {
				if err := c.SubmitLogsTasks(c.CiliumOperatorPods, c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from Cilium operator pods")
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting logs from 'clustermesh-apiserver' pods",
			Quick:           false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.CiliumNamespace, metav1.ListOptions{
					LabelSelector: c.Options.ClustermeshApiserverLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get logs from 'clustermesh-apiserver' pods")
				}
				if err := c.SubmitLogsTasks(AllPods(p), c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from 'clustermesh-apiserver' pods")
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting logs from Hubble pods",
			Quick:           false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.CiliumNamespace, metav1.ListOptions{
					LabelSelector: c.Options.HubbleLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get logs from Hubble pods")
				}
				if err := c.SubmitLogsTasks(FilterPods(p, c.NodeList), c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from Hubble pods")
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting logs from Hubble Relay pods",
			Quick:           false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.CiliumNamespace, metav1.ListOptions{
					LabelSelector: c.Options.HubbleRelayLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get logs from Hubble Relay pods")
				}
				if err := c.SubmitLogsTasks(AllPods(p), c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from Hubble Relay pods")
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting logs from Hubble UI pods",
			Quick:           false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.CiliumNamespace, metav1.ListOptions{
					LabelSelector: c.Options.HubbleUILabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get logs from Hubble UI pods")
				}
				if err := c.SubmitLogsTasks(AllPods(p), c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from Hubble UI pods")
				}
				return nil
			},
		},

		{
			CreatesSubtasks: true,
			Description:     "Collecting platform-specific data",
			Quick:           true,
			Task: func(ctx context.Context) error {
				f := c.Client.AutodetectFlavor(ctx)
				c.logDebug("Detected flavor %q", f.Kind)
				if err := c.submitFlavorSpecificTasks(f); err != nil {
					return fmt.Errorf("failed to collect platform-specific data: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting kvstore data",
			Quick:           true,
			Task: func(ctx context.Context) error {
				if c.CiliumConfigMap == nil {
					c.logDebug("%s ConfigMap not found, skipping kvstore dump", ciliumConfigMapName)
					return nil
				}
				// Check if kvstore is enabled, if not skip this dump.
				if v, ok := c.CiliumConfigMap.Data["kvstore"]; !ok || v != "etcd" {
					c.logDebug("KVStore not enabled, skipping kvstore dump")
					return nil
				}
				if len(c.CiliumPods) == 0 {
					return fmt.Errorf("could not find Cilium Agent Pod to run kvstore get, Cilium Pod list was empty")
				}
				for _, pod := range c.CiliumPods {
					if pod.Status.Phase == "Running" {
						return c.submitKVStoreTasks(ctx, pod.DeepCopy())
					}
				}
				return fmt.Errorf("could not find running Cilium Pod")
			},
		},
	}
	ciliumTasks = append(ciliumTasks, collectCiliumV2OrV2Alpha1Resource(c, "ciliumloadbalancerippools", "Cilium LoadBalancer IP Pools"))

	if c.Options.HubbleFlowsCount > 0 {
		ciliumTasks = append(ciliumTasks, Task{
			CreatesSubtasks: true,
			Description:     "Collecting Hubble flows from Cilium pods",
			Quick:           false,
			Task: func(ctx context.Context) error {
				if err := c.submitHubbleFlowsTasks(ctx, c.CiliumPods, ciliumAgentContainerName); err != nil {
					return fmt.Errorf("failed to collect hubble flows: %w", err)
				}
				return nil
			},
		})
	}

	// TODO(#2645): Ideally we would split ciliumTasks into
	// operator, agent, generic tasks, ..., etc
	// and only run then when feasible.
	if c.Options.CiliumNamespace != "" || c.Options.CiliumOperatorNamespace != "" {
		tasks = append(tasks, ciliumTasks...)

		serialTasks = append(serialTasks, Task{
			CreatesSubtasks: true,
			Description:     "Collecting profiling data from Cilium pods",
			Quick:           false,
			Task: func(_ context.Context) error {
				if !c.Options.Profiling {
					return nil
				}
				if err := c.SubmitProfilingGopsSubtasks(c.CiliumPods, ciliumAgentContainerName); err != nil {
					return fmt.Errorf("failed to collect profiling data from Cilium pods: %w", err)
				}
				return nil
			},
		}, Task{
			CreatesSubtasks: true,
			Description:     "Collecting tracing data from Cilium pods",
			Quick:           false,
			Task: func(_ context.Context) error {
				if !c.Options.Tracing {
					return nil
				}
				if err := c.SubmitTracingGopsSubtask(c.CiliumPods, ciliumAgentContainerName); err != nil {
					return fmt.Errorf("failed to collect tracing data from Cilium pods: %w", err)
				}
				return nil
			},
		})
	}

	tetragonTasks := []Task{
		{
			CreatesSubtasks: true,
			Description:     "Collecting logs from Tetragon pods",
			Quick:           false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.TetragonNamespace, metav1.ListOptions{
					LabelSelector: c.Options.TetragonLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get Tetragon pods: %w", err)
				}

				if err := c.SubmitLogsTasks(FilterPods(p, c.NodeList), c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from Tetragon pods")
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting logs from Tetragon operator pods",
			Quick:           false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.TetragonNamespace, metav1.ListOptions{
					LabelSelector: c.Options.TetragonOperatorLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get Tetragon operator pods: %w", err)
				}

				if err := c.SubmitLogsTasks(FilterPods(p, c.NodeList), c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from Tetragon operator pods")
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting bugtool output from Tetragon pods",
			Quick:           false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.TetragonNamespace, metav1.ListOptions{
					LabelSelector: c.Options.TetragonLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get Tetragon pods: %w", err)
				}
				if err := c.SubmitTetragonBugtoolTasks(FilterPods(p, c.NodeList),
					DefaultTetragonAgentContainerName, DefaultTetragonBugtoolPrefix,
					DefaultTetragonCLICommand); err != nil {
					return fmt.Errorf("failed to collect 'tetragon-bugtool': %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Tetragon configmap",
			Quick:       true,
			Task: func(ctx context.Context) error {
				cmName := DefaultTetragonConfigMapName
				v, err := c.Client.GetConfigMap(ctx, c.Options.TetragonNamespace, cmName, metav1.GetOptions{})
				if err != nil && k8sErrors.IsNotFound(err) {
					c.logDebug("CNI configmap %s not found: %w", cmName, err)
					return nil
				}
				if err != nil {
					return fmt.Errorf("failed to collect the Tetragon configmap: %w", err)
				}
				if err := c.WriteYAML("tetragon-configmap-<ts>.yaml", v); err != nil {
					return fmt.Errorf("failed to write the Tetragon configmap: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting Tetragon PodInfo custom resources",
			Quick:           true,
			Task: func(ctx context.Context) error {
				return c.GatherResourceUnstructured(
					ctx,
					schema.GroupVersionResource{
						Group:    "cilium.io",
						Resource: "podinfo",
						Version:  "v1alpha1",
					},
					DefaultTetragonPodInfo,
				)
			},
		},
		{
			CreatesSubtasks: false,
			Description:     "Collecting Tetragon tracing policies",
			Quick:           true,
			Task: func(ctx context.Context) error {
				return c.GatherResourceUnstructured(
					ctx,
					schema.GroupVersionResource{
						Group:    "cilium.io",
						Resource: "tracingpolicies",
						Version:  "v1alpha1",
					},
					DefaultTetragonTracingPolicy,
				)
			},
		},
		{
			CreatesSubtasks: false,
			Description:     "Collecting Tetragon namespaced tracing policies",
			Quick:           true,
			Task: func(ctx context.Context) error {
				return c.GatherResourceUnstructured(
					ctx,
					schema.GroupVersionResource{
						Group:    "cilium.io",
						Resource: "tracingpoliciesnamespaced",
						Version:  "v1alpha1",
					},
					DefaultTetragonTracingPolicyNamespaced,
				)
			},
		},
	}

	tasks = append(tasks, tetragonTasks...)

	for _, selector := range c.Options.ExtraLabelSelectors {
		tasks = append(tasks, Task{
			CreatesSubtasks: true,
			Description:     fmt.Sprintf("Collecting logs from extra pods %q", selector),
			Quick:           false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, metav1.NamespaceAll, metav1.ListOptions{
					LabelSelector: selector,
				})
				if err != nil {
					return fmt.Errorf("failed to get logs from pods matching selector %q", selector)
				}
				if err := c.SubmitLogsTasks(FilterPods(p, c.NodeList), c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from pods matching selector %q", selector)
				}
				return nil
			},
		})
	}

	helmTasks := []Task{
		{
			CreatesSubtasks: true,
			Description:     "Collecting Helm metadata from the Cilium release",
			Quick:           true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetHelmMetadata(ctx, c.Options.CiliumHelmReleaseName, c.Options.CiliumNamespace)
				if err != nil {
					return fmt.Errorf("failed to get the helm metadata from the release: %w", err)
				}
				if err := c.WriteString(ciliumHelmMetadataFileName, v); err != nil {
					return fmt.Errorf("failed to write the helm metadata to the file: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting Helm metadata from the Tetragon release",
			Quick:           true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetHelmMetadata(ctx, c.Options.TetragonHelmReleaseName, c.Options.TetragonNamespace)
				if err != nil {
					return fmt.Errorf("failed to get the helm metadata from the release: %w", err)
				}
				if err := c.WriteString(tetragonHelmMetadataFileName, v); err != nil {
					return fmt.Errorf("failed to write the helm metadata to the file: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting Helm values from the Cilium release",
			Quick:           true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetHelmValues(ctx, c.Options.CiliumHelmReleaseName, c.Options.CiliumNamespace)
				if err != nil {
					return fmt.Errorf("failed to get the helm values from the release: %w", err)
				}
				if err := c.WriteString(ciliumHelmValuesFileName, v); err != nil {
					return fmt.Errorf("failed to write the helm values to the file: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting Helm values from the Tetragon release",
			Quick:           true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetHelmValues(ctx, c.Options.TetragonHelmReleaseName, c.Options.TetragonNamespace)
				if err != nil {
					return fmt.Errorf("failed to get the helm values from the release: %w", err)
				}
				if err := c.WriteString(tetragonHelmValuesFileName, v); err != nil {
					return fmt.Errorf("failed to write the helm values to the file: %w", err)
				}
				return nil
			},
		},
	}

	tasks = append(tasks, helmTasks...)
	// Append tasks added by AddTasks.
	tasks = append(tasks, c.additionalTasks...)
	if c.FeatureSet[features.AuthSpiffe].Enabled {
		tasks = append(tasks, c.getSPIRETasks()...)
	}
	if c.FeatureSet[features.GatewayAPI].Enabled {
		tasks = append(tasks, c.getGatewayAPITasks()...)
	}
	if c.FeatureSet[features.EnableEnvoyConfig].Enabled {
		tasks = append(tasks, c.getEnvoyConfigTasks()...)
	}
	if c.FeatureSet[features.BGPControlPlane].Enabled {
		tasks = append(tasks, c.getBGPControlPlaneTasks()...)
	}

	// First, run each serial task in its own workerpool.
	var r []workerpool.Task
	for i, t := range serialTasks {
		t := t
		if c.shouldSkipTask(t) {
			c.logDebug("Skipping %q", t.Description)
			continue
		}

		var wg sync.WaitGroup
		if t.CreatesSubtasks {
			wg.Add(1)
		}

		// Adjust the worker count to make enough headroom for tasks that submit sub-tasks.
		// This is necessary because 'Submit' is blocking.
		wc := max(1, c.Options.WorkerCount)
		if t.CreatesSubtasks {
			wc++
		}
		c.Pool = workerpool.New(wc)

		// Add the serial task to the worker pool.
		if err := c.Pool.Submit(fmt.Sprintf("[%d] %s", len(tasks)+i, t.Description), func(ctx context.Context) error {
			if t.CreatesSubtasks {
				defer wg.Done()
			}
			c.logTask(t.Description)
			defer c.logDebug("Finished %q", t.Description)
			return t.Task(ctx)
		}); err != nil {
			return fmt.Errorf("failed to submit task to the worker pool: %w", err)
		}

		// Wait for the subtasks to be submitted and then call 'Drain' to wait for them to finish.
		wg.Wait()
		results, err := c.Pool.Drain()
		if err != nil {
			return fmt.Errorf("failed to drain the serial tasks worker pool: %w", err)
		}
		// Close the worker Pool.
		if err := c.Pool.Close(); err != nil {
			return fmt.Errorf("failed to close the serial tasks worker pool: %w", err)
		}

		r = append(r, results...)
	}

	// Adjust the worker count to make enough headroom for tasks that submit sub-tasks.
	// This is necessary because 'Submit' is blocking.
	wc := max(2, c.Options.WorkerCount)

	c.Pool = workerpool.New(wc)
	c.logDebug("Using %d workers (requested: %d)", wc, c.Options.WorkerCount)

	// Add the tasks to the worker pool.
	for i, t := range tasks {
		t := t
		if c.shouldSkipTask(t) {
			c.logDebug("Skipping %q", t.Description)
			continue
		}
		if t.CreatesSubtasks {
			c.subtasksWg.Add(1)
		}
		if err := c.Pool.Submit(fmt.Sprintf("[%d] %s", i, t.Description), func(ctx context.Context) error {
			if t.CreatesSubtasks {
				defer c.subtasksWg.Done()
			}
			c.logTask(t.Description)
			defer c.logDebug("Finished %q", t.Description)
			return t.Task(ctx)
		}); err != nil {
			return fmt.Errorf("failed to submit task to the worker pool: %w", err)
		}

		if t.CreatesSubtasks {
			c.subtasksWg.Wait()
		}
	}

	// Wait for all tasks to finish.
	results, err := c.Pool.Drain()
	if err != nil {
		return fmt.Errorf("failed to drain the worker pool: %w", err)
	}
	r = append(r, results...)
	// Close the worker Pool.
	if err := c.Pool.Close(); err != nil {
		return fmt.Errorf("failed to close the worker pool: %w", err)
	}

	loggedStart := false
	// Check if any errors occurred and warn the user.
	for _, res := range r {
		if err := res.Err(); err != nil {
			if !loggedStart {
				c.logWarn("The following tasks failed, the sysdump may be incomplete:")
				loggedStart = true
			}
			c.logWarn("%s: %v", res.String(), err)
		}
	}

	if loggedStart {
		c.logWarn("Please note that depending on your Cilium version and installation options, this may be expected")
	}

	c.teardownLogging()

	// Create the zip file in the current directory.
	c.log("🗳 Compiling sysdump")
	f := c.replaceTimestamp(c.Options.OutputFileName) + ".zip"
	if err := zipDirectory(c.sysdumpDir, f); err != nil {
		return fmt.Errorf("failed to create zip file: %w", err)
	}
	c.log("✅ The sysdump has been saved to %s", f)

	// Try to remove the temporary directory.
	c.logDebug("Removing the temporary directory %s", c.sysdumpDir)
	if err := os.RemoveAll(c.sysdumpDir); err != nil {
		c.logWarn("failed to remove temporary directory %s: %v", c.sysdumpDir, err)
	}
	return nil
}

func (c *Collector) getSPIRETasks() []Task {
	return []Task{
		{
			CreatesSubtasks: true,
			Description:     "Collecting logs from Cilium SPIRE server pods",
			Quick:           false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.CiliumSPIRENamespace, metav1.ListOptions{
					LabelSelector: c.Options.CiliumSPIREServerLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get logs from Cilium SPIRE server pods")
				}
				if err := c.SubmitLogsTasks(AllPods(p), c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from Cilium SPIRE server pods")
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting logs from Cilium SPIRE agent pods",
			Quick:           false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.CiliumSPIRENamespace, metav1.ListOptions{
					LabelSelector: c.Options.CiliumSPIREAgentLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get logs from Cilium SPIRE agent pods")
				}
				if err := c.SubmitLogsTasks(FilterPods(p, c.NodeList), c.Options.LogsSinceTime, c.Options.LogsLimitBytes); err != nil {
					return fmt.Errorf("failed to collect logs from Cilium SPIRE agent pods")
				}
				return nil
			},
		},
		{
			Description: "Collecting the Cilium SPIRE server statefulset",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetStatefulSet(ctx, c.Options.CiliumSPIRENamespace, ciliumSPIREServerStatefulSetName, metav1.GetOptions{})
				if err != nil {
					if k8sErrors.IsNotFound(err) {
						c.logWarn("StatefulSet %q not found in namespace %q - this is expected if SPIRE installation is not enabled", ciliumSPIREServerStatefulSetName, c.Options.CiliumSPIRENamespace)
						return nil
					}
					return fmt.Errorf("failed to collect the Cilium SPIRE server statefulset: %w", err)
				}
				if err := c.WriteYAML(ciliumSPIREServerStatefulSetFileName, v); err != nil {
					return fmt.Errorf("failed to collect the Cilium SPIRE server statefulset: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the Cilium SPIRE agent daemonset",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetDaemonSet(ctx, c.Options.CiliumSPIRENamespace, ciliumSPIREAgentDaemonSetName, metav1.GetOptions{})
				if err != nil {
					if k8sErrors.IsNotFound(err) {
						c.logWarn("Daemonset %q not found in namespace %q - this is expected if SPIRE installation is not enabled", ciliumSPIREAgentDaemonSetName, c.Options.CiliumSPIRENamespace)
						return nil
					}
					return fmt.Errorf("failed to collect the Cilium SPIRE agent daemonset: %w", err)
				}
				if err := c.WriteYAML(ciliumSPIREAgentDaemonsetFileName, v); err != nil {
					return fmt.Errorf("failed to collect the Cilium SPIRE agent daemonset: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the Cilium SPIRE agent configuration",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetConfigMap(ctx, c.Options.CiliumSPIRENamespace, ciliumSPIREAgentConfigMapName, metav1.GetOptions{})
				if err != nil {
					if k8sErrors.IsNotFound(err) {
						c.logWarn("ConfigMap %q not found in namespace %q - this is expected if SPIRE installation is not enabled", ciliumSPIREAgentConfigMapName, c.Options.CiliumSPIRENamespace)
						return nil
					}
					return fmt.Errorf("failed to collect the Cilium SPIRE agent configuration: %w", err)
				}
				if err := c.WriteYAML(ciliumSPIREAgentConfigMapFileName, v); err != nil {
					return fmt.Errorf("failed to collect the Cilium SPIRE agent configuration: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting the Cilium SPIRE server configuration",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.GetConfigMap(ctx, c.Options.CiliumSPIRENamespace, ciliumSPIREServerConfigMapName, metav1.GetOptions{})
				if err != nil {
					if k8sErrors.IsNotFound(err) {
						c.logWarn("ConfigMap %q not found in namespace %q - this is expected if SPIRE installation is not enabled", ciliumSPIREServerConfigMapName, c.Options.CiliumSPIRENamespace)
						return nil
					}
					return fmt.Errorf("failed to collect the Cilium SPIRE server configuration: %w", err)
				}
				if err := c.WriteYAML(ciliumSPIREServerConfigMapFileName, v); err != nil {
					return fmt.Errorf("failed to collect the Cilium SPIRE server configuration: %w", err)
				}
				return nil
			},
		},
		{
			CreatesSubtasks: true,
			Description:     "Collecting the Cilium SPIRE server identity entries",
			Quick:           false,
			Task: func(ctx context.Context) error {
				p, err := c.Client.ListPods(ctx, c.Options.CiliumSPIRENamespace, metav1.ListOptions{
					LabelSelector: c.Options.CiliumSPIREServerLabelSelector,
				})
				if err != nil {
					return fmt.Errorf("failed to get identity entries from Cilium SPIRE server pods")
				}
				if err := c.submitSpireEntriesTasks(FilterPods(p, c.NodeList)); err != nil {
					return fmt.Errorf("failed to collect identity entries from Cilium SPIRE server pods")
				}
				return nil
			},
		},
	}
}

func (c *Collector) getGatewayAPITasks() []Task {
	return []Task{
		{
			Description: "Collecting GatewayClass entries",
			Quick:       true,
			Task: func(ctx context.Context) error {
				n := corev1.NamespaceAll
				v, err := c.Client.ListUnstructured(ctx, gatewayClass, &n, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect GatewayClass entries: %w", err)
				}
				if err := c.WriteYAML(gatewayClassesFileName, v); err != nil {
					return fmt.Errorf("failed to collect GatewayClass entries: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting Gateway entries",
			Quick:       true,
			Task: func(ctx context.Context) error {
				n := corev1.NamespaceAll
				v, err := c.Client.ListUnstructured(ctx, gateway, &n, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Gateway entries: %w", err)
				}
				if err := c.WriteYAML(gatewaysFileName, v); err != nil {
					return fmt.Errorf("failed to collect Gateway entries: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting ReferenceGrant entries",
			Quick:       true,
			Task: func(ctx context.Context) error {
				n := corev1.NamespaceAll
				v, err := c.Client.ListUnstructured(ctx, referenceGrant, &n, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect ReferenceGrant entries: %w", err)
				}
				if err := c.WriteYAML(referenceGrantsFileName, v); err != nil {
					return fmt.Errorf("failed to collect ReferenceGrant entries: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting HTTPRoute entries",
			Quick:       true,
			Task: func(ctx context.Context) error {
				n := corev1.NamespaceAll
				v, err := c.Client.ListUnstructured(ctx, httpRoute, &n, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect HTTPRoute entries: %w", err)
				}
				if err := c.WriteYAML(httpRoutesFileName, v); err != nil {
					return fmt.Errorf("failed to collect HTTPRoute entries: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting TLSRoute entries",
			Quick:       true,
			Task: func(ctx context.Context) error {
				n := corev1.NamespaceAll
				v, err := c.Client.ListUnstructured(ctx, tlsRoute, &n, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect TLSRoute entries: %w", err)
				}
				if err := c.WriteYAML(tlsRoutesFileName, v); err != nil {
					return fmt.Errorf("failed to collect TLSRoute entries: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting GRPCRoute entries",
			Quick:       true,
			Task: func(ctx context.Context) error {
				n := corev1.NamespaceAll
				v, err := c.Client.ListUnstructured(ctx, grpcRoute, &n, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect GRPCRoute entries: %w", err)
				}
				if err := c.WriteYAML(grpcRoutesFileName, v); err != nil {
					return fmt.Errorf("failed to collect GRPCRoute entries: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting TCPRoute entries",
			Quick:       true,
			Task: func(ctx context.Context) error {
				n := corev1.NamespaceAll
				v, err := c.Client.ListUnstructured(ctx, tcpRoute, &n, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect TCPRoute entries: %w", err)
				}
				if err := c.WriteYAML(tcpRoutesFileName, v); err != nil {
					return fmt.Errorf("failed to collect TCPRoute entries: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting UDPRoute entries",
			Quick:       true,
			Task: func(ctx context.Context) error {
				n := corev1.NamespaceAll
				v, err := c.Client.ListUnstructured(ctx, udpRoute, &n, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect UDPRoute entries: %w", err)
				}
				if err := c.WriteYAML(udpRoutesFileName, v); err != nil {
					return fmt.Errorf("failed to collect UDPRoute entries: %w", err)
				}
				return nil
			},
		},
	}
}

func (c *Collector) getEnvoyConfigTasks() []Task {
	return []Task{
		{
			Description: "Collecting CiliumClusterwideEnvoyConfigs",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumClusterwideEnvoyConfigs(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect CiliumClusterwideEnvoyConfigs: %w", err)
				}
				if err := c.WriteYAML(ciliumClusterwideEnvoyConfigsFileName, v); err != nil {
					return fmt.Errorf("failed to collect CiliumClusterwideEnvoyConfigs: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Collecting CiliumEnvoyConfigs",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumEnvoyConfigs(ctx, corev1.NamespaceAll, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect CiliumEnvoyConfigs: %w", err)
				}
				if err := c.WriteYAML(ciliumEnvoyConfigsFileName, v); err != nil {
					return fmt.Errorf("failed to collect CiliumEnvoyConfigs: %w", err)
				}
				return nil
			},
		},
	}
}

func (c *Collector) getBGPControlPlaneTasks() []Task {
	return []Task{
		// BGPv1 resource
		{
			Description: "Collecting Cilium BGP Peering Policies",
			Quick:       true,
			Task: func(ctx context.Context) error {
				v, err := c.Client.ListCiliumBGPPeeringPolicies(ctx, metav1.ListOptions{})
				if err != nil {
					return fmt.Errorf("failed to collect Cilium BGP Peering policies: %w", err)
				}
				if err := c.WriteYAML(ciliumBPGPeeringPoliciesFileName, v); err != nil {
					return fmt.Errorf("failed to collect Cilium BGP Peering policies: %w", err)
				}
				return nil
			},
		},
		// BGPv2 resources - can be either v2 or v2alpha1 version
		collectCiliumV2OrV2Alpha1Resource(c, "ciliumbgpclusterconfigs", "Cilium BGP Cluster Configs"),
		collectCiliumV2OrV2Alpha1Resource(c, "ciliumbgppeerconfigs", "Cilium BGP Peer Configs"),
		collectCiliumV2OrV2Alpha1Resource(c, "ciliumbgpadvertisements", "Cilium BGP Advertisements"),
		collectCiliumV2OrV2Alpha1Resource(c, "ciliumbgpnodeconfigs", "Cilium BGP Node Configs"),
		collectCiliumV2OrV2Alpha1Resource(c, "ciliumbgpnodeconfigoverrides", "Cilium BGP Node Config Overrides"),
	}
}

// collects objects of v2 (primarily) or v2alpha1 (fallback) version of a cilium.io resource
func collectCiliumV2OrV2Alpha1Resource(collector *Collector, resource, title string) Task {
	return Task{
		Description: fmt.Sprintf("Collecting %s", title),
		Quick:       true,
		Task: func(ctx context.Context) error {
			gvr := schema.GroupVersionResource{
				Group:    "cilium.io",
				Resource: resource,
				Version:  "v2",
			}
			n := corev1.NamespaceAll
			// try to collect the v2 version first
			v, err := collector.Client.ListUnstructured(ctx, gvr, &n, metav1.ListOptions{})
			if err != nil && k8sErrors.IsNotFound(err) {
				// if the v2 version does not exist, try to collect the v2alpha1 version
				gvr.Version = "v2alpha1"
				v, err = collector.Client.ListUnstructured(ctx, gvr, &n, metav1.ListOptions{})
			}
			if err != nil {
				return fmt.Errorf("failed to collect %s: %w", title, err)
			}
			if err := collector.WriteYAML(fmt.Sprintf(k8sResourceFileName, resource), v); err != nil {
				return fmt.Errorf("failed to collect %s: %w", title, err)
			}
			return nil
		},
	}
}

func (c *Collector) log(msg string, args ...any) {
	fmt.Fprintf(c.logWriter, msg+"\n", args...)
}

func (c *Collector) logDebug(msg string, args ...any) {
	if c.Options.Debug {
		c.log("🩺 "+msg, args...)
	}
}

func (c *Collector) logTask(msg string, args ...any) {
	c.log("🔍 "+msg, args...)
}

func (c *Collector) logWarn(msg string, args ...any) {
	c.log("⚠️ "+msg, args...)
}

func (c *Collector) shouldSkipTask(t Task) bool {
	return c.Options.Quick && !t.Quick
}

func (c *Collector) SubmitTetragonBugtoolTasks(pods []*corev1.Pod, tetragonAgentContainerName,
	tetragonBugtoolPrefix, tetragonCLICommand string) error {
	for _, p := range pods {
		p := p
		workerID := fmt.Sprintf("%s-%s", tetragonBugtoolPrefix, p.Name)
		if err := c.Pool.Submit(workerID, func(ctx context.Context) error {
			p, containerName, cleanupFunc, err := c.ensureExecTarget(ctx, p, tetragonAgentContainerName)
			if err != nil {
				return fmt.Errorf("failed to pick exec target: %w", err)
			}
			defer func() {
				err := cleanupFunc(ctx)
				if err != nil {
					c.logWarn("Failed to clean up exec target: %v", err)
				}
			}()

			tarGzFile := fmt.Sprintf("%s-%s.tar.gz", tetragonBugtoolPrefix, time.Now().Format(timeFormat))
			// Run 'tetra bugtool' in the pod.
			command := []string{tetragonCLICommand, "bugtool", "--out", tarGzFile}

			c.logDebug("Executing 'tetragon-bugtool' command: %v", command)
			_, e, err := c.Client.ExecInPodWithStderr(ctx, p.Namespace, p.Name, containerName, command)
			if err != nil {
				return fmt.Errorf("failed to collect 'tetragon-bugtool' output for %q in namespace %q: %w: %s", p.Name, p.Namespace, err, e.String())
			}

			// Dump the resulting file's contents to the temporary directory.
			f := c.AbsoluteTempPath(fmt.Sprintf("%s-%s-<ts>.tar.gz", tetragonBugtoolPrefix, p.Name))
			err = c.Client.CopyFromPod(ctx, p.Namespace, p.Name, containerName, tarGzFile, f, c.Options.CopyRetryLimit)
			if err != nil {
				return fmt.Errorf("failed to collect 'tetragon-bugtool' output for %q: %w", p.Name, err)
			}
			if err := untar(f, strings.ReplaceAll(f, ".tar.gz", "")); err != nil {
				c.logWarn("Failed to unarchive 'tetragon-bugtool' output for %q: %v", p.Name, err)
				return nil
			}
			// Remove the file we've copied from the pod.
			if err := os.Remove(f); err != nil {
				c.logWarn("Failed to remove original 'tetragon-bugtool' file: %v", err)
				return nil
			}
			if _, err = c.Client.ExecInPod(ctx, p.Namespace, p.Name, containerName, []string{rmCommand, tarGzFile}); err != nil {
				c.logWarn("Failed to delete 'tetragon-bugtool' output from pod %q in namespace %q: %w", p.Name, p.Namespace, err)
				return nil
			}
			return nil
		}); err != nil {
			return fmt.Errorf("failed to submit 'tetragon-bugtool' task for %q: %w", p.Name, err)
		}
	}
	return nil
}

// removeTopDirectory removes the top directory from a relative file path
// (e.g. "a/b/c" => "b/c"). Don't pass an absolute path. It doesn't do what
// you think it does.
func removeTopDirectory(path string) (string, error) {
	// file separator hardcoded because sysdump always created on Linux OS
	index := strings.IndexByte(path, '/')
	if index < 0 {
		return "", fmt.Errorf("invalid path %q", path)
	}
	return path[index+1:], nil
}

func untar(src string, dst string) error {
	reader, err := os.Open(src)
	if err != nil {
		return err
	}
	defer reader.Close()
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		// Cilium and Tetragon bugtool tar files don't contain headers for
		// directories, so create a directory for each file instead.
		if header.Typeflag != tar.TypeReg {
			continue
		}
		name, err := removeTopDirectory(header.Name)
		if err != nil {
			return err
		}
		filename := filepath.Join(dst, name)
		directory := filepath.Dir(filename)
		if err := os.MkdirAll(directory, 0755); err != nil {
			return err
		}
		f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
		if err != nil {
			return err
		}
		if err = copyN(f, tr, 1024); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
}

// copyN copies from src to dst n bytes at a time to avoid this lint error:
// G110: Potential DoS vulnerability via decompression bomb (gosec)
func copyN(dst io.Writer, src io.Reader, n int64) error {
	for {
		_, err := io.CopyN(dst, src, n)
		if errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
	}
}

func zipDirectory(src string, dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	writer := zip.NewWriter(f)
	defer writer.Close()
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(fi)
		if err != nil {
			return err
		}
		header.Name, err = filepath.Rel(filepath.Dir(src), path)
		if err != nil {
			return err
		}
		header.Method = zip.Deflate
		dstFile, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()
		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func (c *Collector) submitCiliumBugtoolTasks(pods []*corev1.Pod) error {
	for _, p := range pods {
		p := p
		if err := c.Pool.Submit("cilium-bugtool-"+p.Name, func(ctx context.Context) error {
			p, containerName, cleanupFunc, err := c.ensureExecTarget(ctx, p, ciliumAgentContainerName)
			if err != nil {
				return fmt.Errorf("failed to pick exec target: %w", err)
			}
			defer func() {
				err := cleanupFunc(ctx)
				if err != nil {
					c.logWarn("Failed to clean up exec target: %v", err)
				}
			}()

			// Default flags for cilium-bugtool
			bugtoolFlags := []string{"--archiveType=gz", "--exclude-object-files"}
			// Additional flags
			bugtoolFlags = append(bugtoolFlags, c.Options.CiliumBugtoolFlags...)

			// Run 'cilium-bugtool' in the pod.
			command := append([]string{ciliumBugtoolCommand}, bugtoolFlags...)

			c.logDebug("Executing cilium-bugtool command: %v", command)
			o, e, err := c.Client.ExecInPodWithStderr(ctx, p.Namespace, p.Name, containerName, command)
			if err != nil {
				return fmt.Errorf("failed to collect 'cilium-bugtool' output for %q in namespace %q: %w: %s", p.Name, p.Namespace, err, e.String())
			}
			// Capture the path to the resulting archive.
			outString := o.String() + e.String()
			m := ciliumBugtoolFileNameRegex.FindStringSubmatch(outString)
			if len(m) != 2 || len(m[1]) == 0 {
				return fmt.Errorf("failed to collect 'cilium-bugtool' output for %q in namespace %q: output doesn't contain archive name: %s", p.Name, p.Namespace, outString)
			}
			tarGzFile := m[1]

			// Dump the resulting file's contents to the temporary directory.
			f := c.AbsoluteTempPath(fmt.Sprintf(ciliumBugtoolFileName, p.Name))
			err = c.Client.CopyFromPod(ctx, p.Namespace, p.Name, containerName, tarGzFile, f, c.Options.CopyRetryLimit)
			if err != nil {
				return fmt.Errorf("failed to collect 'cilium-bugtool' output for %q: %w", p.Name, err)
			}
			// Untar the resulting file.
			if err := untar(f, strings.ReplaceAll(f, ".tar.gz", "")); err != nil {
				c.logWarn("Failed to unarchive 'cilium-bugtool' output for %q: %v", p.Name, err)
				return nil
			}
			// Remove the file we've copied from the pod.
			if err := os.Remove(f); err != nil {
				c.logWarn("Failed to remove original 'cilium-bugtool' file: %v", err)
				return nil
			}
			// Remove the file from the pod.
			if _, err = c.Client.ExecInPod(ctx, p.Namespace, p.Name, containerName, []string{rmCommand, tarGzFile}); err != nil {
				c.logWarn("Failed to delete 'cilium-bugtool' output from pod %q in namespace %q: %w", p.Name, p.Namespace, err)
				return nil
			}
			return nil
		}); err != nil {
			return fmt.Errorf("failed to submit 'cilium-bugtool' task for %q: %w", p.Name, err)
		}
	}
	return nil
}

func (c *Collector) submitHubbleFlowsTasks(_ context.Context, pods []*corev1.Pod, containerName string) error {
	for _, p := range pods {
		p := p
		if err := c.Pool.Submit("hubble-flows-"+p.Name, func(ctx context.Context) error {
			if err := c.WithFileSink(fmt.Sprintf(hubbleFlowsFileName, p.Name), func(stdout io.Writer) error {
				return c.WithFileSink(fmt.Sprintf(hubbleObserveFileName, p.Name), func(stderr io.Writer) error {
					cctx, cancel := context.WithTimeout(ctx, c.Options.HubbleFlowsTimeout)
					defer cancel()

					return c.Client.ExecInPodWithWriters(cctx, nil, p.Namespace, p.Name, containerName, []string{
						"hubble", "observe", "--last", strconv.FormatInt(c.Options.HubbleFlowsCount, 10), "--debug", "-o", "jsonpb",
					}, stdout, stderr)

				})
			}); err != nil {
				return fmt.Errorf("failed to collect hubble flows for %q in namespace %q: %w", p.Name, p.Namespace, err)
			}

			c.logDebug("Finished collecting hubble flows from %s/%s", p.Namespace, p.Name)
			return nil
		}); err != nil {
			return fmt.Errorf("failed to submit 'hubble-flows' task for %q: %w", p.Name, err)
		}
	}
	return nil
}

func (c *Collector) submitSpireEntriesTasks(pods []*corev1.Pod) error {
	for _, p := range pods {
		p := p
		if err := c.Pool.Submit("spire-entries-"+p.Name, func(ctx context.Context) error {
			p, containerName, cleanupFunc, err := c.ensureExecTarget(ctx, p, spireServerContainerName)
			if err != nil {
				return fmt.Errorf("failed to pick exec target: %w", err)
			}
			defer func() {
				err := cleanupFunc(ctx)
				if err != nil {
					c.logWarn("Failed to clean up exec target: %v", err)
				}
			}()

			command := []string{"/opt/spire/bin/spire-server", "entry", "show", "-output", "json"}
			if err := c.WithFileSink(fmt.Sprintf(ciliumSPIREServerEntriesFileName, p.Name), func(out io.Writer) error {
				return c.Client.ExecInPodWithWriters(ctx, nil, p.Namespace, p.Name, containerName, command, out, k8s.StderrAsError)
			}); err != nil {
				return fmt.Errorf("failed to collect 'spire-server' output for %q in namespace %q: %w", p.Name, p.Namespace, err)
			}

			return nil
		}); err != nil {
			return fmt.Errorf("failed to submit SPIRE entries task for %q: %w", p.Name, err)
		}
	}
	return nil
}

func extractGopsPID(output string) (string, error) {
	for entry := range strings.SplitSeq(output, "\n") {
		match := gopsRegexp.FindStringSubmatch(entry)
		if len(match) > 0 {
			result := make(map[string]string)
			for i, name := range gopsRegexp.SubexpNames() {
				if i != 0 && name != "" {
					result[name] = match[i]
				}
			}
			return result["pid"], nil
		}
	}

	return "", fmt.Errorf("failed to extract pid from output: %q", output)
}

func extractGopsProfileData(output string) (string, error) {
	splited := strings.Split(output, "\n")
	prefix := "saved to: "

	for _, str := range splited {
		if strings.Contains(str, prefix) {
			return strings.TrimSpace(strings.Split(str, prefix)[1]), nil
		}
	}
	return "", fmt.Errorf("Unable to find output file: %s", output)

}

func (c *Collector) SubmitCniConflistSubtask(pods []*corev1.Pod, containerName string) error {
	for _, p := range pods {
		p := p
		if err := c.Pool.Submit(fmt.Sprintf("cniconflist-%s", p.GetName()), func(ctx context.Context) error {
			outputStr, err := c.Client.ExecInPod(ctx, p.GetNamespace(), p.GetName(), containerName, []string{
				lsCommand,
				c.Options.CNIConfigDirectory,
			})
			if err != nil {
				return err
			}
			for cniFileName := range strings.SplitSeq(strings.TrimSpace(outputStr.String()), "\n") {
				cniConfigPath := path.Join(c.Options.CNIConfigDirectory, cniFileName)
				if err := c.WithFileSink(fmt.Sprintf(cniConfigFileName, cniFileName, p.GetName()), func(out io.Writer) error {
					return c.Client.ExecInPodWithWriters(ctx, nil, p.GetNamespace(), p.GetName(), containerName, []string{
						catCommand,
						cniConfigPath,
					}, out, k8s.StderrAsError)
				}); err != nil {
					return fmt.Errorf("failed to copy CNI config file %q from directory %q: %w", cniFileName, c.Options.CNIConfigDirectory, err)
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("failed to submit cniconflist task for %q: %w", p.Name, err)
		}
	}
	return nil
}

func (c *Collector) getGopsPID(ctx context.Context, pod *corev1.Pod, containerName string) (string, error) {
	// Run 'gops' on the pod.
	gopsOutput, err := c.Client.ExecInPod(ctx, pod.Namespace, pod.Name, containerName, []string{
		gopsCommand,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list processes %q (%q) in namespace %q: %w", pod.Name, containerName, pod.Namespace, err)
	}
	agentPID := gopsPID
	if c.Options.DetectGopsPID {
		var err error
		outputStr := gopsOutput.String()
		agentPID, err = extractGopsPID(outputStr)
		if err != nil {
			return "", err
		}
	}
	return agentPID, nil
}

// SubmitGopsSubtasks submits tasks to collect gops statistics from pods.
func (c *Collector) SubmitGopsSubtasks(pods []*corev1.Pod, containerName string) error {
	for _, p := range pods {
		p := p

		if !podIsRunningAndHasContainer(p, containerName) {
			continue
		}

		for _, g := range gopsStats {
			g := g
			if err := c.Pool.Submit(fmt.Sprintf("gops-%s-%s", p.Name, g), func(ctx context.Context) error {
				agentPID, err := c.getGopsPID(ctx, p, containerName)
				if err != nil {
					return err
				}

				if err := c.WithFileSink(fmt.Sprintf(gopsFileName, p.Name, containerName, g), func(out io.Writer) error {
					return c.Client.ExecInPodWithWriters(ctx, nil, p.Namespace, p.Name, containerName, []string{
						gopsCommand,
						g,
						agentPID,
					}, out, k8s.StderrAsError)
				}); err != nil {
					return fmt.Errorf("failed to collect gops for %q (%q) in namespace %q: %w", p.Name, containerName, p.Namespace, err)
				}

				return nil
			}); err != nil {
				return fmt.Errorf("failed to submit %s gops task for %q: %w", g, p.Name, err)
			}
		}
	}
	return nil
}

// SubmitProfilingGopsSubtasks submits tasks to collect profiling data from pods.
func (c *Collector) SubmitProfilingGopsSubtasks(pods []*corev1.Pod, containerName string) error {
	for _, p := range pods {
		p := p
		for g := range gopsProfiling {
			g := g
			if err := c.Pool.Submit(fmt.Sprintf("gops-%s-%s", p.Name, g), func(ctx context.Context) error {
				agentPID, err := c.getGopsPID(ctx, p, containerName)
				if err != nil {
					return err
				}
				o, err := c.Client.ExecInPod(ctx, p.Namespace, p.Name, containerName, []string{
					gopsCommand,
					g,
					agentPID,
				})
				if err != nil {
					return fmt.Errorf("failed to collect gops profiling for %q (%q) in namespace %q: %w", p.Name, containerName, p.Namespace, err)
				}
				filePath, err := extractGopsProfileData(o.String())
				if err != nil {
					return fmt.Errorf("failed to collect gops profiling for %q (%q) in namespace %q: %w", p.Name, containerName, p.Namespace, err)
				}
				f := c.AbsoluteTempPath(fmt.Sprintf("%s-%s-<ts>.pprof", p.Name, g))
				err = c.Client.CopyFromPod(ctx, p.Namespace, p.Name, containerName, filePath, f, c.Options.CopyRetryLimit)
				if err != nil {
					return fmt.Errorf("failed to collect gops profiling output for %q: %w", p.Name, err)
				}
				if _, err = c.Client.ExecInPod(ctx, p.Namespace, p.Name, containerName, []string{rmCommand, filePath}); err != nil {
					c.logWarn("failed to delete profiling output from pod %q in namespace %q: %w", p.Name, p.Namespace, err)
					return nil
				}
				return nil
			}); err != nil {
				return fmt.Errorf("failed to submit %s gops task for %q: %w", g, p.Name, err)
			}
		}
	}
	return nil
}

// SubmitStreamProfilingGopsSubtasks submits tasks to collect profiling data from pods. Differently
// from SubmitProfilingGopsSubtasks, it directly retrieves the profiles from the remote gops server,
// rather than calling the gops client binary. This allows to retrieve the profiles from distroless
// containers as well, as it does not depend on any shell tools.
func (c *Collector) SubmitStreamProfilingGopsSubtasks(pods []*corev1.Pod, containerName string, port uint16) error {
	for _, p := range pods {
		p := p

		if !podIsRunningAndHasContainer(p, containerName) {
			continue
		}

		for g, b := range gopsProfiling {
			if err := c.Pool.Submit(fmt.Sprintf("gops-%s-%s", p.Name, g), func(ctx context.Context) error {
				err := c.Client.ProxyTCP(ctx, p.Namespace, p.Name, port, func(stream io.ReadWriteCloser) error {
					defer stream.Close()

					_, err := stream.Write([]byte{b})
					if err != nil {
						return fmt.Errorf("requesting profiling data: %w", err)
					}

					file := c.AbsoluteTempPath(fmt.Sprintf("%s-%s-%s-<ts>.pprof", p.Name, containerName, g))
					outFile, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if err != nil {
						return fmt.Errorf("creating target file: %w", err)
					}
					defer outFile.Close()

					_, err = io.Copy(outFile, stream)
					if err != nil {
						return fmt.Errorf("saving profiling data: %w", err)
					}

					return nil
				})

				if err != nil {
					return fmt.Errorf("failed to collect gops profiling data: %w", err)
				}
				return nil
			}); err != nil {
				return fmt.Errorf("failed to submit %s gops task for %q: %w", g, p.Name, err)
			}
		}
	}
	return nil
}

// SubmitTracingGopsSubtask submits task to collect tracing data from pods.
func (c *Collector) SubmitTracingGopsSubtask(pods []*corev1.Pod, containerName string) error {
	for _, p := range pods {
		p := p
		if err := c.Pool.Submit(fmt.Sprintf("gops-%s-%s", p.Name, gopsTrace), func(ctx context.Context) error {
			agentPID, err := c.getGopsPID(ctx, p, containerName)
			if err != nil {
				return err
			}
			o, err := c.Client.ExecInPod(ctx, p.Namespace, p.Name, containerName, []string{
				gopsCommand,
				gopsTrace,
				agentPID,
			})
			if err != nil {
				return fmt.Errorf("failed to collect gops trace for %q (%q) in namespace %q: %w", p.Name, containerName, p.Namespace, err)
			}
			filePath, err := extractGopsProfileData(o.String())
			if err != nil {
				return fmt.Errorf("failed to collect gops trace for %q (%q) in namespace %q: %w", p.Name, containerName, p.Namespace, err)
			}
			f := c.AbsoluteTempPath(fmt.Sprintf("%s-%s-<ts>.trace", p.Name, gopsTrace))
			err = c.Client.CopyFromPod(ctx, p.Namespace, p.Name, containerName, filePath, f, c.Options.CopyRetryLimit)
			if err != nil {
				return fmt.Errorf("failed to collect gops trace output for %q: %w", p.Name, err)
			}
			if _, err = c.Client.ExecInPod(ctx, p.Namespace, p.Name, containerName, []string{rmCommand, filePath}); err != nil {
				c.logWarn("failed to delete trace output from pod %q in namespace %q: %w", p.Name, p.Namespace, err)
				return nil
			}
			return nil
		}); err != nil {
			return fmt.Errorf("failed to submit %s gops task for %q: %w", gopsTrace, p.Name, err)
		}
	}
	return nil
}

// SubmitLogsTasks submits tasks to collect kubernetes logs from pods.
func (c *Collector) SubmitLogsTasks(pods []*corev1.Pod, since time.Duration, limitBytes int64) error {
	t := metav1.NewTime(time.Now().Add(-since))
	for _, p := range pods {
		p := p
		allContainers := append(p.Spec.Containers, p.Spec.InitContainers...)
		for _, d := range allContainers {
			d := d
			if err := c.Pool.Submit(fmt.Sprintf("logs-%s-%s", p.Name, d.Name), func(ctx context.Context) error {
				if err := c.WithFileSink(fmt.Sprintf(ciliumLogsFileName, p.Name, d.Name), func(out io.Writer) error {
					return c.Client.GetLogs(ctx, p.Namespace, p.Name, d.Name,
						corev1.PodLogOptions{LimitBytes: &limitBytes, SinceTime: &t, Timestamps: true}, out)
				}); err != nil {
					return fmt.Errorf("failed to collect logs for %q (%q) in namespace %q: %w", p.Name, d.Name, p.Namespace, err)
				}

				// Check if this container has restarted, in which case we should gather the previous one's logs too.
				previous := false
				for _, s := range p.Status.ContainerStatuses {
					s := s
					if s.Name == d.Name && s.RestartCount > 0 {
						previous = true
					}
				}
				if previous {
					c.logDebug("Collecting logs for restarted container %q in pod %q in namespace %q", d.Name, p.Name, p.Namespace)
					if err := c.WithFileSink(fmt.Sprintf(ciliumPreviousLogsFileName, p.Name, d.Name), func(out io.Writer) error {
						return c.Client.GetLogs(ctx, p.Namespace, p.Name, d.Name,
							corev1.PodLogOptions{LimitBytes: &limitBytes, SinceTime: &t, Previous: true, Timestamps: true}, out)
					}); err != nil {
						return fmt.Errorf("failed to collect previous logs for %q (%q) in namespace %q: %w", p.Name, d.Name, p.Namespace, err)
					}
				}
				return nil
			}); err != nil {
				return fmt.Errorf("failed to submit logs task for %q (%q): %w", p.Name, d.Name, err)
			}
		}
	}
	return nil
}

func (c *Collector) submitFlavorSpecificTasks(f k8s.Flavor) error {
	switch f.Kind {
	case k8s.KindEKS:
		if err := c.Pool.Submit(awsNodeDaemonSetName, func(ctx context.Context) error {
			// Collect the 'kube-system/aws-node' DaemonSet.
			d, err := c.Client.GetDaemonSet(ctx, awsNodeDaemonSetNamespace, awsNodeDaemonSetName, metav1.GetOptions{})
			if err != nil {
				if k8sErrors.IsNotFound(err) {
					c.logDebug("DaemonSet %q not found in namespace %q - this is expected when running in ENI mode", awsNodeDaemonSetName, awsNodeDaemonSetNamespace)
					return nil
				}
				return fmt.Errorf("failed to collect daemonset %q in namespace %q: %w", awsNodeDaemonSetName, awsNodeDaemonSetNamespace, err)
			}
			if err := c.WriteYAML(awsNodeDaemonSetFileName, d); err != nil {
				return fmt.Errorf("failed to collect daemonset %q in namespace %q: %w", awsNodeDaemonSetName, awsNodeDaemonSetNamespace, err)
			}
			// Only if the 'kube-system/aws-node' Daemonset is present...
			// ... collect any "SecurityGroupPolicy" resources.
			n := corev1.NamespaceAll
			l, err := c.Client.ListUnstructured(ctx, awsSecurityGroupPoliciesGVR, &n, metav1.ListOptions{})
			if err != nil {
				return fmt.Errorf("failed to collect security group policies: %w", err)
			}
			if err := c.WriteYAML(securityGroupPoliciesFileName, l); err != nil {
				return fmt.Errorf("failed to collect security group policies: %w", err)
			}
			// ... collect any "ENIConfigs" resources.
			l, err = c.Client.ListUnstructured(ctx, awsENIConfigsGVR, nil, metav1.ListOptions{})
			if err != nil {
				return fmt.Errorf("failed to collect ENI configs: %w", err)
			}
			if err := c.WriteYAML(eniconfigsFileName, l); err != nil {
				return fmt.Errorf("failed to collect ENI configs: %w", err)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("failed to submit %q task: %w", awsNodeDaemonSetName, err)
		}
		return nil
	default:
		c.logDebug("No flavor-specific data to collect for %q", f.Kind.String())
		return nil
	}
}

func (c *Collector) submitKVStoreTasks(ctx context.Context, pod *corev1.Pod) error {
	for _, dump := range []struct {
		name   string
		prefix string
	}{
		{
			name:   "identities",
			prefix: "state/identities",
		},
		{
			name:   "ips",
			prefix: "state/ip",
		},
		{
			name:   "nodes",
			prefix: "state/nodes",
		},
		{
			name:   "cnpstatuses",
			prefix: "state/cnpstatuses",
		},
		{
			name:   "heartbeat",
			prefix: ".heartbeat",
		},
		{
			name:   "services",
			prefix: "state/services",
		},
	} {
		filename := "kvstore-" + dump.name + ".json"

		var stderr bytes.Buffer
		if err := c.WithFileSink(filename, func(out io.Writer) error {
			return c.Client.ExecInPodWithWriters(ctx, nil, pod.Namespace, pod.Name, defaults.AgentContainerName, []string{
				"cilium", "kvstore", "get", "cilium/" + dump.prefix, "--recursive", "-o", "json",
			}, out, &stderr)
		}); err != nil {
			return fmt.Errorf("failed to collect kvstore data in %q in namespace %q: %w: %s", pod.Name, pod.Namespace, err, stderr.String())
		}
	}

	return nil
}

// SubmitMetricsSubtask submits tasks to collect metrics from pods.
func (c *Collector) SubmitMetricsSubtask(pods []*corev1.Pod, containerName, portName string) error {
	for _, p := range pods {
		p := p
		if !podIsRunningAndHasContainer(p, containerName) {
			continue
		}
		err := c.Pool.Submit(fmt.Sprintf("metrics-%s-%s-%s", p.Name, containerName, portName), func(ctx context.Context) error {
			port, err := getPodMetricsPort(p, containerName, portName)
			if err != nil {
				return fmt.Errorf("failed to collect metrics: %w - this is expected if prometheus metrics are disabled", err)
			}
			rsp, err := c.Client.ProxyGet(ctx, p.Namespace, fmt.Sprintf("%s:%d", p.Name, port), "metrics")
			if err != nil {
				return fmt.Errorf("failed to collect metrics: %w", err)
			}
			if err := c.WriteString(fmt.Sprintf(metricsFileName, p.Name, containerName), rsp); err != nil {
				return fmt.Errorf("failed to collect metrics: %w", err)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to submit metrics task for %q: %w", p.Name, err)
		}
	}
	return nil
}

func (c *Collector) submitClusterMeshAPIServerDbgTasks(pods []*corev1.Pod) error {
	tasks := []struct {
		name      string
		ext       string
		cmd       []string
		container string
	}{
		{
			name:      "version",
			ext:       "txt",
			cmd:       []string{defaults.ClusterMeshBinaryName, "version"},
			container: defaults.ClusterMeshContainerName},
		{
			name:      "troubleshoot",
			ext:       "txt",
			cmd:       []string{defaults.ClusterMeshBinaryName, "clustermesh-dbg", "troubleshoot"},
			container: defaults.ClusterMeshContainerName,
		},
		{
			name:      "version",
			ext:       "txt",
			cmd:       []string{defaults.ClusterMeshBinaryName, "version"},
			container: defaults.ClusterMeshKVStoreMeshContainerName,
		},
		{
			name:      "status",
			ext:       "txt",
			cmd:       []string{defaults.ClusterMeshBinaryName, "kvstoremesh-dbg", "status", "--verbose"},
			container: defaults.ClusterMeshKVStoreMeshContainerName,
		},
		{
			name:      "status",
			ext:       "json",
			cmd:       []string{defaults.ClusterMeshBinaryName, "kvstoremesh-dbg", "status", "-o", "json"},
			container: defaults.ClusterMeshKVStoreMeshContainerName,
		},
		{
			name:      "troubleshoot",
			ext:       "txt",
			cmd:       []string{defaults.ClusterMeshBinaryName, "kvstoremesh-dbg", "troubleshoot", "--include-local"},
			container: defaults.ClusterMeshKVStoreMeshContainerName,
		},
	}

	for _, pod := range pods {
		for _, task := range tasks {
			if !podIsRunningAndHasContainer(pod, task.container) {
				continue
			}

			filename := fmt.Sprintf("%s-%s-%s-<ts>.%s", pod.Name, task.container, task.name, task.ext)
			if err := c.Pool.Submit(filename, func(ctx context.Context) error {
				if err := c.WithFileSink(filename, func(out io.Writer) error {
					var stderr bytes.Buffer

					err := c.Client.ExecInPodWithWriters(ctx, nil, pod.Namespace, pod.Name, task.container, task.cmd, out, &stderr)
					if err != nil {
						stderrStr := stderr.String()
						if strings.Contains(stderrStr, "Usage:") || strings.Contains(stderrStr, "unknown command") {
							// The default cobra error tends to be misleading when both the
							// command and the flags are not found, as it reports the missing
							// flags rather than the missing command. Hence, let's just guess
							// and output a generic unknown command error.
							stderrStr = "unknown command - this is expected if not supported by this Cilium version"
						}

						return fmt.Errorf("%w: %s", err, stderrStr)
					}

					return nil
				}); err != nil {
					return fmt.Errorf("failed to collect clustermesh %s information from %s/%s (%s): %w",
						task.name, pod.Namespace, pod.Name, task.container, err)
				}

				return nil
			}); err != nil {
				return fmt.Errorf("failed to submit %s task: %w", filename, err)
			}
		}
	}

	return nil
}

func getPodMetricsPort(pod *corev1.Pod, containerName, portName string) (int32, error) {
	for _, container := range pod.Spec.Containers {
		if container.Name != containerName {
			continue
		}
		for _, port := range container.Ports {
			if port.Name == portName {
				return port.ContainerPort, nil
			}
		}
	}

	return 0, fmt.Errorf("failed to find port %s in container %s in pod %s", portName, containerName, pod.Name)
}

// podIsRunningAndHasContainer returns whether the given pod is running,
// and includes the specified container.
func podIsRunningAndHasContainer(pod *corev1.Pod, container string) bool {
	if pod.Status.Phase == corev1.PodRunning {
		return slices.ContainsFunc(pod.Spec.Containers, func(c corev1.Container) bool {
			return c.Name == container
		})
	}
	return false
}

func buildNodeNameList(nodes *corev1.NodeList, filter string) []string {
	w := strings.Split(strings.TrimSpace(filter), ",")
	r := make([]string, 0)
	for _, node := range nodes.Items {
		if len(w) == 0 || w[0] == "" {
			r = append(r, node.Name)
			continue
		}
		if isNodeInWhitelist(node, w) {
			r = append(r, node.Name)
			continue
		}
	}
	return r
}

// AllPods converts a PodList into a slice of Pod objects.
func AllPods(l *corev1.PodList) []*corev1.Pod {
	return filterPods(l, func(*corev1.Pod) bool { return true }, 0)
}

// FilterPods filters a list of pods by node names.
func FilterPods(l *corev1.PodList, n []string) []*corev1.Pod {
	return filterPods(l, func(po *corev1.Pod) bool { return slices.Contains(n, po.Spec.NodeName) }, 0)
}

func filterCrashedPods(l *corev1.PodList, limit int) []*corev1.Pod {
	return filterPods(l, func(po *corev1.Pod) bool {
		for _, containerStatus := range po.Status.InitContainerStatuses {
			if containerStatus.State.Waiting != nil && containerStatus.State.Waiting.Reason == "CrashLoopBackOff" {
				return true
			}
		}
		for _, containerStatus := range po.Status.ContainerStatuses {
			if containerStatus.State.Waiting != nil && containerStatus.State.Waiting.Reason == "CrashLoopBackOff" {
				return true
			}
		}
		return false
	}, limit)
}

func filterRunningNotReadyPods(l *corev1.PodList, limit int) []*corev1.Pod {
	return filterPods(l, func(po *corev1.Pod) bool {
		if po.Status.Phase != corev1.PodRunning {
			return false
		}
		return !podutils.IsPodReady(po)
	}, limit)
}

func filterRestartedContainersPods(l *corev1.PodList, limit int) []*corev1.Pod {
	return filterPods(l, func(po *corev1.Pod) bool {
		for _, containerStatus := range po.Status.InitContainerStatuses {
			if containerStatus.RestartCount > 0 {
				return true
			}
		}
		for _, containerStatus := range po.Status.ContainerStatuses {
			if containerStatus.RestartCount > 0 {
				return true
			}
		}
		return false
	}, limit)
}

func filterPods(l *corev1.PodList, filter func(po *corev1.Pod) bool, limit int) []*corev1.Pod {
	r := make([]*corev1.Pod, 0)
	for _, p := range l.Items {
		p := p
		if filter(&p) {
			r = append(r, &p)
			if limit > 0 && len(r) >= limit {
				break
			}
		}
	}
	return r
}

func isNodeInWhitelist(node corev1.Node, w []string) bool {
	for _, n := range w {
		if node.Name == n {
			return true
		}
		for _, a := range node.Status.Addresses {
			if a.Address == n {
				return true
			}
		}
	}
	return false
}

// AddTasks adds extra tasks for the collector to execute. Must be called before Run().
func (c *Collector) AddTasks(tasks []Task) {
	c.additionalTasks = append(c.additionalTasks, tasks...)
}

func detectCiliumNamespace(k KubernetesClient) (string, error) {
	for _, ns := range DefaultCiliumNamespaces {
		ctx := context.Background()
		ns, err := k.GetNamespace(ctx, ns, metav1.GetOptions{})
		if k8sErrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("failed to detect Cilium namespace: %w", err)
		}

		_, err = k.GetDaemonSet(ctx, ns.Name, "cilium", metav1.GetOptions{})
		if k8sErrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("failed to check for Cilium DaemonSet: %w", err)
		}
		return ns.Name, nil
	}
	return "", fmt.Errorf("failed to detect Cilium namespace, could not find Cilium installation in namespaces: %v", DefaultCiliumNamespaces)
}

func detectCiliumOperatorNamespace(k KubernetesClient) (string, error) {
	for _, ns := range DefaultCiliumNamespaces {
		ctx := context.Background()
		ns, err := k.GetNamespace(ctx, ns, metav1.GetOptions{})
		if k8sErrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("failed to detect Cilium operator namespace: %w", err)
		}

		_, err = k.GetDeployment(ctx, ns.Name, ciliumOperatorDeploymentName, metav1.GetOptions{})
		if k8sErrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("failed to check for Cilium operator Deployment: %w", err)
		}
		return ns.Name, nil
	}
	return "", fmt.Errorf("failed to detect Cilium operator namespace, could not find Cilium installation in namespaces: %v", DefaultCiliumNamespaces)
}

func detectCiliumSPIRENamespace(k KubernetesClient) (string, error) {
	for _, ns := range DefaultCiliumSPIRENamespaces {
		ctx := context.Background()
		ns, err := k.GetNamespace(ctx, ns, metav1.GetOptions{})
		if k8sErrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("failed to detect Cilium SPIRE namespace: %w", err)
		}

		_, err = k.GetDaemonSet(ctx, ns.Name, ciliumSPIREAgentDaemonSetName, metav1.GetOptions{})
		if k8sErrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("failed to check for Cilium SPIRE agent DaemonSet: %w", err)
		}
		return ns.Name, nil
	}
	return "", fmt.Errorf("failed to detect Cilium SPIRE namespace, could not find Cilium SPIRE installation in namespaces: %v", DefaultCiliumSPIRENamespaces)
}

func InitSysdumpFlags(cmd *cobra.Command, options *Options, optionPrefix string, hooks Hooks) {
	cmd.Flags().StringVar(&options.CiliumLabelSelector,
		optionPrefix+"cilium-label-selector", DefaultCiliumLabelSelector,
		"The labels used to target Cilium pods")
	cmd.Flags().StringVar(&options.CiliumNamespace,
		optionPrefix+"cilium-namespace", "",
		"The namespace Cilium is running in. If not provided then the --namespace global flag is used (if provided)")
	cmd.Flags().StringVar(&options.CiliumOperatorNamespace,
		optionPrefix+"cilium-operator-namespace", "",
		"The namespace Cilium operator is running in. If not provided then the --namespace global flag is used (if provided)")
	cmd.Flags().StringVar(&options.CiliumSPIRENamespace,
		optionPrefix+"cilium-spire-namespace", "",
		"The namespace Cilium SPIRE installation is running in")
	cmd.Flags().StringVar(&options.CiliumDaemonSetSelector,
		optionPrefix+"cilium-daemon-set-label-selector", DefaultCiliumLabelSelector,
		"The labels used to target Cilium daemon set")
	cmd.Flags().StringVar(&options.CiliumEnvoyLabelSelector,
		optionPrefix+"cilium-envoy-label-selector", DefaultCiliumEnvoyLabelSelector,
		"The labels used to target Cilium Envoy pods")
	cmd.Flags().StringVar(&options.CiliumHelmReleaseName,
		optionPrefix+"cilium-helm-release-name", "",
		"The Cilium Helm release name for which to get values. If not provided then the --helm-release-name global flag is used (if provided)")
	cmd.Flags().StringVar(&options.TetragonHelmReleaseName,
		optionPrefix+"tetragon-helm-release-name", "",
		"The Tetragon Helm release name for which to get values.")
	cmd.Flags().StringVar(&options.CiliumOperatorLabelSelector,
		optionPrefix+"cilium-operator-label-selector", DefaultCiliumOperatorLabelSelector,
		"The labels used to target Cilium operator pods")
	cmd.Flags().StringVar(&options.ClustermeshApiserverLabelSelector,
		optionPrefix+"clustermesh-apiserver-label-selector", DefaultClustermeshApiserverLabelSelector,
		"The labels used to target 'clustermesh-apiserver' pods")
	cmd.Flags().StringVar(&options.CiliumNodeInitLabelSelector,
		optionPrefix+"cilium-node-init-selector", DefaultCiliumNodeInitLabelSelector,
		"The labels used to target Cilium node init pods")
	cmd.Flags().StringVar(&options.CiliumSPIREAgentLabelSelector,
		optionPrefix+"cilium-spire-agent-selector", DefaultCiliumSpireAgentLabelSelector,
		"The labels used to target Cilium spire-agent pods")
	cmd.Flags().StringVar(&options.CiliumSPIREServerLabelSelector,
		optionPrefix+"cilium-spire-server-selector", DefaultCiliumSpireServerLabelSelector,
		"The labels used to target Cilium spire-server pods")
	cmd.Flags().BoolVar(&options.CollectLogsFromNotReadyAgents,
		optionPrefix+"collect-logs-from-not-ready-agents", DefaultCollectLogsFromNotReadyAgents,
		"Whether to collect logs from not ready Cilium agent pods")
	cmd.Flags().BoolVar(&options.Debug,
		optionPrefix+"debug", DefaultDebug,
		"Whether to enable debug logging")
	cmd.Flags().BoolVar(&options.Profiling,
		optionPrefix+"profiling", DefaultProfiling,
		"Whether to enable scraping profiling data")
	cmd.Flags().BoolVar(&options.Tracing,
		optionPrefix+"tracing", DefaultTracing,
		"Whether to enable scraping tracing data")
	cmd.Flags().StringArrayVar(&options.ExtraLabelSelectors,
		optionPrefix+"extra-label-selectors", nil,
		"Optional set of labels selectors used to target additional pods for log collection.")
	cmd.Flags().StringVar(&options.HubbleLabelSelector,
		optionPrefix+"hubble-label-selector", DefaultHubbleLabelSelector,
		"The labels used to target Hubble pods")
	cmd.Flags().Int64Var(&options.HubbleFlowsCount,
		optionPrefix+"hubble-flows-count", DefaultHubbleFlowsCount,
		"Number of Hubble flows to collect. Setting to zero disables collecting Hubble flows.")
	cmd.Flags().DurationVar(&options.HubbleFlowsTimeout,
		optionPrefix+"hubble-flows-timeout", DefaultHubbleFlowsTimeout,
		"Timeout for collecting Hubble flows")
	cmd.Flags().StringVar(&options.HubbleRelayLabelSelector,
		optionPrefix+"hubble-relay-labels", DefaultHubbleRelayLabelSelector,
		"The labels used to target Hubble Relay pods")
	cmd.Flags().StringVar(&options.HubbleUILabelSelector,
		optionPrefix+"hubble-ui-labels", DefaultHubbleUILabelSelector,
		"The labels used to target Hubble UI pods")
	cmd.Flags().StringVar(&options.HubbleGenerateCertsLabelSelector,
		optionPrefix+"hubble-generate-certs-labels", DefaultHubbleGenerateCertsLabelSelector,
		"The labels used to target Hubble UI pods")
	cmd.Flags().Int64Var(&options.LogsLimitBytes,
		optionPrefix+"logs-limit-bytes", DefaultLogsLimitBytes,
		"The limit on the number of bytes to retrieve when collecting logs")
	cmd.Flags().DurationVar(&options.LogsSinceTime,
		optionPrefix+"logs-since-time", DefaultLogsSinceTime,
		"How far back in time to go when collecting logs")
	cmd.Flags().StringVar(&options.NodeList,
		optionPrefix+"node-list", DefaultNodeList,
		"Comma-separated list of node IPs or names to filter pods for which to collect gops and logs")
	cmd.Flags().StringVar(&options.OutputFileName,
		optionPrefix+"output-filename", DefaultOutputFileName,
		"The name of the resulting file (without extension)\n'<ts>' can be used as the placeholder for the timestamp")
	cmd.Flags().BoolVar(&options.Quick,
		optionPrefix+"quick", DefaultQuick,
		"Whether to enable quick mode (i.e. skip collection of 'cilium-bugtool' output and logs)")
	cmd.Flags().IntVar(&options.WorkerCount,
		optionPrefix+"worker-count", DefaultWorkerCount,
		"The number of workers to use\nNOTE: There is a lower bound requirement on the number of workers for the sysdump operation to be effective. Therefore, for low values, the actual number of workers may be adjusted upwards. Defaults to the number of available CPUs.")
	cmd.Flags().StringArrayVar(&options.CiliumBugtoolFlags,
		optionPrefix+"cilium-bugtool-flags", nil,
		"Optional set of flags to pass to cilium-bugtool command.")
	cmd.Flags().BoolVar(&options.DetectGopsPID,
		optionPrefix+"detect-gops-pid", false,
		"Whether to automatically detect the gops agent PID.")
	cmd.Flags().StringVar(&options.CNIConfigDirectory,
		optionPrefix+"cni-config-directory", DefaultCNIConfigDirectory,
		"Directory where CNI configs are located")
	cmd.Flags().StringVar(&options.CNIConfigMapName,
		optionPrefix+"cni-configmap-name", DefaultCNIConfigMapName,
		"The name of the CNI config map")
	cmd.Flags().StringVar(&options.TetragonNamespace,
		optionPrefix+"tetragon-namespace", DefaultTetragonNamespace,
		"The namespace Tetragon is running in")
	cmd.Flags().StringVar(&options.TetragonLabelSelector,
		optionPrefix+"tetragon-label-selector", DefaultTetragonLabelSelector,
		"The labels used to target Tetragon pods")
	cmd.Flags().StringVar(&options.TetragonOperatorLabelSelector,
		optionPrefix+"tetragon-operator-label-selector", DefaultTetragonOperatorLabelSelector,
		"The labels used to target Tetragon operator pods")
	cmd.Flags().IntVar(&options.CopyRetryLimit,
		optionPrefix+"copy-retry-limit", DefaultCopyRetryLimit,
		"Retry limit for file copying operations. If set to -1, copying will be retried indefinitely. Useful for collecting sysdump while on unreliable connection.")

	hooks.AddSysdumpFlags(cmd.Flags())
}

// Hooks to extend cilium-cli with additional sysdump tasks and related flags.
type Hooks interface {
	AddSysdumpFlags(flags *pflag.FlagSet)
	AddSysdumpTasks(*Collector) error
}
