// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package manager

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/cilium/hive/cell"
	"github.com/cilium/hive/job"
	"github.com/cilium/statedb"
	"github.com/google/renameio/v2"
	jsoniter "github.com/json-iterator/go"
	"go4.org/netipx"
	"golang.org/x/time/rate"

	"github.com/cilium/cilium/pkg/backoff"
	"github.com/cilium/cilium/pkg/cidr"
	cmtypes "github.com/cilium/cilium/pkg/clustermesh/types"
	"github.com/cilium/cilium/pkg/controller"
	"github.com/cilium/cilium/pkg/datapath/iptables/ipset"
	"github.com/cilium/cilium/pkg/datapath/tables"
	"github.com/cilium/cilium/pkg/datapath/tunnel"
	datapath "github.com/cilium/cilium/pkg/datapath/types"
	"github.com/cilium/cilium/pkg/identity"
	"github.com/cilium/cilium/pkg/ip"
	"github.com/cilium/cilium/pkg/ipcache"
	ipcacheTypes "github.com/cilium/cilium/pkg/ipcache/types"
	k8sConst "github.com/cilium/cilium/pkg/k8s/apis/cilium.io"
	"github.com/cilium/cilium/pkg/labels"
	"github.com/cilium/cilium/pkg/labelsfilter"
	"github.com/cilium/cilium/pkg/lock"
	"github.com/cilium/cilium/pkg/logging/logfields"
	"github.com/cilium/cilium/pkg/metrics"
	"github.com/cilium/cilium/pkg/metrics/metric"
	"github.com/cilium/cilium/pkg/node"
	"github.com/cilium/cilium/pkg/node/addressing"
	nodeTypes "github.com/cilium/cilium/pkg/node/types"
	"github.com/cilium/cilium/pkg/option"
	"github.com/cilium/cilium/pkg/source"
	"github.com/cilium/cilium/pkg/time"
	"github.com/cilium/cilium/pkg/trigger"
	"github.com/cilium/cilium/pkg/wireguard/types"
)

const (
	// The filename for the nodes checkpoint. This is periodically written, and
	// restored on restart. The default path is /run/cilium/state/nodes.json
	nodesFilename = "nodes.json"
	// Minimum amount of time to wait in between writing nodes file.
	nodeCheckpointMinInterval = time.Minute
)

var (
	baseBackgroundSyncInterval = time.Minute
)

type nodeEntry struct {
	// mutex serves two purposes:
	// 1. Serialize any direct access to the node field in this entry.
	// 2. Serialize all calls do the datapath layer for a particular node.
	//
	// See description of Manager.mutex for more details
	//
	// If both the nodeEntry.mutex and Manager.mutex must be held, then the
	// Manager.mutex must *always* be acquired first.
	mutex lock.Mutex
	node  nodeTypes.Node
}

// IPCache is the set of interactions the node manager performs with the ipcache
type IPCache interface {
	GetMetadataSourceByPrefix(prefix cmtypes.PrefixCluster) source.Source
	UpsertMetadata(prefix cmtypes.PrefixCluster, src source.Source, resource ipcacheTypes.ResourceID, aux ...ipcache.IPMetadata)
	OverrideIdentity(prefix cmtypes.PrefixCluster, identityLabels labels.Labels, src source.Source, resource ipcacheTypes.ResourceID)
	RemoveMetadata(prefix cmtypes.PrefixCluster, resource ipcacheTypes.ResourceID, aux ...ipcache.IPMetadata)
	RemoveIdentityOverride(prefix cmtypes.PrefixCluster, identityLabels labels.Labels, resource ipcacheTypes.ResourceID)
	UpsertMetadataBatch(updates ...ipcache.MU) (revision uint64)
	RemoveMetadataBatch(updates ...ipcache.MU) (revision uint64)
}

// IPSetFilterFn is a function allowing to optionally filter out the insertion
// of IPSet entries based on node characteristics. The insertion is performed
// if the function returns false, and skipped otherwise.
type IPSetFilterFn func(*nodeTypes.Node) bool

var _ Notifier = (*manager)(nil)

// manager is the entity that manages a collection of nodes
type manager struct {
	logger *slog.Logger
	// mutex is the lock protecting access to the nodes map. The mutex must
	// be held for any access of the nodes map.
	//
	// The manager mutex works together with the entry mutex in the
	// following way to minimize the duration the manager mutex is held:
	//
	// 1. Acquire manager mutex to safely access nodes map and to retrieve
	//    node entry.
	// 2. Acquire mutex of the entry while the manager mutex is still held.
	//    This guarantees that no change to the entry has happened.
	// 3. Release of the manager mutex to unblock changes or reads to other
	//    node entries.
	// 4. Change of entry data or performing of datapath interactions
	// 5. Release of the entry mutex
	//
	// If both the nodeEntry.mutex and Manager.mutex must be held, then the
	// Manager.mutex must *always* be acquired first.
	mutex lock.RWMutex

	// nodes is the list of nodes. Access must be protected via mutex.
	nodes map[nodeTypes.Identity]*nodeEntry

	// Upon agent startup, this is filled with nodes as read from disk. Used to
	// synthesize node deletion events for nodes which disappeared while we were
	// down.
	restoredNodes map[nodeTypes.Identity]*nodeTypes.Node

	// nodeHandlersMu protects the nodeHandlers map against concurrent access.
	nodeHandlersMu lock.RWMutex
	// nodeHandlers has a slice containing all node handlers subscribed to node
	// events.
	nodeHandlers map[datapath.NodeHandler]struct{}

	// group of jobs, tied to the lifecycle of the manager
	jobGroup job.Group

	// metrics to track information about the node manager
	metrics *nodeMetrics

	// conf is the configuration of the caller passed in via NewManager.
	// This field is immutable after NewManager()
	conf *option.DaemonConfig

	underlay tunnel.UnderlayProtocol

	// ipcache is the set operations performed against the ipcache
	ipcache IPCache

	// ipsetMgr is the ipset cluster nodes configuration manager
	ipsetMgr         ipset.Manager
	ipsetInitializer ipset.Initializer
	ipsetFilter      IPSetFilterFn

	// controllerManager manages the controllers that are launched within the
	// Manager.
	controllerManager *controller.Manager

	// health reports on the current health status of the node manager module.
	health cell.Health

	// nodeCheckpointer triggers writing the current set of nodes to disk
	nodeCheckpointer *trigger.Trigger
	checkpointerDone chan struct{} // Closed once the checkpointer is shut down.

	// Ensure the pruning is only attempted once.
	nodePruneOnce sync.Once

	// Reference to the StateDB
	db *statedb.DB
	// The devices table
	devices statedb.Table[*tables.Device]

	// custom mutator function to enrich prefixCluster(s) from node objects.
	prefixClusterMutatorFn func(node *nodeTypes.Node) []cmtypes.PrefixClusterOpts
}

// Subscribe subscribes the given node handler to node events.
func (m *manager) Subscribe(nh datapath.NodeHandler) {
	m.nodeHandlersMu.Lock()
	m.nodeHandlers[nh] = struct{}{}
	m.nodeHandlersMu.Unlock()
	// Add all nodes already received by the manager.
	m.mutex.RLock()
	for _, v := range m.nodes {
		v.mutex.Lock()
		if err := nh.NodeAdd(v.node); err != nil {
			m.logger.Error(
				"Failed applying node handler following initial subscribe. Cilium may have degraded functionality. See error message for more details.",
				logfields.Error, err,
				logfields.Handler, nh.Name(),
				logfields.Node, v.node.Name,
			)
		}
		v.mutex.Unlock()
	}
	m.mutex.RUnlock()
}

// Unsubscribe unsubscribes the given node handler with node events.
func (m *manager) Unsubscribe(nh datapath.NodeHandler) {
	m.nodeHandlersMu.Lock()
	delete(m.nodeHandlers, nh)
	m.nodeHandlersMu.Unlock()
}

// Iter executes the given function in all subscribed node handlers.
func (m *manager) Iter(f func(nh datapath.NodeHandler)) {
	m.nodeHandlersMu.RLock()
	defer m.nodeHandlersMu.RUnlock()

	for nh := range m.nodeHandlers {
		f(nh)
	}
}

type nodeMetrics struct {
	// metricEventsReceived is the prometheus metric to track the number of
	// node events received
	EventsReceived metric.Vec[metric.Counter]

	// metricNumNodes is the prometheus metric to track the number of nodes
	// being managed
	NumNodes metric.Gauge

	// metricDatapathValidations is the prometheus metric to track the
	// number of datapath node validation calls
	DatapathValidations metric.Counter
}

func NewNodeMetrics() *nodeMetrics {
	return &nodeMetrics{
		EventsReceived: metric.NewCounterVec(metric.CounterOpts{
			ConfigName: metrics.Namespace + "_" + "nodes_all_events_received_total",
			Namespace:  metrics.Namespace,
			Subsystem:  "nodes",
			Name:       "all_events_received_total",
			Help:       "Number of node events received",
		}, []string{"event_type", "source"}),

		NumNodes: metric.NewGauge(metric.GaugeOpts{
			ConfigName: metrics.Namespace + "_" + "nodes_all_num",
			Namespace:  metrics.Namespace,
			Subsystem:  "nodes",
			Name:       "all_num",
			Help:       "Number of nodes managed",
		}),

		DatapathValidations: metric.NewCounter(metric.CounterOpts{
			ConfigName: metrics.Namespace + "_" + "nodes_all_datapath_validations_total",
			Namespace:  metrics.Namespace,
			Subsystem:  "nodes",
			Name:       "all_datapath_validations_total",
			Help:       "Number of validation calls to implement the datapath implementation of a node",
		}),
	}
}

// New returns a new node manager
func New(
	logger *slog.Logger,
	c *option.DaemonConfig,
	tunnelConf tunnel.Config,
	ipCache IPCache,
	ipsetMgr ipset.Manager,
	ipsetFilter IPSetFilterFn,
	nodeMetrics *nodeMetrics,
	health cell.Health,
	jobGroup job.Group,
	db *statedb.DB,
	devices statedb.Table[*tables.Device],
) (*manager, error) {
	if ipsetFilter == nil {
		ipsetFilter = func(*nodeTypes.Node) bool { return false }
	}

	m := &manager{
		logger:                 logger,
		nodes:                  map[nodeTypes.Identity]*nodeEntry{},
		restoredNodes:          map[nodeTypes.Identity]*nodeTypes.Node{},
		conf:                   c,
		underlay:               tunnelConf.UnderlayProtocol(),
		controllerManager:      controller.NewManager(),
		nodeHandlers:           map[datapath.NodeHandler]struct{}{},
		ipcache:                ipCache,
		ipsetMgr:               ipsetMgr,
		ipsetInitializer:       ipsetMgr.NewInitializer(),
		ipsetFilter:            ipsetFilter,
		metrics:                nodeMetrics,
		health:                 health,
		jobGroup:               jobGroup,
		db:                     db,
		devices:                devices,
		prefixClusterMutatorFn: func(node *nodeTypes.Node) []cmtypes.PrefixClusterOpts { return nil },
	}

	return m, nil
}

func (m *manager) Start(cell.HookContext) error {
	// Ensure that we read a potential nodes file before we overwrite it.
	m.restoreNodeCheckpoint()
	if err := m.initNodeCheckpointer(nodeCheckpointMinInterval); err != nil {
		return fmt.Errorf("failed to initialize node file writer: %w", err)
	}

	m.jobGroup.Add(job.OneShot("backgroundSync", m.backgroundSync))

	return nil
}

// Stop shuts down a node manager
func (m *manager) Stop(cell.HookContext) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.nodeCheckpointer != nil {
		// Using the shutdown func of trigger to checkpoint would block shutdown
		// for up to its MinInterval, which is too long.
		m.nodeCheckpointer.Shutdown()
		close(m.checkpointerDone)
		err := m.checkpoint()
		if err != nil {
			m.logger.Error("Failed to write final node checkpoint.", logfields.Error, err)
		}
		m.nodeCheckpointer = nil
	}

	return nil
}

// ClusterSizeDependantInterval returns a time.Duration that is dependant on
// the cluster size, i.e. the number of nodes that have been discovered. This
// can be used to control sync intervals of shared or centralized resources to
// avoid overloading these resources as the cluster grows.
//
// Example sync interval with baseInterval = 1 * time.Minute
//
// nodes | sync interval
// ------+-----------------
// 1     |   41.588830833s
// 2     | 1m05.916737320s
// 4     | 1m36.566274746s
// 8     | 2m11.833474640s
// 16    | 2m49.992800643s
// 32    | 3m29.790453687s
// 64    | 4m10.463236193s
// 128   | 4m51.588744261s
// 256   | 5m32.944565093s
// 512   | 6m14.416550710s
// 1024  | 6m55.946873494s
// 2048  | 7m37.506428894s
// 4096  | 8m19.080616652s
// 8192  | 9m00.662124608s
// 16384 | 9m42.247293667s
func (m *manager) ClusterSizeDependantInterval(baseInterval time.Duration) time.Duration {
	m.mutex.RLock()
	numNodes := len(m.nodes)
	m.mutex.RUnlock()

	return backoff.ClusterSizeDependantInterval(baseInterval, numNodes)
}

func (m *manager) backgroundSyncInterval() time.Duration {
	return m.ClusterSizeDependantInterval(baseBackgroundSyncInterval)
}

// backgroundSync ensures that local node has a valid datapath in-place for
// each node in the cluster. See NodeValidateImplementation().
func (m *manager) backgroundSync(ctx context.Context, health cell.Health) error {
	for {
		syncInterval := m.backgroundSyncInterval()
		startWaiting := time.After(syncInterval)
		m.logger.Debug(
			"Starting new iteration of background sync",
			logfields.SyncInterval, syncInterval,
		)
		err := m.singleBackgroundLoop(ctx, syncInterval)
		m.logger.Debug(
			"Finished iteration of background sync",
			logfields.SyncInterval, syncInterval,
		)

		select {
		case <-ctx.Done():
			return nil
		// This handles cases when we didn't fetch nodes yet (e.g. on bootstrap)
		// but also case when we have 1 node, in which case rate.Limiter doesn't
		// throttle anything.
		case <-startWaiting:
		}

		if err != nil {
			health.Degraded("Failed to apply node validation", err)
		} else {
			health.OK("Node validation successful")
		}
	}
}

func (m *manager) singleBackgroundLoop(ctx context.Context, expectedLoopTime time.Duration) error {
	var errs error
	// get a copy of the node identities to avoid locking the entire manager
	// throughout the process of running the datapath validation.
	nodes := m.GetNodeIdentities()
	limiter := rate.NewLimiter(
		rate.Limit(float64(len(nodes))/float64(expectedLoopTime.Seconds())),
		1, // One token in bucket to amortize for latency of the operation
	)
	for _, nodeIdentity := range nodes {
		if err := limiter.Wait(ctx); err != nil {
			m.logger.Debug(
				"Error while rate limiting backgroundSync updates",
				logfields.Error, err,
			)
		}

		select {
		case <-ctx.Done():
			return nil
		default:
		}
		// Retrieve latest node information in case any event
		// changed the node since the call to GetNodes()
		m.mutex.RLock()
		entry, ok := m.nodes[nodeIdentity]
		if !ok {
			m.mutex.RUnlock()
			continue
		}
		entry.mutex.Lock()
		m.mutex.RUnlock()
		{
			m.Iter(func(nh datapath.NodeHandler) {
				if err := nh.NodeValidateImplementation(entry.node); err != nil {
					m.logger.Error(
						"Failed to apply node handler during background sync. Cilium may have degraded functionality. See error message for details.",
						logfields.Error, err,
						logfields.Handler, nh.Name(),
						logfields.Node, entry.node.Name,
					)
					errs = errors.Join(errs, fmt.Errorf("failed while handling %s on node %s: %w", nh.Name(), entry.node.Name, err))
				}
			})
		}
		entry.mutex.Unlock()

		m.metrics.DatapathValidations.Inc()
	}
	return errs
}

func (m *manager) restoreNodeCheckpoint() {
	path := filepath.Join(m.conf.StateDir, nodesFilename)
	scopedLog := m.logger.With(logfields.Path, path)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// If we don't have a file to restore from, there's nothing we can
			// do. This is expected in the upgrade path.
			scopedLog.Debug(
				fmt.Sprintf("No %v file found, cannot replay node deletion events for nodes"+
					" which disappeared during downtime.", nodesFilename),
			)
			return
		}
		scopedLog.Error(
			"failed to read node checkpoint file",
			logfields.Error, err,
		)
		return
	}

	r := jsoniter.ConfigFastest.NewDecoder(bufio.NewReader(f))
	var nodeCheckpoint []*nodeTypes.Node
	if err := r.Decode(&nodeCheckpoint); err != nil {
		scopedLog.Error(
			"failed to decode node checkpoint file",
			logfields.Error, err,
		)
		return
	}

	// We can't call NodeUpdated for restored nodes here, as the machinery
	// assumes a fully initialized node manager, which we don't currently have.
	// In addition, we only want to replay NodeDeletions, since k8s provided
	// up-to-date information on all live nodes. We keep the restored nodes
	// separate, let whatever init needs to happen occur and once we're synced
	// to k8s, compare the restored nodes to the live ones.
	for _, n := range nodeCheckpoint {
		n.Source = source.Restored
		m.restoredNodes[n.Identity()] = n
	}
}

// initNodeCheckpointer sets up the trigger for writing nodes to disk.
func (m *manager) initNodeCheckpointer(minInterval time.Duration) error {
	var err error
	health := m.health.NewScope("node-checkpoint-writer")
	m.checkpointerDone = make(chan struct{})

	m.nodeCheckpointer, err = trigger.NewTrigger(trigger.Parameters{
		Name:        "node-checkpoint-trigger",
		MinInterval: minInterval, // To avoid rapid repetition (e.g. during startup).
		TriggerFunc: func(reasons []string) {
			m.mutex.RLock()
			select {
			// The trigger package does not check whether the trigger is shut
			// down already after sleeping to honor the MinInterval. Hence, we
			// do so ourselves.
			case <-m.checkpointerDone:
				return
			default:
			}
			err := m.checkpoint()
			m.mutex.RUnlock()

			if err != nil {
				m.logger.Error(
					"could not write node checkpoint",
					logfields.Error, err,
					logfields.Reasons, reasons,
				)
				health.Degraded("failed to write node checkpoint", err)
			} else {
				health.OK("node checkpoint written")
			}
		},
	})
	return err
}

// checkpoint writes all nodes to disk. Assumes the manager is read locked.
// Don't call this directly, use the nodeCheckpointer trigger.
func (m *manager) checkpoint() error {
	stateDir := m.conf.StateDir
	nodesPath := filepath.Join(stateDir, nodesFilename)
	m.logger.Debug(
		"writing node checkpoint to disk",
		logfields.Path, nodesPath,
	)

	// Write new contents to a temporary file which will be atomically renamed to the
	// real file at the end of this function to avoid data corruption if we crash.
	f, err := renameio.TempFile(stateDir, nodesPath)
	if err != nil {
		return fmt.Errorf("failed to open temporary file: %w", err)
	}
	defer f.Cleanup()

	bw := bufio.NewWriter(f)
	w := jsoniter.ConfigFastest.NewEncoder(bw)
	ns := make([]nodeTypes.Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		ns = append(ns, n.node)
	}
	if err := w.Encode(ns); err != nil {
		return fmt.Errorf("failed to encode node checkpoint: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("failed to flush node checkpoint writer: %w", err)
	}

	return f.CloseAtomicallyReplace()
}

func (m *manager) nodeAddressHasTunnelIP(address nodeTypes.Address) bool {
	// If the host firewall is enabled, all traffic to remote nodes must go
	// through the tunnel to preserve the source identity as part of the
	// encapsulation. In encryption case we also want to use vxlan device
	// to create symmetric traffic when sending nodeIP->pod and pod->nodeIP.
	return address.Type == addressing.NodeCiliumInternalIP || m.conf.NodeEncryptionEnabled() ||
		m.conf.EnableHostFirewall
}

func (m *manager) nodeAddressHasEncryptKey() bool {
	// If we are doing encryption, but not node based encryption, then do not
	// add a key to the nodeIPs so that we avoid a trip through stack and attempting
	// to encrypt something we know does not have an encryption policy installed
	// in the datapath. By setting key=0 and tunnelIP this will result in traffic
	// being sent unencrypted over overlay device.
	return m.conf.NodeEncryptionEnabled() &&
		// Also ignore any remote node's key if the local node opted to not perform
		// node-to-node encryption
		!node.GetOptOutNodeEncryption(m.logger)
}

// endpointEncryptionKey returns the encryption key index to use for the health
// and ingress endpoints of a node. This is needed for WireGuard where the
// node's EncryptionKey and the endpoint's EncryptionKey are not the same if
// a node has opted out of node-to-node encryption by zeroing n.EncryptionKey.
// With WireGuard, we always want to encrypt pod-to-pod traffic, thus we return
// a static non-zero encrypt key here.
// With IPSec (or no encryption), the node's encryption key index and the
// encryption key of the endpoint on that node are the same.
func (m *manager) endpointEncryptionKey(n *nodeTypes.Node) ipcacheTypes.EncryptKey {
	if m.conf.EnableWireguard {
		return ipcacheTypes.EncryptKey(types.StaticEncryptKey)
	}

	return ipcacheTypes.EncryptKey(n.EncryptionKey)
}

func (m *manager) nodeIdentityLabels(n nodeTypes.Node) (nodeLabels labels.Labels, hasOverride bool) {
	nodeLabels = labels.NewFrom(labels.LabelRemoteNode)
	if n.IsLocal() {
		nodeLabels = labels.NewFrom(labels.LabelHost)
		if m.conf.PolicyCIDRMatchesNodes() {
			for _, address := range n.IPAddresses {
				addr, ok := netipx.FromStdIP(address.IP)
				if ok {
					bitLen := addr.BitLen()
					if m.conf.EnableIPv4 && bitLen == net.IPv4len*8 ||
						m.conf.EnableIPv6 && bitLen == net.IPv6len*8 {
						prefix, err := addr.Prefix(bitLen)
						if err == nil {
							cidrLabels := labels.GetCIDRLabels(prefix)
							nodeLabels.MergeLabels(cidrLabels)
						}
					}
				}
			}
		}
	} else if !identity.NumericIdentity(n.NodeIdentity).IsReservedIdentity() {
		// This needs to match clustermesh-apiserver's VMManager.AllocateNodeIdentity
		nodeLabels = labels.Map2Labels(n.Labels, labels.LabelSourceK8s)
		hasOverride = true
	} else if !n.IsLocal() && option.Config.PerNodeLabelsEnabled() {
		lbls := labels.Map2Labels(n.Labels, labels.LabelSourceNode)
		clusterLabel := labels.NewLabel(k8sConst.PolicyLabelCluster, n.Cluster, labels.LabelSourceK8s)
		lbls[clusterLabel.Key] = clusterLabel
		filteredLbls, _ := labelsfilter.FilterNodeLabels(lbls)
		nodeLabels.MergeLabels(filteredLbls)
	}

	return nodeLabels, hasOverride
}

// worldLabelForPrefix returns the labels which will resolve to
// reserved:world identity given the provided prefix and the
// current cluster configuration in terms of dual-stack.
func worldLabelForPrefix(prefix netip.Prefix) labels.Labels {
	lbls := make(labels.Labels, 1)
	lbls.AddWorldLabel(prefix.Addr())
	return lbls
}

// NodeUpdated is called after the information of a node has been updated. The
// node in the manager is added or updated if the source is allowed to update
// the node. If an update or addition has occurred, NodeUpdate() of the datapath
// interface is invoked.
func (m *manager) NodeUpdated(n nodeTypes.Node) {
	m.logger.Info(
		"Node updated",
		logfields.ClusterName, n.Cluster,
		logfields.NodeName, n.Name,
		logfields.SPI, n.EncryptionKey,
	)
	if m.logger.Enabled(context.Background(), slog.LevelDebug) {
		m.logger.Debug(
			fmt.Sprintf("Received node update event from %s", n.Source),
			logfields.Node, n,
		)
	}

	nodeIdentifier := n.Identity()
	dpUpdate := true
	var nodeIP netip.Addr
	if nIP := n.GetNodeIP(m.underlay == tunnel.IPv6); nIP != nil {
		// GH-24829: Support IPv6-only nodes.

		// Skip returning the error here because at this level, we assume that
		// the IP is valid as long as it's coming from nodeTypes.Node. This
		// object is created either from the node discovery (K8s) or from an
		// event from the kvstore.
		nodeIP, _ = netipx.FromStdIP(nIP)
	}

	resource := ipcacheTypes.NewResourceID(ipcacheTypes.ResourceKindNode, "", n.Name)
	nodeLabels, nodeIdentityOverride := m.nodeIdentityLabels(n)

	var ipsetEntries []netip.Prefix
	var nodeIPsAdded, healthIPsAdded, ingressIPsAdded, podCIDRsAdded []netip.Prefix

	for _, address := range n.IPAddresses {
		prefix := ip.IPToNetPrefix(address.IP)
		var prefixCluster cmtypes.PrefixCluster
		if address.Type == addressing.NodeCiliumInternalIP {
			prefixCluster = cmtypes.PrefixClusterFrom(prefix, m.prefixClusterMutatorFn(&n)...)
		} else {
			prefixCluster = cmtypes.NewLocalPrefixCluster(prefix)
		}

		if address.Type == addressing.NodeInternalIP && !m.ipsetFilter(&n) {
			ipsetEntries = append(ipsetEntries, prefix)
		}

		var tunnelIP netip.Addr
		if m.nodeAddressHasTunnelIP(address) {
			tunnelIP = nodeIP
		}

		var key uint8
		if m.nodeAddressHasEncryptKey() {
			key = n.EncryptionKey
		}

		// We expect the node manager to have a source of either Kubernetes,
		// CustomResource, or KVStore. Prioritize the KVStore source over the
		// rest as it is the strongest source, i.e. only trigger datapath
		// updates if the information we receive takes priority.
		//
		// There are two exceptions to the rules above:
		// * kube-apiserver entries - in that case,
		//   we still want to inform subscribers about changes in auxiliary
		//   data such as for example the health endpoint.
		// * CiliumInternal IP addresses that match configured local router IP.
		//   In that case, we still want to inform subscribers about a new node
		//   even when IP addresses may seem repeated across the nodes.
		existing := m.ipcache.GetMetadataSourceByPrefix(prefixCluster)
		overwrite := source.AllowOverwrite(existing, n.Source)
		if !overwrite && existing != source.KubeAPIServer &&
			!(address.Type == addressing.NodeCiliumInternalIP && m.conf.IsLocalRouterIP(address.ToString())) {
			dpUpdate = false
		}

		lbls := nodeLabels
		// Add the CIDR labels for this node, if we allow selecting nodes by CIDR
		if m.conf.PolicyCIDRMatchesNodes() {
			lbls = labels.NewFrom(nodeLabels)
			lbls.MergeLabels(labels.GetCIDRLabels(prefixCluster.AsPrefix()))
		}

		// Always associate the prefix with metadata, even though this may not
		// end up in an ipcache entry.
		m.ipcache.UpsertMetadata(prefixCluster, n.Source, resource,
			lbls,
			ipcacheTypes.TunnelPeer{Addr: tunnelIP},
			ipcacheTypes.EncryptKey(key))
		if nodeIdentityOverride {
			m.ipcache.OverrideIdentity(prefixCluster, nodeLabels, n.Source, resource)
		}
		nodeIPsAdded = append(nodeIPsAdded, prefixCluster.AsPrefix())
	}

	var v4Addrs, v6Addrs []netip.Addr
	for _, prefix := range ipsetEntries {
		addr := prefix.Addr()
		if addr.Is6() {
			v6Addrs = append(v6Addrs, addr)
		} else {
			v4Addrs = append(v4Addrs, addr)
		}
	}
	m.ipsetMgr.AddToIPSet(ipset.CiliumNodeIPSetV4, ipset.INetFamily, v4Addrs...)
	m.ipsetMgr.AddToIPSet(ipset.CiliumNodeIPSetV6, ipset.INet6Family, v6Addrs...)

	// Add the remote node's Pod CIDRs as fallback entries into IPCache with
	// the nodeIP as the tunnel endpoint (no tunnel endpoint fallback is needed
	// for the local node).
	if !n.IsLocal() {
		ipv4PodCIDRs := n.GetIPv4AllocCIDRs()
		ipv6PodCIDRs := n.GetIPv6AllocCIDRs()

		mu := make([]ipcache.MU, 0, len(ipv4PodCIDRs)+len(ipv6PodCIDRs))
		for entry := range m.podCIDREntries(n.Source, resource, m.cidrsToPrefixesCluster(&n, ipv4PodCIDRs...), nodeIP, n.EncryptionKey) {
			mu = append(mu, entry)
			podCIDRsAdded = append(podCIDRsAdded, entry.Prefix.AsPrefix())
		}
		for entry := range m.podCIDREntries(n.Source, resource, m.cidrsToPrefixesCluster(&n, ipv6PodCIDRs...), nodeIP, n.EncryptionKey) {
			mu = append(mu, entry)
			podCIDRsAdded = append(podCIDRsAdded, entry.Prefix.AsPrefix())
		}
		m.ipcache.UpsertMetadataBatch(mu...)
	}

	for _, address := range []net.IP{n.IPv4HealthIP, n.IPv6HealthIP} {
		prefix := ip.IPToNetPrefix(address)
		if !prefix.IsValid() {
			continue
		}

		prefixCluster := cmtypes.PrefixClusterFrom(prefix, m.prefixClusterMutatorFn(&n)...)

		if !source.AllowOverwrite(m.ipcache.GetMetadataSourceByPrefix(prefixCluster), n.Source) {
			dpUpdate = false
		}

		m.ipcache.UpsertMetadata(prefixCluster, n.Source, resource,
			labels.LabelHealth,
			ipcacheTypes.TunnelPeer{Addr: nodeIP},
			m.endpointEncryptionKey(&n))
		healthIPsAdded = append(healthIPsAdded, prefixCluster.AsPrefix())
	}

	for _, address := range []net.IP{n.IPv4IngressIP, n.IPv6IngressIP} {
		prefix := ip.IPToNetPrefix(address)
		if !prefix.IsValid() {
			continue
		}

		prefixCluster := cmtypes.PrefixClusterFrom(prefix, m.prefixClusterMutatorFn(&n)...)

		if !source.AllowOverwrite(m.ipcache.GetMetadataSourceByPrefix(prefixCluster), n.Source) {
			dpUpdate = false
		}

		m.ipcache.UpsertMetadata(prefixCluster, n.Source, resource,
			labels.LabelIngress,
			ipcacheTypes.TunnelPeer{Addr: nodeIP},
			m.endpointEncryptionKey(&n))
		ingressIPsAdded = append(ingressIPsAdded, prefixCluster.AsPrefix())
	}

	m.mutex.Lock()
	entry, oldNodeExists := m.nodes[nodeIdentifier]
	if oldNodeExists {
		m.metrics.EventsReceived.WithLabelValues("update", string(n.Source)).Inc()

		if !source.AllowOverwrite(entry.node.Source, n.Source) {
			// Done; skip node-handler updates and label injection
			// triggers below. Includes case where the local host
			// was discovered locally and then is subsequently
			// updated by the k8s watcher.
			m.mutex.Unlock()
			return
		}

		entry.mutex.Lock()
		m.mutex.Unlock()
		oldNode := entry.node
		entry.node = n
		if dpUpdate {
			var errs error
			m.Iter(func(nh datapath.NodeHandler) {
				if err := nh.NodeUpdate(oldNode, entry.node); err != nil {
					m.logger.Error(
						"Failed to handle node update event while applying handler. Cilium may be have degraded functionality. See error message for details.",
						logfields.Error, err,
						logfields.Handler, nh.Name(),
						logfields.Node, entry.node.Name,
					)
					errs = errors.Join(errs, err)
				}
			})

			hr := m.health.NewScope("nodes-update")
			if errs != nil {
				hr.Degraded("Failed to update nodes", errs)
			} else {
				hr.OK("Node updates successful")
			}
		}

		m.removeNodeFromIPCache(oldNode, resource, ipsetEntries, nodeIPsAdded, healthIPsAdded, ingressIPsAdded, podCIDRsAdded)

		entry.mutex.Unlock()
	} else {
		m.metrics.EventsReceived.WithLabelValues("add", string(n.Source)).Inc()
		m.metrics.NumNodes.Inc()

		entry = &nodeEntry{node: n}
		entry.mutex.Lock()
		m.nodes[nodeIdentifier] = entry
		m.mutex.Unlock()
		var errs error
		if dpUpdate {
			m.Iter(func(nh datapath.NodeHandler) {
				if err := nh.NodeAdd(entry.node); err != nil {
					m.logger.Error(
						"Failed to handle node update event while applying handler. Cilium may be have degraded functionality. See error message for details.",
						logfields.Error, err,
						logfields.Handler, nh.Name(),
						logfields.Node, entry.node.Name,
					)
					errs = errors.Join(errs, err)
				}
			})
		}
		entry.mutex.Unlock()
		hr := m.health.NewScope("nodes-add")
		if errs != nil {
			hr.Degraded("Failed to add nodes", errs)
		} else {
			hr.OK("Node adds successful")
		}

	}

	if m.nodeCheckpointer != nil {
		m.nodeCheckpointer.TriggerWithReason("NodeUpdate")
	}
}

func (m *manager) cidrsToPrefixesCluster(n *nodeTypes.Node, cidrs ...*cidr.CIDR) iter.Seq[cmtypes.PrefixCluster] {
	return func(yield func(cmtypes.PrefixCluster) bool) {
		for _, cidr := range cidrs {
			if !yield(cmtypes.PrefixClusterFromCIDR(cidr, m.prefixClusterMutatorFn(n)...)) {
				return
			}
		}
	}
}

func (m *manager) podCIDREntries(source source.Source, resource ipcacheTypes.ResourceID, prefixes iter.Seq[cmtypes.PrefixCluster], tunnelIP netip.Addr, encryptKey uint8) iter.Seq[ipcache.MU] {
	return func(yield func(ipcache.MU) bool) {
		for prefix := range prefixes {
			if !prefix.IsValid() {
				continue
			}

			metadata := []ipcache.IPMetadata{
				worldLabelForPrefix(prefix.AsPrefix()),
				ipcacheTypes.TunnelPeer{Addr: tunnelIP},
				ipcacheTypes.EncryptKey(encryptKey),
			}

			if !yield(ipcache.MU{
				Prefix:   prefix,
				Source:   source,
				Resource: resource,
				Metadata: metadata,
			}) {
				return
			}
		}
	}
}

// removeNodeFromIPCache removes all addresses associated with oldNode from the IPCache,
// unless they are present in the nodeIPsAdded, healthIPsAdded, ingressIPsAdded lists.
// Removes all pod CIDRs associated with the oldNode from IPCache, unless they are present
// in podCIDRsAdded.
// Removes ipset entry associated with oldNode if it is not present in ipsetEntries.
//
// The removal logic in this function should mirror the upsert logic in NodeUpdated.
func (m *manager) removeNodeFromIPCache(oldNode nodeTypes.Node, resource ipcacheTypes.ResourceID,
	ipsetEntries, nodeIPsAdded, healthIPsAdded, ingressIPsAdded, podCIDRsAdded []netip.Prefix,
) {
	var oldNodeIP netip.Addr
	if nIP := oldNode.GetNodeIP(false); nIP != nil {
		// See comment in NodeUpdated().
		oldNodeIP, _ = netipx.FromStdIP(nIP)
	}
	oldNodeLabels, oldNodeIdentityOverride := m.nodeIdentityLabels(oldNode)

	// Delete the old node IP addresses if they have changed in this node.
	var v4Addrs, v6Addrs []netip.Addr
	for _, address := range oldNode.IPAddresses {
		prefix := ip.IPToNetPrefix(address.IP)
		if slices.Contains(nodeIPsAdded, prefix) {
			continue
		}

		var oldPrefixCluster cmtypes.PrefixCluster
		if address.Type == addressing.NodeCiliumInternalIP {
			oldPrefixCluster = cmtypes.PrefixClusterFrom(prefix, m.prefixClusterMutatorFn(&oldNode)...)
		} else {
			oldPrefixCluster = cmtypes.NewLocalPrefixCluster(prefix)
		}

		if address.Type == addressing.NodeInternalIP && !slices.Contains(ipsetEntries, oldPrefixCluster.AsPrefix()) {
			addr, ok := netipx.FromStdIP(address.IP)
			if !ok {
				m.logger.Error(
					"unable to convert to netip.Addr",
					logfields.IPAddr, address.IP,
				)
				continue
			}
			if addr.Is6() {
				v6Addrs = append(v6Addrs, addr)
			} else {
				v4Addrs = append(v4Addrs, addr)
			}
		}

		var oldTunnelIP netip.Addr
		if m.nodeAddressHasTunnelIP(address) {
			oldTunnelIP = oldNodeIP
		}

		var oldKey uint8
		if m.nodeAddressHasEncryptKey() {
			oldKey = oldNode.EncryptionKey
		}

		m.ipcache.RemoveMetadata(oldPrefixCluster, resource,
			oldNodeLabels,
			ipcacheTypes.TunnelPeer{Addr: oldTunnelIP},
			ipcacheTypes.EncryptKey(oldKey))
		if oldNodeIdentityOverride {
			m.ipcache.RemoveIdentityOverride(oldPrefixCluster, oldNodeLabels, resource)
		}
	}

	m.ipsetMgr.RemoveFromIPSet(ipset.CiliumNodeIPSetV4, v4Addrs...)
	m.ipsetMgr.RemoveFromIPSet(ipset.CiliumNodeIPSetV6, v6Addrs...)

	// Remove old pod CIDR fallback entries from IPCache
	if !oldNode.IsLocal() {
		oldIPv4PodCIDRs := oldNode.GetIPv4AllocCIDRs()
		oldIPv6PodCIDRs := oldNode.GetIPv6AllocCIDRs()

		mu := make([]ipcache.MU, 0, len(oldIPv4PodCIDRs)+len(oldIPv6PodCIDRs))
		for entry := range m.podCIDREntries(oldNode.Source, resource, m.cidrsToPrefixesCluster(&oldNode, oldIPv4PodCIDRs...), oldNodeIP, oldNode.EncryptionKey) {
			if slices.Contains(podCIDRsAdded, entry.Prefix.AsPrefix()) {
				continue
			}
			mu = append(mu, entry)
		}
		for entry := range m.podCIDREntries(oldNode.Source, resource, m.cidrsToPrefixesCluster(&oldNode, oldIPv6PodCIDRs...), oldNodeIP, oldNode.EncryptionKey) {
			if slices.Contains(podCIDRsAdded, entry.Prefix.AsPrefix()) {
				continue
			}
			mu = append(mu, entry)
		}
		m.ipcache.RemoveMetadataBatch(mu...)
	}

	// Delete the old health IP addresses if they have changed in this node.
	for _, address := range []net.IP{oldNode.IPv4HealthIP, oldNode.IPv6HealthIP} {
		prefix := ip.IPToNetPrefix(address)
		if !prefix.IsValid() || slices.Contains(healthIPsAdded, prefix) {
			continue
		}

		prefixCluster := cmtypes.PrefixClusterFrom(prefix, m.prefixClusterMutatorFn(&oldNode)...)

		m.ipcache.RemoveMetadata(prefixCluster, resource,
			labels.LabelHealth,
			ipcacheTypes.TunnelPeer{Addr: oldNodeIP},
			m.endpointEncryptionKey(&oldNode))
	}

	// Delete the old ingress IP addresses if they have changed in this node.
	for _, address := range []net.IP{oldNode.IPv4IngressIP, oldNode.IPv6IngressIP} {
		prefix := ip.IPToNetPrefix(address)
		if !prefix.IsValid() || slices.Contains(ingressIPsAdded, prefix) {
			continue
		}

		prefixCluster := cmtypes.PrefixClusterFrom(prefix, m.prefixClusterMutatorFn(&oldNode)...)

		m.ipcache.RemoveMetadata(prefixCluster, resource,
			labels.LabelIngress,
			ipcacheTypes.TunnelPeer{Addr: oldNodeIP},
			m.endpointEncryptionKey(&oldNode))
	}
}

// NodeDeleted is called after a node has been deleted. It removes the node
// from the manager if the node is still owned by the source of which the event
// origins from. If the node was removed, NodeDelete() is invoked of the
// datapath interface.
func (m *manager) NodeDeleted(n nodeTypes.Node) {
	m.logger.Info(
		"Node deleted",
		logfields.ClusterName, n.Cluster,
		logfields.NodeName, n.Name,
	)
	m.logger.Debug(
		"Received node delete event",
		logfields.Source, n.Source,
	)

	m.metrics.EventsReceived.WithLabelValues("delete", string(n.Source)).Inc()

	nodeIdentifier := n.Identity()

	var (
		entry         *nodeEntry
		oldNodeExists bool
	)

	m.mutex.Lock()
	// If the node is restored from disk, it doesn't exist in the bookkeeping,
	// but we need to synthesize a deletion event for downstream.
	if n.Source == source.Restored {
		entry = &nodeEntry{
			node: n,
		}
	} else {
		entry, oldNodeExists = m.nodes[nodeIdentifier]
		if !oldNodeExists {
			m.mutex.Unlock()
			return
		}
	}

	// If the source is Kubernetes and the node is the node we are running on
	// Kubernetes is giving us a hint it is about to delete our node. Close down
	// the agent gracefully in this case.
	if n.Source != entry.node.Source {
		m.mutex.Unlock()
		if n.IsLocal() && n.Source == source.Kubernetes {
			m.logger.Debug(
				"Kubernetes is deleting local node, close manager",
			)
			m.Stop(context.Background())
		} else {
			m.logger.Debug(
				"Ignoring delete event of node",
				logfields.Name, n.Name,
				logfields.Source, n.Source,
				logfields.NodeOwner, entry.node.Source,
			)
		}
		return
	}

	// The ipcache is recreated from scratch on startup, no need to prune restored stale nodes.
	if n.Source != source.Restored {
		resource := ipcacheTypes.NewResourceID(ipcacheTypes.ResourceKindNode, "", n.Name)
		m.removeNodeFromIPCache(entry.node, resource, nil, nil, nil, nil, nil)
	}

	m.metrics.NumNodes.Dec()

	entry.mutex.Lock()
	delete(m.nodes, nodeIdentifier)
	if m.nodeCheckpointer != nil {
		m.nodeCheckpointer.TriggerWithReason("NodeDeleted")
	}
	m.mutex.Unlock()
	var errs error
	m.Iter(func(nh datapath.NodeHandler) {
		if err := nh.NodeDelete(n); err != nil {
			// For now we log the error and continue. Eventually we will want to encorporate
			// this into the node managers health status.
			// However this is a bit tricky - as leftover node deletes are not retries so this will
			// need to be accompanied by some kind of retry mechanism.
			m.logger.Error(
				"Failed to handle node delete event while applying handler. Cilium may be have degraded functionality.",
				logfields.Error, err,
				logfields.Handler, nh.Name(),
				logfields.Node, n.Name,
			)
			errs = errors.Join(errs, err)
		}
	})
	entry.mutex.Unlock()

	hr := m.health.NewScope("nodes-delete")
	if errs != nil {
		hr.Degraded("Failed to delete nodes", errs)
	} else {
		hr.OK("Node deletions successful")
	}
}

// NodeSync signals the manager that the initial nodes listing (either from k8s
// or kvstore) has been completed. This allows the manager to initiate the
// deletion of possible stale nodes.
func (m *manager) NodeSync() {
	m.ipsetInitializer.InitDone()

	// Due to the complexity around kvstore vs k8s as node sources, it may occur
	// that both sources call NodeSync at some point. Ensure we only run this
	// pruning operation once.
	m.nodePruneOnce.Do(func() {
		m.pruneNodes(false)
	})
}

func (m *manager) MeshNodeSync() {
	m.pruneNodes(true)
}

func (m *manager) pruneNodes(includeMeshed bool) {
	m.mutex.Lock()
	if len(m.restoredNodes) == 0 {
		m.mutex.Unlock()
		return
	}
	// Live nodes should not be pruned.
	for id := range m.nodes {
		delete(m.restoredNodes, id)
	}

	if len(m.restoredNodes) > 0 {
		if m.logger.Enabled(context.Background(), slog.LevelDebug) {
			printableNodes := make([]string, 0, len(m.restoredNodes))
			for ni := range m.restoredNodes {
				printableNodes = append(printableNodes, ni.String())
			}
			m.logger.Debug(
				"Deleting stale nodes",
				logfields.LenStaleNodes, len(m.restoredNodes),
				logfields.StaleNodes, printableNodes,
			)
		} else {
			m.logger.Info(
				"Deleting stale nodes",
				logfields.LenStaleNodes, len(m.restoredNodes),
			)
		}
	}
	m.mutex.Unlock()

	// Delete nodes now considered stale. Can't hold the mutex as
	// NodeDeleted also acquires it.
	for id, n := range m.restoredNodes {
		if n.Cluster == m.conf.ClusterName || includeMeshed {
			m.NodeDeleted(*n)
			delete(m.restoredNodes, id)
		}
	}
}

// GetNodeIdentities returns a list of all node identities store in node
// manager.
func (m *manager) GetNodeIdentities() []nodeTypes.Identity {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	nodes := make([]nodeTypes.Identity, 0, len(m.nodes))
	for nodeIdentity := range m.nodes {
		nodes = append(nodes, nodeIdentity)
	}

	return nodes
}

// GetNodes returns a copy of all of the nodes as a map from Identity to Node.
func (m *manager) GetNodes() map[nodeTypes.Identity]nodeTypes.Node {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	nodes := make(map[nodeTypes.Identity]nodeTypes.Node, len(m.nodes))
	for nodeIdentity, entry := range m.nodes {
		entry.mutex.Lock()
		nodes[nodeIdentity] = entry.node
		entry.mutex.Unlock()
	}

	return nodes
}

// SetPrefixClusterMutatorFn allows to inject a custom prefix cluster mutator.
// The mutator may then be applied to the PrefixCluster(s) using cmtypes.PrefixClusterFrom,
// cmtypes.PrefixClusterFromCIDR and the like.
func (m *manager) SetPrefixClusterMutatorFn(mutator func(*nodeTypes.Node) []cmtypes.PrefixClusterOpts) {
	m.prefixClusterMutatorFn = mutator
}
